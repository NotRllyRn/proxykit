package nodehost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/worker"
)

func awaitRuntimeResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(4 * time.Second):
		t.Fatal("runtime operation did not finish")
		return nil
	}
}

func TestRuntimePreparationReservesDrainingReuse(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprintf("abort=%v", abort), func(t *testing.T) {
			store, dir := t.TempDir(), t.TempDir()
			started, release, stopped := filepath.Join(dir, "started"), filepath.Join(dir, "release"), filepath.Join(dir, "stopped")
			installFixture(t, store, "stateful", fmt.Sprintf(`import fs from 'node:fs';export default api=>{api.cleanup(()=>fs.writeFileSync(%q,'stopped'));api.on({id:'pid',stage:'responseStart',bodyMode:'none'},i=>({action:'replace',message:{...i.response,header:{...i.response.header,'X-Pid':[String(process.pid)]}}}));};`, stopped))
			m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }), 3*time.Second, 4)
			_, headers := getRuntime(t, gateway.URL)
			pid := headers.Get("X-Pid")
			req, _ := http.NewRequest("GET", gateway.URL, nil)
			_, _, unpin, err := m.Acquire(req)
			if err != nil {
				t.Fatal(err)
			}
			defer unpin()
			if err = m.Change(context.Background(), "stateful", false); err != nil {
				t.Fatal(err)
			}
			// Seed the complete candidate with one additional setup-gated installation.
			// This fixture-only state write lets preparation deterministically pause after
			// selecting the draining worker, without adding production scheduling hooks.
			installFixture(t, store, "slow", fmt.Sprintf(`import fs from 'node:fs';export default async api=>{fs.writeFileSync(%q,'ready');while(!fs.existsSync(%q))await new Promise(r=>setTimeout(r,5));if(%v)throw Error('deliberate setup failure');};`, started, release, abort))
			result := make(chan error, 1)
			go func() { result <- m.Change(context.Background(), "stateful", true) }()
			waitFile(t, started)
			m.mu.Lock()
			live := m.instances["stateful"]
			reserved := live != nil && live.preparing && !live.stopping
			m.mu.Unlock()
			if !reserved {
				t.Fatal("reused worker was not reserved during preparation")
			}
			unpin()
			waitRetired(t, m)
			if err = live.worker.Health(); err != nil {
				t.Fatalf("retirement stopped reserved worker: %v", err)
			}
			if _, err = os.Stat(stopped); !os.IsNotExist(err) {
				t.Fatalf("reserved worker cleaned up: %v", err)
			}
			if err = os.WriteFile(release, []byte("go"), 0600); err != nil {
				t.Fatal(err)
			}
			err = awaitRuntimeResult(t, result)
			if abort {
				if err == nil {
					t.Fatal("setup failure published")
				}
				waitFile(t, stopped)
				if !live.worker.Reaped() {
					t.Fatal("aborted reservation retained orphan worker")
				}
				m.mu.Lock()
				same := m.instances["stateful"] == live
				m.mu.Unlock()
				if same {
					t.Fatal("aborted reservation retained registry ownership after reaping")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				waitRetired(t, m)
				_, next := getRuntime(t, gateway.URL)
				if next.Get("X-Pid") != pid {
					t.Fatalf("published worker changed: %q -> %q", pid, next.Get("X-Pid"))
				}
				m.mu.Lock()
				same := m.instances["stateful"] == live && !live.preparing && !live.stopping
				m.mu.Unlock()
				if !same || live.worker.Health() != nil {
					t.Fatal("published worker lost ownership or stopped")
				}
			}
		})
	}
}

func TestRuntimeCloseCancelsSetupAndRejectsPublication(t *testing.T) {
	store, dir := t.TempDir(), t.TempDir()
	started := filepath.Join(dir, "started")
	installFixture(t, store, "slow", fmt.Sprintf(`import fs from 'node:fs';export default async api=>{fs.writeFileSync(%q,'ready');await new Promise(()=>{});};`, started))
	disableFixture(t, store, "slow")
	m, _ := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), time.Second, 4)
	before, err := m.installer.State()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", "http://example.test", nil)
	_, _, unpin, err := m.Acquire(req)
	if err != nil {
		t.Fatal(err)
	}
	defer unpin()
	result := make(chan error, 1)
	go func() { result <- m.Change(context.Background(), "slow", true) }()
	waitFile(t, started)
	m.mu.Lock()
	live := m.instances["slow"]
	m.mu.Unlock()
	// An already-expired caller deadline must not wait for setup/teardown.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	begin := time.Now()
	if err = m.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close: %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatal("Close blocked behind setup")
	}
	unpin()
	if err = awaitRuntimeResult(t, result); err == nil {
		t.Fatal("setup published after shutdown")
	}
	finish, done := context.WithTimeout(context.Background(), 4*time.Second)
	defer done()
	if err = m.Close(finish); err != nil {
		t.Fatal(err)
	}
	after, err := m.installer.State()
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != before.Revision {
		t.Fatal("shutdown candidate persisted")
	}
	if live == nil || !live.worker.Reaped() {
		t.Fatal("setup worker not reaped")
	}
	assertRuntimeClosed(t, m)
}

