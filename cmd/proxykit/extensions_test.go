package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extensions"
)

func TestExtensionCLIInstallListRemoveFixture(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"root/proxykit-extension.json": `{"manifestVersion":1,"id":"example-user","version":"1.0.0","entry":"main.mjs"}`, "root/main.mjs": `throw new Error('must not execute while installing');`} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			fmt.Fprintf(w, `{"sha":%q}`, sha)
		} else {
			w.Write(archive.Bytes())
		}
	}))
	defer server.Close()
	store := filepath.Join(t.TempDir(), "extensions")
	i := extensions.Installer{Store: store, APIBase: server.URL, ArchiveBase: server.URL}
	ctx := context.Background()
	var out bytes.Buffer
	if err := runExtensionWithInstaller(ctx, []string{"install", "https://github.com/example/repository"}, &out, i); err == nil {
		t.Fatal("implicit trust")
	}
	if err := runExtensionWithInstaller(ctx, []string{"install", "https://github.com/example/repository", "--trust"}, &out, i); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Activation is NOT yet wired") || !strings.Contains(out.String(), sha) {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := runExtensionWithInstaller(ctx, []string{"list"}, &out, i); err != nil {
		t.Fatal(err)
	}
	var list []extensions.Installation
	if err := json.Unmarshal(out.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatal(out.String(), err)
	}
	t.Setenv("PROXYKIT_EXTENSIONS_DIR", store)
	if err := run(ctx, []string{"-upstream", "http://127.0.0.1:1"}, &out); err == nil || !strings.Contains(err.Error(), "refusing to serve") {
		t.Fatal("silently ignored installed extension", err)
	}
	if err := runExtensionWithInstaller(ctx, []string{"remove", "../bad"}, &out, i); err == nil {
		t.Fatal("unsafe remove")
	}
	if err := run(ctx, []string{"extension", "remove", "example-user"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(ctx, []string{"extension", "list"}, &out); err != nil || out.String() != "[]\n" {
		t.Fatal(out.String(), err)
	}
}
func TestExtensionCLIArgumentsAndNoStore(t *testing.T) {
	t.Setenv("PROXYKIT_EXTENSIONS_DIR", filepath.Join(t.TempDir(), "missing"))
	for _, args := range [][]string{{"extension"}, {"extension", "unknown"}, {"extension", "list", "extra"}, {"extension", "remove"}, {"extension", "install", "--trust"}, {"extension", "install", "https://github.com/a/b", "--trust", "--trust"}} {
		if err := run(context.Background(), args, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := run(context.Background(), []string{"-help"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}
