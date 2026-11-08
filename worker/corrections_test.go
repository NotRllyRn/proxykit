package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStopInheritedStderrBound(t *testing.T) {
	var logs bytes.Buffer
	w := newTestWorker(t, `import {spawn} from 'node:child_process'; export default api => {spawn(process.execPath,['-e','setTimeout(()=>{},1500)'],{stdio:['ignore','ignore','inherit']});}`, func(o *Options) { o.Stderr = &logs; o.ShutdownTimeout = 50 * time.Millisecond })
	start(t, w)
	before := time.Now()
	_ = w.Stop(context.Background()) // ErrWaitDelay is an actionable forwarding outcome.
	if elapsed := time.Since(before); elapsed > 350*time.Millisecond {
		t.Fatalf("Stop exceeded bound: %s", elapsed)
	}
}

type blockedLogger struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *blockedLogger) Write(b []byte) (int, error) {
	l.once.Do(func() { close(l.entered) })
	<-l.release
	return len(b), nil
}
func TestStopDoesNotWaitForUserWriter(t *testing.T) {
	logger := &blockedLogger{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(logger.release)
	w := newTestWorker(t, `export default api=>{ console.error('log'); };`, func(o *Options) { o.Stderr = logger; o.ShutdownTimeout = 50 * time.Millisecond })
	start(t, w)
	select {
	case <-logger.entered:
	case <-time.After(time.Second):
		t.Fatal("logger not entered")
	}
	before := time.Now()
	if err := w.Stop(context.Background()); err == nil {
		t.Fatal("blocked forwarding must report timeout")
	}
	if time.Since(before) > 350*time.Millisecond {
		t.Fatal("Stop waited on user writer")
	}
}

func TestConcurrentStopJoinsCleanup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleanup")
	w := newTestWorker(t, fmt.Sprintf(`import {writeFileSync} from 'node:fs'; export default api=>api.cleanup(async()=>{await new Promise(r=>setTimeout(r,80));writeFileSync(%q,'done');});`, marker), nil)
	start(t, w)
	gate := make(chan struct{})
	errs := make(chan error, 20)
	for n := 0; n < 20; n++ {
		go func() { <-gate; errs <- w.Stop(context.Background()) }()
	}
	close(gate)
	for n := 0; n < 20; n++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "done" {
		t.Fatalf("cleanup lost: %q %v", b, err)
	}
	if err := w.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInvokeStopWireOrdering(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleanup")
	source := fmt.Sprintf(`import {writeFileSync} from 'node:fs'; export default api=>{api.on({id:'echo',stage:'observe',bodyMode:'none'},x=>x);api.cleanup(async()=>{await new Promise(r=>setTimeout(r,20));writeFileSync(%q,'done');});};`, marker)
	w := newTestWorker(t, source, nil)
	input := json.RawMessage(`"` + strings.Repeat("a", 800000) + `"`)
	for n := 0; n < 12; n++ {
		start(t, w)
		done := make(chan struct{})
		go func() { defer close(done); _, _ = w.Invoke(context.Background(), "echo", input) }()
		// Exercise shutdown while marshaling a large admitted invocation.
		time.Sleep(time.Millisecond)
		if err := w.Stop(context.Background()); err != nil {
			t.Fatalf("round %d: %v", n, err)
		}
		<-done
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("round %d cleanup lost: %v", n, err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStopReportsCleanupAndProtocolFailures(t *testing.T) {
	for name, cleanup := range map[string]string{
		"cleanup exception":       `throw new Error('cleanup failed')`,
		"missing acknowledgement": `process.exit(0)`,
		"protocol crash":          `process.stdout.write('broken');await new Promise(r=>setTimeout(r,100))`,
	} {
		t.Run(name, func(t *testing.T) {
			w := newTestWorker(t, `export default api=>api.cleanup(async()=>{`+cleanup+`});`, nil)
			start(t, w)
			if err := w.Stop(context.Background()); err == nil {
				t.Fatal("Stop hid shutdown failure")
			}
			if err := w.Stop(context.Background()); err == nil {
				t.Fatal("Stop lost recorded failure")
			}
			// A deliberate later restart must not be prevented by the recorded crash.
			if err := w.Restart(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRestartAlreadyCrashed(t *testing.T) {
	w := newTestWorker(t, `export default api=>api.on({id:'echo',stage:'observe',bodyMode:'none'},()=>process.exit(3));`, nil)
	start(t, w)
	_, err := w.Invoke(context.Background(), "echo", json.RawMessage(`null`))
	if err == nil {
		t.Fatal("missing crash")
	}
	p := w.process()
	select {
	case <-p.exited:
	case <-time.After(time.Second):
		t.Fatal("not reaped")
	}
	if err := w.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
}
