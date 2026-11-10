//go:build linux

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extensions"
)

// Exercise the actual main and go-run wrapper in a terminal, not run(ctx) or a
// signal sent only to the host PID. No upstream requests or real stores are used.
func TestTerminalCtrlC(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node unavailable")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("Python unavailable for PTY regression")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "proxykit")
	build := exec.Command("go", "build", "-o", binary, "./cmd/proxykit")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, launch := range []string{"compiled", "go-run"} {
		for _, phase := range []string{"ready", "startup", "parent-death-ready", "parent-death-startup"} {
			if strings.HasPrefix(phase, "parent-death") && launch == "go-run" {
				continue // kill the actual host, not the go-run wrapper
			}
			t.Run(launch+"/"+phase, func(t *testing.T) {
				store := filepath.Join(t.TempDir(), "extensions")
				pkg := filepath.Join(store, "example-user", "package")
				if err := os.MkdirAll(pkg, 0700); err != nil {
					t.Fatal(err)
				}
				manifest := extensions.Manifest{ManifestVersion: 1, APIVersion: 1, ID: "example-user", Version: "1.0.0", Entry: "main.mjs"}
				commit := strings.Repeat("a", 40)
				m, _ := json.Marshal(manifest)
				record, _ := json.Marshal(extensions.Installation{Manifest: manifest, Source: "https://github.com/example/repository", Commit: commit})
				for path, data := range map[string][]byte{
					filepath.Join(pkg, "proxykit-extension.json"):             m,
					filepath.Join(pkg, "main.mjs"):                            []byte(signalFixture),
					filepath.Join(store, "example-user", "installation.json"): record,
				} {
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				installer := extensions.Installer{Store: store}
				state, err := installer.State()
				if err != nil {
					t.Fatal(err)
				}
				state.Decisions[manifest.ID] = extensions.Decision{Commit: commit, Enabled: true}
				if err := installer.SaveState(state); err != nil {
					t.Fatal(err)
				}
				command := []string{binary}
				if launch == "go-run" {
					command = []string{"go", "run", "./cmd/proxykit"}
				}
				args := append([]string{filepath.Join(root, "cmd", "proxykit", "testdata", "ctrl_c.py"), store, phase}, command...)
				args = append(args, "-listen", "127.0.0.1:0", "-upstream", "https://api.openai.com", "-node", node)
				driver := exec.Command("python3", args...)
				driver.Dir = root
				driver.Env = append(os.Environ(), "PROXYKIT_EXTENSIONS_DIR="+store, "PROXYKIT_SIGNAL_PHASE="+phase)
				if out, err := driver.CombinedOutput(); err != nil {
					t.Fatalf("terminal Ctrl-C: %v\n%s", err, out)
				}
				if unlock, err := extensions.Lock(store); err != nil {
					t.Fatalf("store still locked: %v", err)
				} else {
					unlock()
				}
			})
		}
	}
}

const signalFixture = `import {writeFileSync,openSync,closeSync,unlinkSync} from 'node:fs';
import {spawn} from 'node:child_process';
import {join} from 'node:path';
export default async api => {
  const dir = process.env.PROXYKIT_EXTENSIONS_DIR;
  writeFileSync(join(dir,'worker.pid'), String(process.pid));
  const child = spawn(process.execPath,['-e','setInterval(()=>{},1000)'],{stdio:'ignore'});
  writeFileSync(join(dir,'descendant.pid'), String(child.pid));
  const lock = join(dir,'fixture.lock'); closeSync(openSync(lock,'wx'));
  writeFileSync(join(dir,'acquired'),'');
  if (process.env.PROXYKIT_SIGNAL_PHASE.endsWith('startup')) await new Promise(r=>setTimeout(r,400));
  // Intentionally register after the await: cancellation must await setup.
  api.cleanup(async()=>{
    console.log('cleanup after host shutdown');
    await new Promise(r=>setTimeout(r,100));
    unlinkSync(lock); writeFileSync(join(dir,'cleaned'),'');
  });
};`
