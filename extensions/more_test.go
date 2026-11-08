package extensions

import (
	"archive/tar"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedReplacementKeepsInstalledPackage(t *testing.T) {
	good, _ := fixture(t, archive(t, validItems()))
	ctx := context.Background()
	record, err := good.Install(ctx, "https://github.com/example/repository", true)
	if err != nil {
		t.Fatal(err)
	}
	bad, _ := fixture(t, archive(t, append(validItems(), item{name: "root/link", kind: tar.TypeSymlink, link: "/tmp"})))
	bad.Store = good.Store
	if _, err = bad.Install(ctx, record.Source, true); err == nil {
		t.Fatal("bad replacement succeeded")
	}
	list, err := good.List()
	if err != nil || len(list) != 1 || list[0] != record {
		t.Fatal("prior provenance changed", list, err)
	}
	if _, err = os.Stat(filepath.Join(good.Store, record.Manifest.ID, "package", "dist", "main.mjs")); err != nil {
		t.Fatal("prior entry removed", err)
	}
	entries, err := os.ReadDir(good.Store)
	if err != nil || len(entries) != 1 {
		t.Fatal("stages or locks leaked", err)
	}
}
func TestTruncatedArchiveAndExpansionTail(t *testing.T) {
	data := archive(t, validItems())
	for name, b := range map[string][]byte{"truncated-gzip": data[:len(data)-5], "truncated-tar": data[:len(data)/2]} {
		t.Run(name, func(t *testing.T) {
			i, _ := fixture(t, b)
			if _, err := i.Install(context.Background(), "https://github.com/example/repository", true); err == nil {
				t.Fatal("truncated archive accepted")
			}
		})
	}
}
func TestBadResolutionAndHTTPStatus(t *testing.T) {
	for _, response := range []string{`{"sha":"main"}`, `{"sha":"` + strings.Repeat("x", 40) + `"}`, `not JSON`, "status"} {
		t.Run(response, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if response == "status" {
					w.WriteHeader(503)
					return
				}
				fmt.Fprint(w, response)
			}))
			defer server.Close()
			i := Installer{Store: t.TempDir(), APIBase: server.URL, ArchiveBase: server.URL}
			if _, err := i.Install(context.Background(), "https://github.com/example/repository", true); err == nil {
				t.Fatal("invalid resolution accepted")
			}
		})
	}
}
func TestStrictSemanticVersions(t *testing.T) {
	for _, v := range []string{"01.0.0", "1.01.0", "1.0.00", "1.0.0-01", "1.0.0-..", "1.0.0+a..b", "1.0.0-", "1.0.0+"} {
		if validVersion(v) {
			t.Errorf("accepted %s", v)
		}
	}
	for _, v := range []string{"0.0.0", "1.2.3-alpha.1+build.01", "1.2.3-rc-1"} {
		if !validVersion(v) {
			t.Errorf("rejected %s", v)
		}
	}
}
