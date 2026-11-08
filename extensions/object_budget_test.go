package extensions

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImplicitDirectoryObjectBudget(t *testing.T) {
	for _, depth := range []int{3, 100} {
		dest := t.TempDir()
		data := archive(t, []item{{name: "root/" + strings.Repeat("a/", depth) + "main.mjs", body: "export default ()=>{}"}})
		i := Installer{MaxFiles: 2, MaxExtracted: 1024}
		err := i.extract(context.Background(), data, dest)
		if err == nil || !strings.Contains(err.Error(), "object count") {
			t.Fatalf("depth %d: %v", depth, err)
		}
		entries, _ := os.ReadDir(dest)
		if len(entries) != 0 {
			t.Fatal("created parents before budget check")
		}
	}
}
func TestObjectBudgetCountsEachParentOnce(t *testing.T) {
	data := archive(t, []item{
		{name: "root/dist/a.mjs", body: "a"},
		{name: "root/dist", kind: tar.TypeDir}, // explicit header after implicit parent
		{name: "root/dist/b.mjs", body: "b"},
	})
	i := Installer{MaxFiles: 3, MaxExtracted: 1024}
	dest := t.TempDir()
	if err := i.extract(context.Background(), data, dest); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := filepath.WalkDir(dest, func(p string, d os.DirEntry, err error) error {
		if p != dest {
			count++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("created %d objects", count)
	}
	i.MaxFiles = 2
	if err := i.extract(context.Background(), archive(t, []item{{name: "root/dist/a.mjs", body: "a"}, {name: "root/dist/b.mjs", body: "b"}}), t.TempDir()); err == nil {
		t.Fatal("object overflow accepted")
	}
}
