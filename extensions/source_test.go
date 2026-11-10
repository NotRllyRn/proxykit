package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/worker"
)

func sourceItems(entry, body string, extra ...item) []item {
	return append([]item{{name: "root/proxykit-extension.json", body: `{"manifestVersion":1,"apiVersion":1,"id":"example-user","version":"1.0.0","entry":"` + entry + `"}`}, {name: "root/" + entry, body: body}}, extra...)
}
func TestInstalledSourceRuntime(t *testing.T) {
	for _, entry := range []string{"src/main.mts", "src/main.ts", "main.mts", "main.ts", "main.ts.mts"} {
		t.Run(entry, func(t *testing.T) {
			dir := filepath.Dir(entry)
			items := sourceItems(entry, `import {defineHTTP, pass} from '@proxykit/sdk';
import {label} from './exports.ts';
import {hidden} from './.hidden/helper.ts';
export default defineHTTP(api => { api.on({id:'source',stage:'request',bodyMode:'none'}, async () => ({...pass(),metadata:{label,hidden,dynamic:(await import('./relative.mts')).label,asset:new URL('./asset.txt',import.meta.url).href,data:api.dataDir}})); });`, item{name: filepath.ToSlash(filepath.Join("root", dir, "exports.ts")), body: `export { label } from './relative.mjs';`}, item{name: filepath.ToSlash(filepath.Join("root", dir, "relative.mts")), body: `export const label: string = 'compiled';`}, item{name: filepath.ToSlash(filepath.Join("root", dir, ".hidden/helper.ts")), body: `export const hidden: string = 'dot directory';`}, item{name: filepath.ToSlash(filepath.Join("root", dir, "asset.txt")), body: "asset content"}, item{name: "root/package.json", body: `{"type":"module","scripts":{"install":"exit 99","build":"exit 99"}}`})
			i, _ := fixture(t, archive(t, items))
			if entry == "src/main.ts" {
				cwd, e := os.Getwd()
				if e != nil {
					t.Fatal(e)
				}
				i.Store, e = filepath.Rel(cwd, i.Store)
				if e != nil {
					t.Fatal(e)
				}
			}
			r, e := i.Install(context.Background(), "https://github.com/example/repository", true)
			if e != nil {
				t.Fatal(e)
			}
			if r.Build == nil || r.Build.CompilerSHA256 != compilerHash {
				t.Fatal("missing compiler provenance")
			}
			s, e := i.State()
			if e != nil || s.Decisions[r.Manifest.ID].Enabled {
				t.Fatal("install enabled", e)
			}
			rows, e := i.Inventory()
			if e != nil || len(rows) != 1 || rows[0].Diagnostic != "" {
				t.Fatal(rows, e)
			}
			data, e := i.DataDir(r.Manifest.ID)
			if e != nil {
				t.Fatal(e)
			}
			w, e := worker.New(worker.Options{Entry: filepath.Join(i.Store, r.Manifest.ID, "package", r.RuntimeEntry()), DataDir: data})
			if e != nil {
				t.Fatal(e)
			}
			if e = w.Start(context.Background()); e != nil {
				t.Fatal(e)
			}
			defer w.Stop(context.Background())
			b, e := w.Invoke(context.Background(), "source", json.RawMessage(`{}`))
			if e != nil {
				t.Fatal(e)
			}
			var result struct {
				Action   string
				Metadata map[string]string
			}
			if e = json.Unmarshal(b, &result); e != nil {
				t.Fatal(e)
			}
			if result.Action != "pass" || result.Metadata["label"] != "compiled" || result.Metadata["hidden"] != "dot directory" || result.Metadata["dynamic"] != "compiled" || result.Metadata["data"] != data {
				t.Fatalf("%s", b)
			}
			u, e := url.Parse(result.Metadata["asset"])
			if e != nil {
				t.Fatal(e)
			}
			asset, e := os.ReadFile(u.Path)
			if e != nil || string(asset) != "asset content" {
				t.Fatal("runtime relative asset", e)
			}
			entries, e := os.ReadDir(data)
			if e != nil || len(entries) != 0 {
				t.Fatal("build polluted data", e)
			}
			// Source and runtime assets are both covered; mutation is incompatible, never executed.
			if e = os.WriteFile(u.Path, []byte("changed"), 0600); e != nil {
				t.Fatal(e)
			}
			rows, e = i.Inventory()
			if e != nil || !strings.Contains(rows[0].Diagnostic, "integrity") {
				t.Fatal(rows, e)
			}
		})
	}
}
func TestSourceFailuresDoNotPublish(t *testing.T) {
	cases := map[string]string{
		"syntax":     `export default (`,
		"type":       `const value: number = 'wrong'; export default () => {};`,
		"sdk-type":   `import {defineHTTP} from '@proxykit/sdk'; export default defineHTTP(api => {api.on({id:'x',stage:'not-real',bodyMode:'none'}, () => ({action:'pass'}));});`,
		"sdk-export": `import {missing} from '@proxykit/sdk'; export default missing;`,
		"bare":       `import x from 'arbitrary-package'; export default x;`,
		"builtin":    `import fs from 'node:fs'; export default fs;`,
		"escape":     `import x from '../../outside.mts'; export default x;`,
		"nonliteral": `const x = './other.mts'; export default () => import(x);`,
		"reference": `/// <reference path="../../outside.d.ts" />
export default () => {};`,
		"commonjs": `export = {};`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			i, _ := fixture(t, archive(t, sourceItems("main.mts", body)))
			before, e := i.State()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = i.Install(context.Background(), "https://github.com/example/repository", true); e == nil {
				t.Fatal("accepted invalid source")
			}
			after, e := i.State()
			if e != nil || after.Revision != before.Revision || len(after.Decisions) != 0 {
				t.Fatal("intent mutated", after, e)
			}
			if _, e = os.Stat(filepath.Join(i.Store, "example-user")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("published failed compile", e)
			}
			entries, e := os.ReadDir(i.Store)
			if e != nil {
				t.Fatal(e)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".stage-") {
					t.Fatal("stage leaked")
				}
			}
		})
	}
}
func TestSourceCompileCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[cancelled], func(t *testing.T) {
			i, _ := fixture(t, archive(t, sourceItems("main.mts", `export default () => {};`)))
			// Cancellation before process start must also leave no state.
			i.CompileTimeout = time.Nanosecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			start := time.Now()
			_, e := i.Install(ctx, "https://github.com/example/repository", true)
			if e == nil || time.Since(start) > 15*time.Second {
				t.Fatal("unbounded cancellation", e)
			}
			if !errors.Is(e, context.Canceled) && !errors.Is(e, context.DeadlineExceeded) {
				t.Fatal(e)
			}
			s, e := i.State()
			if e != nil || s.Revision != 0 {
				t.Fatal("intent mutation", e)
			}
		})
	}
}

