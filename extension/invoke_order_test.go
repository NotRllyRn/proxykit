package extension

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"sync"
	"testing"
)

// String blocks panic formatting after the handler has exited, so the test can
// inspect invocation bookkeeping without relying on a sleep or a copy's size.
type blockedPanic struct {
	entered chan struct{}
	release chan struct{}
}

func (p blockedPanic) String() string {
	close(p.entered)
	<-p.release
	return "expected panic"
}

func TestInvokeCancellationRetainsAdmissionAndShutdownTracking(t *testing.T) {
	p := blockedPanic{entered: make(chan struct{}), release: make(chan struct{})}
	cleaned := make(chan struct{})
	var scope context.Context
	h := Registration{ID: "recovery", Owner: "example", Handler: func(ctx context.Context, _ Input) (Result, error) {
		scope = ctx
		panic(p)
	}, Cleanup: func(context.Context) error {
		close(cleaned)
		return nil
	}}
	e, err := New(Config{Admission: 1, InvocationTimeout: -1}, h)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var once sync.Once
	release := func() { once.Do(func() { close(p.release) }) }
	defer release()
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := e.invoke(ctx, h, Input{})
		returned <- err
	}()
	<-p.entered // Handler exited, but panic recovery is still running.
	if !errors.Is(scope.Err(), context.Canceled) {
		t.Fatalf("handler scope still active after panic: %v", scope.Err())
	}
	cancel()
	if err := <-returned; !errors.Is(err, context.Canceled) {
		t.Fatalf("invoke: %v", err)
	}
	if _, err := e.invoke(e.ctx, h, Input{}); !errors.Is(err, ErrOverload) {
		t.Fatalf("recovery bookkeeping released capacity early: %v", err)
	}
	// An already-canceled wait deterministically stops waiting; it must not
	// clean up while the native goroutine is still in recovery bookkeeping.
	if err := e.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
	select {
	case <-cleaned:
		t.Fatal("cleanup ran before invocation bookkeeping completed")
	default:
	}
	release()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("cleanup did not run after invocation drain")
	}
	if n := len(e.slots); n != 0 {
		t.Fatalf("drained invocation retained %d slots", n)
	}
}

// A completed invocation must make its admission slot available before its
// caller can start the next hook. Exercise the scheduler handoff repeatedly;
// there is deliberately no retry or wait for capacity in the caller.
func TestInvokeReleasesAdmissionBeforePublication(t *testing.T) {
	for _, outcome := range []string{"pass", "panic", "limit"} {
		t.Run(outcome, func(t *testing.T) {
			e, err := New(Config{Admission: 1, BodyBytes: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer e.Close()
			h := Registration{ID: "handoff", Handler: func(context.Context, Input) (Result, error) {
				// Let the invoking goroutine reach its result wait first.
				runtime.Gosched()
				switch outcome {
				case "panic":
					panic("expected")
				case "limit":
					return Result{Message: &Message{Body: []byte("too large")}}, nil
				default:
					return Result{
						Message:     &Message{Body: []byte("x"), Header: http.Header{"Example": {"value"}}},
						Metadata:    map[string]string{"example": "value"},
						Credentials: http.Header{"Authorization": {"synthetic"}},
					}, nil
				}
			}}
			for i := 0; i < 20000; i++ {
				_, err := e.invoke(e.ctx, h, Input{})
				if errors.Is(err, ErrOverload) {
					t.Fatalf("sequential invocation %d rejected: %v", i, err)
				}
				if (outcome == "pass") != (err == nil) {
					t.Fatalf("invocation %d: unexpected error %v", i, err)
				}
				if n := len(e.slots); n != 0 {
					t.Fatalf("invocation %d published with %d admission slot still occupied", i, n)
				}
			}
		})
	}
}
