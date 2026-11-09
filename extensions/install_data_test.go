package extensions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallInitializesPersistentData(t *testing.T) {
	i, _ := fixture(t, archive(t, validItems()))
	ctx := context.Background()
	source := "https://github.com/example/repository"
	for attempt := 0; attempt < 2; attempt++ {
		record, err := i.Install(ctx, source, true)
		if err != nil {
			t.Fatal(err)
		}
		data := filepath.Join(i.Store, ".data", record.Manifest.ID)
		for _, path := range []string{filepath.Dir(data), data} {
			st, err := os.Lstat(path)
			if err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
				t.Fatalf("install did not create private directory %s: %v %v", path, st, err)
			}
		}
		config := filepath.Join(data, "config.json")
		if attempt == 0 {
			if err = os.WriteFile(config, []byte("persistent"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err = i.Remove(record.Manifest.ID); err != nil {
			t.Fatal(err)
		}
		if b, err := os.ReadFile(config); err != nil || string(b) != "persistent" {
			t.Fatalf("remove/reinstall lost data: %q %v", b, err)
		}
	}
}

func TestInstallStateFailureRetainsData(t *testing.T) {
	i, _ := fixture(t, archive(t, validItems()))
	data, err := i.DataDir("example-user")
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(data, "config.json")
	if err = os.WriteFile(config, []byte("persistent"), 0600); err != nil {
		t.Fatal(err)
	}
	// An invalid state file fails the commit after storage preparation.
	if err = os.Mkdir(filepath.Join(i.Store, ".enabled.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = i.Install(context.Background(), "https://github.com/example/repository", true); err == nil {
		t.Fatal("state failure reported success")
	}
	if _, err = os.Stat(filepath.Join(i.Store, "example-user")); !os.IsNotExist(err) {
		t.Fatal("failed install left package", err)
	}
	if b, err := os.ReadFile(config); err != nil || string(b) != "persistent" {
		t.Fatalf("failed install lost data: %q %v", b, err)
	}
}

func TestInstallDataFailureDoesNotPublish(t *testing.T) {
	for _, component := range []string{".data", ".data/example-user"} {
		for _, kind := range []string{"symlink", "file", "public"} {
			t.Run(component+"/"+kind, func(t *testing.T) {
				i, _ := fixture(t, archive(t, validItems()))
				path := filepath.Join(i.Store, component)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink(t.TempDir(), path)
				case "file":
					err = os.WriteFile(path, []byte("preserve"), 0600)
				case "public":
					err = os.Mkdir(path, 0700)
					if err == nil {
						err = os.Chmod(path, 0755)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err = i.Install(context.Background(), "https://github.com/example/repository", true); err == nil {
					t.Fatal("unsafe storage accepted")
				}
				if _, err = os.Stat(filepath.Join(i.Store, "example-user")); !os.IsNotExist(err) {
					t.Fatal("failed install published package", err)
				}
				state, err := i.State()
				if err != nil || state.Revision != 0 || len(state.Decisions) != 0 {
					t.Fatalf("failed install changed intent: %+v %v", state, err)
				}
				if _, err = os.Lstat(path); err != nil {
					t.Fatal("failed install removed storage", err)
				}
			})
		}
	}
}
