package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestWorker(t *testing.T, source string, adjust func(*Options)) *Worker {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	entry := filepath.Join(t.TempDir(), "extension.mjs")
	if err := os.WriteFile(entry, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	o := Options{Entry: entry, CallTimeout: 2 * time.Second, StartupTimeout: 2 * time.Second, CancelGrace: time.Second, ShutdownTimeout: time.Second}
	if adjust != nil {
		adjust(&o)
	}
	w, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = w.Stop(ctx)
	})
	return w
}

const echo = `export default api => { api.on({id:'echo',stage:'observe',bodyMode:'none'}, async input => { if(input.delay) await new Promise(r=>setTimeout(r,input.delay)); return input; }); };`

func start(t *testing.T, w *Worker) {
	t.Helper()
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func invoke(t *testing.T, w *Worker, input string) string {
	t.Helper()
	b, err := w.Invoke(context.Background(), "echo", json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestSetupHooksConcurrentCorrelationCleanupRestart(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleanup")
	var logs bytes.Buffer
	source := fmt.Sprintf(`import {writeFileSync} from 'node:fs'; export default api=>{ console.log('setup log'); console.table([{label:'Example user'}]); console.count('setup'); api.on({id:'echo',stage:'observe',bodyMode:'none',order:2},async input=>{await new Promise(r=>setTimeout(r,input.delay)); return {value:input.value,config:api.config};});api.cleanup(()=>writeFileSync(%q,'done')); };`, marker)
	w := newTestWorker(t, source, func(o *Options) {
		o.Config = json.RawMessage(`{"label":"Work laptop"}`)
		o.Stderr = &logs
		o.Concurrency = 8
	})
	start(t, w)
	hooks := w.Hooks()
	if len(hooks) != 1 || hooks[0].Order != 2 {
		t.Fatal(hooks)
	}
	hooks[0].ID = "mutated"
	if w.Hooks()[0].ID != "echo" {
		t.Fatal("mutable hooks")
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			b, err := w.Invoke(context.Background(), "echo", json.RawMessage(fmt.Sprintf(`{"value":%d,"delay":%d}`, n, (8-n)*5)))
			if err != nil {
				t.Error(err)
				return
			}
			var out struct {
				Value  int `json:"value"`
				Config struct {
					Label string `json:"label"`
				} `json:"config"`
			}
			if err = json.Unmarshal(b, &out); err != nil || out.Value != n || out.Config.Label != "Work laptop" {
				t.Errorf("wrong correlation %s %v", b, err)
			}
		}(n)
	}
	wg.Wait()
	if err := w.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "done" {
		t.Fatal("cleanup missing", err)
	}
	if !strings.Contains(logs.String(), "setup log") {
		t.Fatal("missing stderr logs")
	}
	start(t, w)
	if err := w.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestCancellationRetainsCapacityUntilSettlement(t *testing.T) {
	w := newTestWorker(t, echo, func(o *Options) { o.Concurrency = 1 })
	start(t, w)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := w.Invoke(ctx, "echo", json.RawMessage(`{"delay":250}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err = w.Invoke(context.Background(), "echo", json.RawMessage(`{}`)); !errors.Is(err, ErrBusy) {
		t.Fatalf("cancel released capacity: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err = w.Invoke(context.Background(), "echo", json.RawMessage(`{"value":"fresh"}`))
		if err == nil {
			break
		}
		if !errors.Is(err, ErrBusy) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestDefaultTimeoutKillsUncooperativeAndRestart(t *testing.T) {
	w := newTestWorker(t, `export default api=>api.on({id:'echo',stage:'observe',bodyMode:'none'},()=>new Promise(()=>{}));`, func(o *Options) {
		o.Concurrency = 1
		o.CallTimeout = 40 * time.Millisecond
		o.CancelGrace = 80 * time.Millisecond
	})
	start(t, w)
	_, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err = w.Invoke(context.Background(), "echo", json.RawMessage(`{}`)); !errors.Is(err, ErrBusy) {
		t.Fatal("capacity not retained", err)
	}
	select {
	case <-w.process().exited:
	case <-time.After(2 * time.Second):
		t.Fatal("uncooperative process not killed")
	}
	if err = w.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestCooperativeCancelAndAdmission(t *testing.T) {
	source := `export default api=>api.on({id:'echo',stage:'observe',bodyMode:'none'},async (input,{signal})=>{if(input.wait) await new Promise(resolve=>{if(signal.aborted) resolve();else signal.addEventListener('abort',resolve,{once:true})});return input;});`
	w := newTestWorker(t, source, nil)
	start(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := w.Invoke(ctx, "echo", json.RawMessage(`{"wait":true}`)); done <- err }()
	waitPending(t, w, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func waitPending(t *testing.T, w *Worker, count int) {
	t.Helper()
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		p := w.process()
		p.mu.Lock()
		n := len(p.pending)
		p.mu.Unlock()
		if n == count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending count did not arrive")
}
func TestProtocolFaultsFailPending(t *testing.T) {
	for name, code := range map[string]string{
		"crash":              `process.exit(23)`,
		"text-stdout":        `process.stdout.write('not a frame')`,
		"zero-length":        `process.stdout.write(Buffer.alloc(4))`,
		"oversize-length":    `process.stdout.write(Buffer.from([127,255,255,255]))`,
		"malformed-json":     `const b=Buffer.from('{');const h=Buffer.alloc(4);h.writeUInt32BE(b.length);process.stdout.write(Buffer.concat([h,b]))`,
		"unknown-version":    `const b=Buffer.from('{"v":2,"type":"result","id":"1","output":null}');const h=Buffer.alloc(4);h.writeUInt32BE(b.length);process.stdout.write(Buffer.concat([h,b]))`,
		"unmatched-reply":    `const b=Buffer.from('{"v":1,"type":"result","id":"999","output":null}');const h=Buffer.alloc(4);h.writeUInt32BE(b.length);process.stdout.write(Buffer.concat([h,b]))`,
		"duplicate-json-key": `const b=Buffer.from('{"v":1,"v":1,"type":"result","id":"1","output":null}');const h=Buffer.alloc(4);h.writeUInt32BE(b.length);process.stdout.write(Buffer.concat([h,b]))`,
		"unknown-field":      `const b=Buffer.from('{"v":1,"type":"result","id":"1","output":null,"extra":true}');const h=Buffer.alloc(4);h.writeUInt32BE(b.length);process.stdout.write(Buffer.concat([h,b]))`,
	} {
		t.Run(name, func(t *testing.T) {
			source := `export default api=>api.on({id:'echo',stage:'observe',bodyMode:'none'},()=>{` + code + `;return new Promise(()=>{});});`
			w := newTestWorker(t, source, nil)
			start(t, w)
			_, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{}`))
			if err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("protocol failure not actionable", err)
			}
			select {
			case <-w.process().exited:
			case <-time.After(time.Second):
				t.Fatal("not reaped")
			}
		})
	}
}
func TestStartupFailures(t *testing.T) {
	for name, source := range map[string]string{"throw": `throw new Error('setup failed')`, "missing-setup": `export const x=1`, "duplicate-hook": `export default api=>{api.on({id:'x',stage:'observe',bodyMode:'none'},()=>null);api.on({id:'x',stage:'observe',bodyMode:'none'},()=>null);}`, "bad-shape": `export default api=>api.on({id:'x',stage:'observe',bodyMode:'mystery'},()=>null)`, "timeout": `export default ()=>new Promise(()=>{})`} {
		t.Run(name, func(t *testing.T) {
			w := newTestWorker(t, source, func(o *Options) {
				if name == "timeout" {
					o.StartupTimeout = 100 * time.Millisecond
				}
			})
			if err := w.Start(context.Background()); err == nil {
				t.Fatal("bad startup accepted")
			}
		})
	}
}
func TestFrameBoundsAndHookError(t *testing.T) {
	w := newTestWorker(t, `export default api=>api.on({id:'echo',stage:'observe',bodyMode:'buffered'},input=>{if(input.large)return 'x'.repeat(4096);if(input.fail)throw new Error('expected');return input;});`, func(o *Options) { o.MaxFrame = 1024 })
	start(t, w)
	if _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`"`+strings.Repeat("x", 2048)+`"`)); err == nil {
		t.Fatal("oversize input accepted")
	}
	if _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{"large":true}`)); err == nil || !strings.Contains(err.Error(), "bound") {
		t.Fatal("oversize output accepted", err)
	}
	if _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{"fail":true}`)); err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatal(err)
	}
	if got := invoke(t, w, `{"ok":true}`); got != `{"ok":true}` {
		t.Fatal(got)
	}
}
func TestBoundedShutdown(t *testing.T) {
	w := newTestWorker(t, `export default api=>{api.on({id:'echo',stage:'observe',bodyMode:'none'},()=>null);api.cleanup(()=>new Promise(()=>{}));};`, func(o *Options) { o.ShutdownTimeout = 80 * time.Millisecond })
	start(t, w)
	if err := w.Stop(context.Background()); err == nil {
		t.Fatal("uncooperative cleanup reported graceful stop")
	}
	// Deadline returns promptly; reaping may finish asynchronously. The
	// process remains unavailable and cannot be replaced before reaping.
	if p := w.process(); p != nil {
		select {
		case <-p.exited:
		case <-time.After(time.Second):
			t.Fatal("not reaped")
		}
		_ = w.Stop(context.Background())
	}
	if _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{}`)); err == nil {
		t.Fatal("invoke after stop")
	}
}
func TestLifecycleAndInputValidation(t *testing.T) {
	if _, err := New(Options{Entry: "x", Concurrency: -1}); err == nil {
		t.Fatal("invalid limits")
	}
	w := newTestWorker(t, echo, nil)
	if _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{}`)); err == nil {
		t.Fatal("invoke before start")
	}
	start(t, w)
	if err := w.Start(context.Background()); err == nil {
		t.Fatal("double start")
	}
	if _, err := w.Invoke(context.Background(), "missing", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown hook")
	}
	if _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{`)); err == nil {
		t.Fatal("invalid JSON")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Invoke(ctx, "echo", json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
