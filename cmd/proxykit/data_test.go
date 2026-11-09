package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extensions"
)

func TestDataDirCommandBeforeInstall(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store")
	i := extensions.Installer{Store: store}
	var out bytes.Buffer
	if err := runExtensionWithInstaller(context.Background(), []string{"data-dir", "example"}, &out, i); err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSuffix(out.String(), "\n")
	if path != filepath.Join(store, ".data", "example") || !filepath.IsAbs(path) {
		t.Fatal(out.String())
	}
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		t.Fatalf("%v %v", st, err)
	}
	if _, err = os.Stat(filepath.Join(store, "example")); !os.IsNotExist(err) {
		t.Fatal("data-dir must not install anything", err)
	}
	for _, args := range [][]string{{"data-dir"}, {"data-dir", "example", "extra"}, {"data-dir", "../escape"}, {"data-dir", "Example"}} {
		out.Reset()
		if err = runExtensionWithInstaller(context.Background(), args, &out, i); err == nil || out.Len() != 0 {
			t.Fatalf("accepted %v: %v %q", args, err, out.String())
		}
	}
}
