package nodehost

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/extensions"
)

func TestRuntimeReusesInstalledTypeScriptWorkers(t *testing.T) {
	var archive atomic.Value
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			fmt.Fprintf(w, `{"sha":%q}`, strings.Repeat("a", 40))
		} else {
			w.Write(archive.Load().([]byte))
		}
	}))
	defer fake.Close()
	i := extensions.Installer{Store: filepath.Join(t.TempDir(), "store"), APIBase: fake.URL, ArchiveBase: fake.URL}
	for _, id := range []string{"source-a", "source-b"} {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		for name, body := range map[string]string{
			"proxykit-extension.json": fmt.Sprintf(`{"manifestVersion":1,"apiVersion":1,"id":%q,"version":"1.0.0","entry":"main.ts"}`, id),
			"main.ts": fmt.Sprintf(`import {defineHTTP, replace} from '@proxykit/sdk';
// Declare only the runtime global used as test evidence; no Node imports.
declare const process: {pid: number};
export default defineHTTP(api => {
 let count = 0;
 api.on({id:'state',stage:'responseStart',bodyMode:'none'}, input => replace({...input.response,header:{...input.response.header,
 %q:[String(process.pid)],%q:[String(++count)]}}));
});`, "X-"+id+"-Pid", "X-"+id+"-Count"),
		} {
			if e := tw.WriteHeader(&tar.Header{Name: "root/" + name, Mode: 0600, Size: int64(len(body))}); e != nil {
				t.Fatal(e)
			}
			if _, e := io.WriteString(tw, body); e != nil {
				t.Fatal(e)
			}
		}
		if e := tw.Close(); e != nil {
			t.Fatal(e)
		}
		if e := gz.Close(); e != nil {
			t.Fatal(e)
		}
		archive.Store(b.Bytes())
		if _, e := i.Install(context.Background(), "https://github.com/example/repository", true); e != nil {
			t.Fatal(e)
		}
	}
	m, gateway := runtimeProxy(t, i.Store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}), time.Second, 4)
	change := func(id string, enabled bool) {
		t.Helper()
		if e := m.Change(context.Background(), id, enabled); e != nil {
			t.Fatal(e)
		}
		waitRetired(t, m)
	}
	change("source-a", true)
	_, h := getRuntime(t, gateway.URL)
	pid := h.Get("X-Source-A-Pid")
	if pid == "" || h.Get("X-Source-A-Count") != "1" {
		t.Fatal(h)
	}
	for n := 0; n < 2; n++ {
		change("source-b", true)
		_, h = getRuntime(t, gateway.URL)
		if h.Get("X-Source-A-Pid") != pid || h.Get("X-Source-A-Count") != fmt.Sprint(2+n*2) || h.Get("X-Source-B-Count") != "1" {
			t.Fatal("enabling B restarted A or lost state", h)
		}
		change("source-b", false)
		change("source-a", true) // Reconcile already-enabled stable source.
		_, h = getRuntime(t, gateway.URL)
		if h.Get("X-Source-A-Pid") != pid || h.Get("X-Source-A-Count") != fmt.Sprint(3+n*2) || h.Get("X-Source-B-Pid") != "" {
			t.Fatal("disabling B changed A", h)
		}
	}
	change("source-a", false)
	change("source-a", true)
	_, h = getRuntime(t, gateway.URL)
	if h.Get("X-Source-A-Pid") == "" || h.Get("X-Source-A-Count") != "1" {
		t.Fatal("stable source failed to restart after disable", h)
	}
	change("source-b", true) // Fresh inventory must also reuse the restarted A.
}

// Production archive installer -> typechecker -> installed host -> native HTTP pipeline.
func TestInstalledTypeScriptHTTPAndFailedReplacement(t *testing.T) {
	makeArchive := func(source string) []byte {
		var b bytes.Buffer
		gz := gzip.NewWriter(&b)
		tw := tar.NewWriter(gz)
		for name, body := range map[string]string{
			"root/proxykit-extension.json": `{"manifestVersion":1,"apiVersion":1,"id":"source-example","version":"1.0.0","entry":"src/main.mts"}`,
			"root/src/main.mts":            source,
			"root/src/label.ts":            `export const label: string = 'source installed';`,
		} {
			if e := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); e != nil {
				t.Fatal(e)
			}
			if _, e := io.WriteString(tw, body); e != nil {
				t.Fatal(e)
			}
		}
		if e := tw.Close(); e != nil {
			t.Fatal(e)
		}
		if e := gz.Close(); e != nil {
			t.Fatal(e)
		}
		return b.Bytes()
	}
	var archive atomic.Value
	archive.Store(makeArchive(`import {defineHTTP, replace} from '@proxykit/sdk';
import {label} from './label.js';
export default defineHTTP(api => {
 api.on({id:'label',stage:'responseStart',bodyMode:'none'}, input => replace({...input.response,header:{...input.response.header,'X-Source':[label]}}));
});`))
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			fmt.Fprintf(w, `{"sha":%q}`, strings.Repeat("a", 40))
		} else {
			w.Write(archive.Load().([]byte))
		}
	}))
	defer fake.Close()
	i := extensions.Installer{Store: filepath.Join(t.TempDir(), "store"), APIBase: fake.URL, ArchiveBase: fake.URL}
	record, e := i.Install(context.Background(), "https://github.com/example/repository", true)
	if e != nil {
		t.Fatal(e)
	}
	state, e := i.State()
	if e != nil {
		t.Fatal(e)
	}
	state, e = i.Decide(state, record.Manifest.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = i.SaveState(state); e != nil {
		t.Fatal(e)
	}
	server, _, _ := openProxy(t, i.Store, extension.Config{}, false)
	check := func() {
		t.Helper()
		resp, e := http.Get(server.URL + "/example")
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, e := io.ReadAll(resp.Body)
		if e != nil || string(b) != "upstream" || resp.Header.Get("X-Source") != "source installed" {
			t.Fatal(resp.Status, string(b), resp.Header, e)
		}
	}
	check()
	before, e := os.ReadFile(filepath.Join(i.Store, ".enabled.json"))
	if e != nil {
		t.Fatal(e)
	}
	archive.Store(makeArchive(`const label: number = 'type error'; export default () => {};`))
	if _, e = i.Install(context.Background(), record.Source, true); e == nil || !strings.Contains(e.Error(), "TS2322") {
		t.Fatal("replacement must fail actual typecheck", e)
	}
	after, e := os.ReadFile(filepath.Join(i.Store, ".enabled.json"))
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("failed compile mutated enabled intent", e)
	}
	check()
	rows, e := i.Inventory()
	if e != nil || len(rows) != 1 || rows[0].Diagnostic != "" {
		t.Fatal("prior package damaged", rows, e)
	}
	// Integrity also protects original source, not just the generated module.
	if e = os.WriteFile(filepath.Join(i.Store, record.Manifest.ID, "package", "src", "label.ts"), []byte("modified"), 0600); e != nil {
		t.Fatal(e)
	}
	rows, e = i.Inventory()
	if e != nil || !strings.Contains(rows[0].Diagnostic, "integrity") {
		t.Fatal(rows, e)
	}
	check() // Running generation is untouched; next activation refuses changed packages.
}
