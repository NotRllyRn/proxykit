package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sha = "0123456789abcdef0123456789abcdef01234567"

type item struct {
	name, body string
	kind       byte
	link       string
	pax        map[string]string
}

func archive(t *testing.T, entries []item) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Mode: 0600, Typeflag: kind, Linkname: e.link, PAXRecords: e.pax}
		if kind == tar.TypeXGlobalHeader {
			h = &tar.Header{Typeflag: kind, PAXRecords: e.pax}
		}
		if kind == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func validItems() []item {
	return []item{{name: "root/proxykit-extension.json", body: `{"manifestVersion":1,"apiVersion":1,"id":"example-user","version":"1.0.0","entry":"dist/main.mjs"}`}, {name: "root/dist/main.mjs", body: `export default api => {};`}}
}
func fixture(t *testing.T, data []byte) (Installer, *[]string) {
	t.Helper()
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			fmt.Fprintf(w, `{"sha":%q}`, sha)
		} else {
			w.Write(data)
		}
	}))
	t.Cleanup(server.Close)
	return Installer{Store: filepath.Join(t.TempDir(), "store"), APIBase: server.URL, ArchiveBase: server.URL}, &paths
}
func TestInstallListRemove(t *testing.T) {
	i, paths := fixture(t, archive(t, validItems()))
	ctx := context.Background()
	if _, err := i.Install(ctx, "https://github.com/example/repository/tree/main", false); err == nil {
		t.Fatal("implicit trust")
	}
	record, err := i.Install(ctx, "https://github.com/example/repository/tree/main", true)
	if err != nil {
		t.Fatal(err)
	}
	if record.Commit != sha || !strings.HasSuffix((*paths)[1], sha) {
		t.Fatalf("not immutable: %+v %v", record, *paths)
	}
	records, err := i.List()
	if err != nil || len(records) != 1 || records[0] != record {
		t.Fatalf("list: %+v %v", records, err)
	}
	if _, err = i.Install(ctx, record.Source, true); err == nil {
		t.Fatal("overwrite accepted")
	}
	b, err := os.ReadFile(filepath.Join(i.Store, record.Manifest.ID, "package", "dist", "main.mjs"))
	if err != nil || len(b) == 0 {
		t.Fatal("prior install lost", err)
	}
	if err = i.Remove("../outside"); err == nil {
		t.Fatal("unsafe remove")
	}
	if err = i.Remove(record.Manifest.ID); err != nil {
		t.Fatal(err)
	}
	records, err = i.List()
	if err != nil || len(records) != 0 {
		t.Fatal(records, err)
	}
}
func TestRejectArchivesAndRollback(t *testing.T) {
	cases := map[string][]item{
		"traversal":        append(validItems(), item{name: "root/../../escape", body: "bad"}),
		"absolute":         append(validItems(), item{name: "/root/escape", body: "bad"}),
		"backslash":        append(validItems(), item{name: `root/a\b`, body: "bad"}),
		"symlink":          append(validItems(), item{name: "root/link", kind: tar.TypeSymlink, link: "/tmp"}),
		"hardlink":         append(validItems(), item{name: "root/link", kind: tar.TypeLink, link: "root/dist/main.mjs"}),
		"fifo":             append(validItems(), item{name: "root/fifo", kind: tar.TypeFifo}),
		"duplicate":        append(validItems(), validItems()[1]),
		"multiple-root":    append(validItems(), item{name: "other/file", body: "bad"}),
		"unknown-manifest": {{name: "root/proxykit-extension.json", body: `{"manifestVersion":1,"id":"test","version":"1.0.0","entry":"main.mjs","scripts":{}}`}, {name: "root/main.mjs", body: ""}},
		"duplicate-key":    {{name: "root/proxykit-extension.json", body: `{"manifestVersion":1,"id":"test","id":"other","version":"1.0.0","entry":"main.mjs"}`}, {name: "root/main.mjs", body: ""}},
		"missing-entry":    {{name: "root/proxykit-extension.json", body: `{"manifestVersion":1,"id":"test","version":"1.0.0","entry":"missing.mjs"}`}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			i, _ := fixture(t, archive(t, entries))
			if _, err := i.Install(context.Background(), "https://github.com/example/repository", true); err == nil {
				t.Fatal("accepted bad archive")
			}
			es, err := os.ReadDir(i.Store)
			if err != nil || len(es) != 1 || es[0].Name() != ".owner-lock" {
				t.Fatalf("stage/lock leaked: %v %v", es, err)
			}
		})
	}
}
func TestBoundsAndCancellation(t *testing.T) {
	for _, kind := range []string{"download", "extracted", "files", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			i, _ := fixture(t, archive(t, validItems()))
			ctx := context.Background()
			switch kind {
			case "download":
				i.MaxDownload = 8
			case "extracted":
				i.MaxExtracted = 8
			case "files":
				i.MaxFiles = 1
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := i.Install(ctx, "https://github.com/example/repository", true); err == nil {
				t.Fatal("bound ignored")
			}
		})
	}
}
func TestSourceAndManifestValidation(t *testing.T) {
	for _, s := range []string{"http://github.com/a/b", "https://github.com.evil/a/b", "https://user@github.com/a/b", "https://github.com/a/b/tree/feature/slash", "https://github.com/a/b/tree/%2e%2e", "https://github.com/a/b?token=x", "https://github.com/a/b#fragment", "https://github.com/a/../tree/main"} {
		if _, err := ParseSource(s); err == nil {
			t.Errorf("accepted %s", s)
		}
	}
	for _, m := range []Manifest{{1, 1, "../bad", "1.0.0", "main.mjs"}, {2, 1, "test", "1.0.0", "main.mjs"}, {1, 1, "test", "latest", "main.mjs"}, {1, 1, "test", "1.0.0", "../main.mjs"}, {1, 1, "test", "1.0.0", "C:/main.mjs"}, {1, 1, "test", "1.0.0", "main.cts"}} {
		if err := m.Validate(); err == nil {
			t.Errorf("accepted %+v", m)
		}
	}
}
func TestSymlinkStoreAndRemove(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "store")); err != nil {
		t.Fatal(err)
	}
	i := Installer{Store: filepath.Join(dir, "store")}
	if err := i.Remove("example-user"); err == nil {
		t.Fatal("symlink store accepted")
	}
	i.Store = dir
	if err := os.Symlink(outside, filepath.Join(dir, "example-user")); err != nil {
		t.Fatal(err)
	}
	if err := i.Remove("example-user"); err == nil {
		t.Fatal("symlink remove accepted")
	}
	if _, err := i.List(); err == nil {
		t.Fatal("symlink list accepted")
	}
}
