package extensions

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManifestAPIContract(t *testing.T) {
	for _, version := range []int{0, 1, 2, -1} {
		m := Manifest{ManifestVersion: 1, APIVersion: version, ID: "example-user", Version: "1.0.0", Entry: "main.mjs"}
		e := m.Validate()
		if version == 1 && e != nil {
			t.Fatal(e)
		}
		if version != 1 && (e == nil || !strings.Contains(e.Error(), "apiVersion")) {
			t.Fatalf("version %d: %v", version, e)
		}
	}
	var m Manifest
	if e := strict([]byte(`{"manifestVersion":1,"apiVersion":"1","id":"example-user","version":"1.0.0","entry":"main.mjs"}`), &m); e == nil {
		t.Fatal("string API accepted")
	}
}
func TestLegacyAndIncompatibleInventoryFailClosed(t *testing.T) {
	store := t.TempDir()
	id := "example-user"
	path := filepath.Join(store, id)
	if e := os.MkdirAll(filepath.Join(path, "package"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(path, "package", "main.mjs"), []byte("throw Error('must never execute')"), 0600); e != nil {
		t.Fatal(e)
	}
	record := Installation{Manifest: Manifest{ManifestVersion: 1, ID: id, Version: "1.0.0", Entry: "main.mjs"}, Source: "https://github.com/example/repository", Commit: strings.Repeat("a", 40)}
	b, _ := json.Marshal(record)
	if e := os.WriteFile(filepath.Join(path, "installation.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	i := Installer{Store: store}
	s, e := i.State()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = i.Enabled(s); e == nil || !strings.Contains(e.Error(), "legacy") {
		t.Fatal(e)
	}
	rows, e := i.Inventory()
	if e != nil || len(rows) != 1 || !strings.Contains(rows[0].Diagnostic, "apiVersion") {
		t.Fatal(rows, e)
	}
	if _, e = i.Decide(s, id, true); e == nil {
		t.Fatal("incompatible record enabled")
	}
	s, e = i.Decide(s, id, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = i.SaveState(s); e != nil {
		t.Fatal(e)
	}
	records, e := i.Enabled(s)
	if e != nil || len(records) != 0 {
		t.Fatal(records, e)
	}
	// A changed commit never inherits an old decision, even a disabled one.
	record.Commit = strings.Repeat("b", 40)
	b, _ = json.Marshal(record)
	os.WriteFile(filepath.Join(path, "installation.json"), b, 0600)
	if _, e = i.Enabled(s); e == nil {
		t.Fatal("changed installation inherited decision")
	}
}

func TestRemoveInvalidatesStaleAuthorizationBeforeReinstall(t *testing.T) {
	store := t.TempDir()
	if e := os.Chmod(store, 0700); e != nil {
		t.Fatal(e)
	}
	id := "example-user"
	p := filepath.Join(store, id)
	makeRecord := func() {
		t.Helper()
		os.MkdirAll(filepath.Join(p, "package"), 0700)
		os.WriteFile(filepath.Join(p, "package", "main.mjs"), []byte("export default ()=>{}"), 0600)
		r := Installation{Manifest: Manifest{ManifestVersion: 1, APIVersion: 1, ID: id, Version: "1.0.0", Entry: "main.mjs"}, Source: "https://github.com/example/repository", Commit: strings.Repeat("a", 40)}
		b, _ := json.Marshal(r)
		if e := os.WriteFile(filepath.Join(p, "installation.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	makeRecord()
	i := Installer{Store: store}
	s, _ := i.State()
	s, e := i.Decide(s, id, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = i.SaveState(s); e != nil {
		t.Fatal(e)
	}
	if e = i.Remove(id); e != nil {
		t.Fatal(e)
	}
	// Simulate process failure after reinstalled package rename, before its state
	// write. An earlier true decision must not resurrect the same immutable commit.
	makeRecord()
	s, e = i.State()
	if e != nil {
		t.Fatal(e)
	}
	records, e := i.Enabled(s)
	if e != nil || len(records) != 0 {
		t.Fatal("stale enable resurrected", records, e)
	}
}

func TestStoreLockHelper(t *testing.T) {
	store := os.Getenv("PROXYKIT_TEST_LOCK_STORE")
	if store == "" {
		return
	}
	unlock, e := Lock(store)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	fmt.Println("LOCKED")
	select {}
}
func TestStoreOwnershipSurvivesConcurrentCommandsAndCrash(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("Unix ownership contract")
	}
	store := t.TempDir()
	if e := os.Chmod(store, 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreLockHelper$")
	cmd.Env = append(os.Environ(), "PROXYKIT_TEST_LOCK_STORE="+store)
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "LOCKED" {
		t.Fatal("child did not acquire lock")
	}
	if _, e = Lock(store); !errors.Is(e, ErrStoreBusy) {
		t.Fatal("second owner admitted", e)
	}
	if e = (Installer{Store: store}).Remove("example-user"); !errors.Is(e, ErrStoreBusy) {
		t.Fatal("offline mutation admitted", e)
	}
	st, e := os.Stat(filepath.Join(store, ".owner-lock"))
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	cmd.Wait()
	unlock, e := Lock(store)
	if e != nil {
		t.Fatal("crashed owner retained lock", e)
	}
	defer unlock()
	now, e := os.Stat(filepath.Join(store, ".owner-lock"))
	if e != nil || !os.SameFile(st, now) {
		t.Fatal("ownership split by unlink", e)
	}
}
