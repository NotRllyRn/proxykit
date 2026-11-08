package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extensions"
)

// Blocking only worker stderr lets run return (and the real main potentially
// block logging its error) while os/exec still cannot finish reaping the worker.
type shutdownOutput struct {
	started, listening, release chan struct{}
	workerOnce, listenOnce      sync.Once
}

func (w *shutdownOutput) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("hold reaping")) {
		w.workerOnce.Do(func() { close(w.started) })
		<-w.release
	}
	if bytes.Contains(p, []byte("listening on")) {
		w.listenOnce.Do(func() { close(w.listening) })
	}
	return len(p), nil
}

func TestRunRetainsStoreLockUntilRuntimeCleanup(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	store := filepath.Join(t.TempDir(), "extensions")
	t.Setenv("PROXYKIT_EXTENSIONS_DIR", store)
	dir := filepath.Join(store, "example-user", "package")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := extensions.Manifest{ManifestVersion: 1, APIVersion: 1, ID: "example-user", Version: "1.0.0", Entry: "main.mjs"}
	m, _ := json.Marshal(manifest)
	commit := strings.Repeat("a", 40)
	record, _ := json.Marshal(extensions.Installation{Manifest: manifest, Source: "https://github.com/example/repository", Commit: commit})
	for path, data := range map[string][]byte{
		filepath.Join(dir, "main.mjs"):                            []byte(`export default api=>{process.stderr.write('hold reaping');api.cleanup(()=>{});};`),
		filepath.Join(dir, "proxykit-extension.json"):             m,
		filepath.Join(store, "example-user", "installation.json"): record,
	} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	installer := extensions.Installer{Store: store}
	state, err := installer.State()
	if err != nil {
		t.Fatal(err)
	}
	state.Decisions[manifest.ID] = extensions.Decision{Commit: commit, Enabled: true}
	if err := installer.SaveState(state); err != nil {
		t.Fatal(err)
	}
	output := &shutdownOutput{started: make(chan struct{}), listening: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(output.release) }) }
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, []string{"-listen", "127.0.0.1:0", "-upstream", "http://example.test", "-extension-drain", "10ms", "-extension-cleanup", "10ms"}, output)
	}()
	for _, ready := range []chan struct{}{output.started, output.listening} {
		select {
		case <-ready:
		case err := <-result:
			t.Fatalf("run exited before ready: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("startup timed out")
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("bounded shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not respect shutdown budget")
	}
	if unlock, err := extensions.Lock(store); err == nil {
		unlock()
		t.Fatal("run released store lock before actual runtime cleanup")
	} else if !errors.Is(err, extensions.ErrStoreBusy) {
		t.Fatalf("lock: %v", err)
	}
	// Completion, not process exit, must release the transferred lock so using
	// run in tests or an embedding process does not permanently leak ownership.
	release()
	deadline := time.After(5 * time.Second)
	for {
		if unlock, err := extensions.Lock(store); err == nil {
			unlock()
			break
		} else if !errors.Is(err, extensions.ErrStoreBusy) {
			t.Fatalf("lock after cleanup: %v", err)
		}
		select {
		case <-deadline:
			t.Fatal("store lock retained after runtime completion")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