// A host-controlled Node executable stand-in proves cancellation of an already
// running compiler process, not merely cancellation before Command.Start.
func TestRunningCompilerDeadlineAndCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "started")
			executable := filepath.Join(dir, "node-fixture")
			if e := os.WriteFile(executable, []byte("#!/bin/sh\nprintf started > '"+marker+"'\nexec sleep 30\n"), 0700); e != nil {
				t.Fatal(e)
			}
			i, _ := fixture(t, archive(t, sourceItems("main.mts", `export default () => {};`)))
			i.NodeBinary = executable
			i.CompileTimeout = 300 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := i.Install(ctx, "https://github.com/example/repository", true); done <- e }()
			// Race instrumentation can make compiler provisioning slow. Allow
			// that separately from the two-second process termination bound.
			limit := time.After(15 * time.Second)
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
		started:
			for {
				select {
				case e := <-done:
					t.Fatal("compiler never started", e)
				case <-limit:
					t.Fatal("compiler start timeout")
				case <-ticker.C:
					if _, e := os.Stat(marker); e == nil {
						break started
					}
				}
			}
			if !deadline {
				cancel()
			}
			select {
			case e := <-done:
				want := context.Canceled
				if deadline {
					want = context.DeadlineExceeded
				}
				if !errors.Is(e, want) {
					t.Fatal(e)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("compiler did not terminate/reap")
			}
			if _, e := os.Stat(filepath.Join(i.Store, "example-user")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("published canceled compilation", e)
			}
			s, e := i.State()
			if e != nil || s.Revision != 0 || len(s.Decisions) != 0 {
				t.Fatal("intent mutation", s, e)
			}
		})
	}
}
func TestSourceInstallDoesNotExecute(t *testing.T) {
	i, _ := fixture(t, archive(t, sourceItems("main.ts", `throw new Error('package executed while disabled'); export default () => {};`)))
	if _, e := i.Install(context.Background(), "https://github.com/example/repository", true); e != nil {
		t.Fatal(e)
	}
	s, e := i.State()
	if e != nil || s.Decisions["example-user"].Enabled {
		t.Fatal("install enabled package", e)
	}
}
func TestSourceTreeBoundsAndLinks(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "module.mjs")
	if e := os.WriteFile(p, []byte("export default 1;"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := treeDigest(root, 1, 10); e == nil {
		t.Fatal("accepted oversized tree")
	}
	if _, e := treeDigest(root, 100, 0); e == nil {
		t.Fatal("accepted excessive artifacts")
	}
	before, e := treeDigest(root, 100, 10)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("export default 2;"), 0600); e != nil {
		t.Fatal(e)
	}
	after, e := treeDigest(root, 100, 10)
	if e != nil || before == after {
		t.Fatal("module mutation not detected", e)
	}
	if e = os.Symlink(p, filepath.Join(root, "link")); e != nil {
		t.Skip("symlink unsupported", e)
	}
	if _, e = treeDigest(root, 100, 10); e == nil {
		t.Fatal("accepted symlink")
	}
}
func TestCompilerDiagnosticsBound(t *testing.T) {
	w := &cappedLog{}
	b := make([]byte, 1<<20)
	for n := 0; n < 16; n++ {
		if size, e := w.Write(b); e != nil || size != len(b) {
			t.Fatal(size, e)
		}
	}
	if len(w.b) != 16384 {
		t.Fatal("unbounded diagnostic storage", len(w.b))
	}
}
func TestSourceRejectsConfigAndReservedArtifacts(t *testing.T) {
	for _, extra := range []item{{name: "root/tsconfig.json", body: `{"compilerOptions":{"plugins":[{"name":"bad"}]}}`}, {name: "root/.proxykit-build/main.mjs", body: "throw 1"}, {name: "root/.sdk/index.mjs", body: "throw 1"}, {name: "root/dependency.js", body: "throw 1"}, {name: "root/types.d.ts", body: "declare const x: number;"}, {name: "root/main.mts", body: "export default () => {};"}} {
		t.Run(extra.name, func(t *testing.T) {
			i, _ := fixture(t, archive(t, sourceItems("main.ts", `export default () => {};`, extra)))
			if _, e := i.Install(context.Background(), "https://github.com/example/repository", true); e == nil {
				t.Fatal("accepted unsupported package")
			}
		})
	}
}
