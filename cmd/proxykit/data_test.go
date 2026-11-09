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

func TestDataDirCommandRejected(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store")
	i := extensions.Installer{Store: store}
	for _, args := range [][]string{{"data-dir"}, {"data-dir", "example"}, {"data-dir", "example", "extra"}} {
		var out bytes.Buffer
		if err := runExtensionWithInstaller(context.Background(), args, &out, i); err == nil || err.Error() != "unknown extension command" || out.Len() != 0 {
			t.Fatalf("accepted %v: %v %q", args, err, out.String())
		}
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatal("rejected command touched store", err)
	}
	if err := runExtensionWithInstaller(context.Background(), nil, &bytes.Buffer{}, i); err == nil || strings.Contains(err.Error(), "data-dir") {
		t.Fatal("removed command advertised", err)
	}
}
