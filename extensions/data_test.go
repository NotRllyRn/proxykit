package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirIdentityAndValidation(t *testing.T) {
	i := Installer{Store: filepath.Join(t.TempDir(), "store")}
	a, err := i.DataDir("example-one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := i.DataDir("example-two")
	if err != nil || a == b || !filepath.IsAbs(a) {
		t.Fatalf("paths %q %q: %v", a, b, err)
	}
	if err = os.WriteFile(filepath.Join(a, "config.json"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if again, err := i.DataDir("example-one"); err != nil || again != a {
		t.Fatalf("stable path %q: %v", again, err)
	}
	for _, id := range []string{"", "../escape", "Example", "a/b", ".data"} {
		if _, err := i.DataDir(id); err == nil {
			t.Fatalf("accepted ID %q", id)
		}
	}
}

func TestDataDirResolvesRelativeStore(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	store, err := filepath.Rel(cwd, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store = filepath.Join(store, "store")
	path, err := (Installer{Store: store}).DataDir("example")
	if err != nil || !filepath.IsAbs(path) {
		t.Fatalf("%q %v", path, err)
	}
}

func TestDataDirRejectsUnsafePaths(t *testing.T) {
	for _, component := range []string{"", ".data", ".data/example"} {
		for _, kind := range []string{"symlink", "file", "public"} {
			t.Run(component+"/"+kind, func(t *testing.T) {
				store := filepath.Join(t.TempDir(), "store")
				path := filepath.Join(store, component)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink(t.TempDir(), path)
				case "file":
					err = os.WriteFile(path, nil, 0600)
				case "public":
					err = os.Mkdir(path, 0700)
					if err == nil {
						err = os.Chmod(path, 0755)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err = (Installer{Store: store}).DataDir("example"); err == nil {
					t.Fatal("accepted unsafe path")
				}
			})
		}
	}
}