// A blocked os/exec stderr copier deterministically retains process reaping
// after bounded Stop has returned, without relying on OS scheduling delays.
type runtimeBlockedStderr struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *runtimeBlockedStderr) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(p), nil
}

func TestRuntimeCloseRetainsAbortedWorkerUntilReaped(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "slow", `export default async api=>{process.stderr.write('hold reaping');await new Promise(()=>{});};`)
	disableFixture(t, store, "slow")
	stderr := &runtimeBlockedStderr{started: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(stderr.release) }) }
	defer release()
	m, err := OpenRuntime(context.Background(), RuntimeOptions{Options: Options{Store: store, Gateway: "http://example.test", Worker: worker.Options{Stderr: stderr, ShutdownTimeout: 10 * time.Millisecond}}, Drain: time.Second, Cleanup: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		release()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = m.Close(ctx)
	})
	result := make(chan error, 1)
	go func() { result <- m.Change(context.Background(), "slow", true) }()
	select {
	case <-stderr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("setup did not write stderr")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err = m.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close: %v", err)
	}
	if err = awaitRuntimeResult(t, result); err == nil {
		t.Fatal("setup published during shutdown")
	}
	m.mu.Lock()
	live := m.instances["slow"]
	m.mu.Unlock()
	if live == nil || !live.stopping || live.worker.Reaped() {
		t.Fatal("aborted worker lost retained reaping ownership")
	}
	select {
	case <-m.shutdownDone:
		t.Fatal("shutdown completed before worker reaping")
	default:
	}
	release()
	finish, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	if err = m.Close(finish); errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("shutdown did not finish after reaping")
	}
	if !live.worker.Reaped() {
		t.Fatal("shutdown completed before reaping")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.instances) != 0 {
		t.Fatal("reaped worker still owns registry")
	}
}

type runtimeRoundTripper func(*http.Request) (*http.Response, error)

func (f runtimeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func assertRuntimeClosed(t *testing.T, m *Runtime) {
	t.Helper()
	if err := m.Change(context.Background(), "example", true); !errors.Is(err, extension.ErrClosed) {
		t.Fatalf("Change after Close: %v", err)
	}
	if _, err := m.Install(context.Background(), "https://github.com/example/repository", true); !errors.Is(err, extension.ErrClosed) {
		t.Fatalf("Install after Close: %v", err)
	}
	if err := m.Remove("example"); !errors.Is(err, extension.ErrClosed) {
		t.Fatalf("Remove after Close: %v", err)
	}
}

func TestRuntimeCloseDeadlineDuringInstallRetainsTeardown(t *testing.T) {
	m, _ := runtimeProxy(t, t.TempDir(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), time.Second, 4)
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	m.installer.Client = &http.Client{Transport: runtimeRoundTripper(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(canceled)
		<-release // Deliberately delay settlement even after observing cancellation.
		return nil, r.Context().Err()
	})}
	result := make(chan error, 1)
	go func() {
		_, err := m.Install(context.Background(), "https://github.com/example/repository", true)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("install did not start")
	}
	queued, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := m.Change(queued, "example", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued admission: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	begin := time.Now()
	if err := m.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close: %v", err)
	}
	if time.Since(begin) > time.Second {
		t.Fatal("Close blocked behind installer")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("install was not canceled")
	}
	assertRuntimeClosed(t, m)
	select {
	case <-m.shutdownDone:
		t.Fatal("teardown completed before installer settled")
	default:
	}
	// End the deliberately retained transport, then observe eventual teardown.
	release <- struct{}{}
	if err := awaitRuntimeResult(t, result); err == nil {
		t.Fatal("canceled install succeeded")
	}
	finish, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	if err := m.Close(finish); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil || len(m.instances) != 0 || len(m.draining) != 0 {
		t.Fatal("shutdown retained runtime resources")
	}
}
