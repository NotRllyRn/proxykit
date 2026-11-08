package extension_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
)

type countedBody struct{ closed atomic.Int32 }

func (*countedBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *countedBody) Close() error           { b.closed.Add(1); return nil }

func TestCallExpiresAtInvocationEndAndReturnedChildSurvives(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint("timeout=", timeout), func(t *testing.T) {
			savedCalls := make(chan func(context.Context, *http.Request) (*http.Response, error), 1)
			release, exited := make(chan struct{}), make(chan struct{})
			h := reg("retain", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
				if in.Identity.Depth != 0 {
					return extension.Result{}, nil
				}
				savedCalls <- in.Call
				if timeout {
					<-ctx.Done()
					<-release
				}
				close(exited)
				// A late callback must not install this response or metadata.
				if timeout {
					return extension.Result{Action: extension.Respond, Message: &extension.Message{Status: 299, Header: http.Header{}, Body: []byte("late")}, Metadata: map[string]string{"late": "write"}}, nil
				}
				return extension.Result{}, nil
			})
			h.Failure = extension.FailOpen
			var calls atomic.Int32
			selectChild := reg("select", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
				if in.Identity.Depth != 0 {
					return extension.Result{}, nil
				}
				// Root is still active, but the earlier invocation is not.
				body := &countedBody{}
				r := request("")
				r.Body = body
				saved := <-savedCalls
				_, err := saved(context.Background(), r)
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("expired call: %v", err)
				}
				if body.closed.Load() != 1 {
					t.Errorf("rejected body closes: %d", body.closed.Load())
				}
				if timeout {
					close(release)
					<-exited
				}
				if in.Metadata["late"] != "" {
					t.Error("late metadata committed")
				}
				return extension.Result{Action: extension.Child, Request: request("child")}, nil
			})
			e := engine(t, extension.Config{InvocationTimeout: 50 * time.Millisecond, Attempts: 2}, h, selectChild)
			resp, err := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				b, _ := io.ReadAll(r.Body)
				if string(b) == "child" {
					return response("child"), nil
				}
				return response("root"), nil
			})).RoundTrip(request("root"))
			if err != nil {
				t.Fatal(err)
			}
			if got := read(t, resp); got != "child" {
				t.Fatalf("response %q", got)
			}
			if calls.Load() != 2 {
				t.Fatalf("transport calls %d", calls.Load())
			}
		})
	}
}

func TestInvocationEndCancelsAlreadyAdmittedCall(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint("timeout=", timeout), func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan error, 1)
			h := reg("launch", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
				if in.Identity.Depth != 0 {
					return extension.Result{}, nil
				}
				go func() { _, err := in.Call(context.Background(), request("child")); canceled <- err }()
				<-started
				if timeout {
					<-ctx.Done()
				}
				return extension.Result{}, nil
			})
			h.Failure = extension.FailOpen
			e := engine(t, extension.Config{InvocationTimeout: 50 * time.Millisecond}, h)
			resp, err := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
				b, _ := io.ReadAll(r.Body)
				if string(b) == "child" {
					close(started)
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return response("root"), nil
			})).RoundTrip(request("root"))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close() // keep root active while checking child cancellation
			select {
			case err := <-canceled:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("child error %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("admitted call outlived invocation")
			}
		})
	}
}

func TestManagedClientRejectedBodiesClose(t *testing.T) {
	clients := make(chan *http.Client, 1)
	var e *extension.Engine
	issue := reg("issue", extension.Prepare, func(ctx context.Context, _ extension.Input) (extension.Result, error) {
		client, err := e.Client(ctx, "http://gateway.test")
		if err != nil {
			return extension.Result{}, err
		}
		body := &countedBody{}
		r := request("")
		r.Body = body // different origin, while invocation is active
		if _, err := client.Do(r); err == nil {
			t.Error("wrong origin accepted")
		}
		if body.closed.Load() != 1 {
			t.Errorf("wrong-origin body closes %d", body.closed.Load())
		}
		clients <- client
		return extension.Result{}, nil
	})
	expired := reg("expired", extension.Resolve, func(context.Context, extension.Input) (extension.Result, error) {
		client := <-clients
		body := &countedBody{}
		r, _ := http.NewRequest("POST", "http://gateway.test/", body)
		if _, err := client.Do(r); !errors.Is(err, context.Canceled) {
			t.Errorf("expired client error %v", err)
		}
		if body.closed.Load() != 1 {
			t.Errorf("expired client body closes %d", body.closed.Load())
		}
		return extension.Result{}, nil
	})
	e = engine(t, extension.Config{}, issue, expired)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)

	body := &countedBody{}
	r := request("")
	r.URL = nil
	r.Body = body
	if _, err := e.Transport(transport(func(*http.Request) (*http.Response, error) {
		t.Error("invalid request dispatched")
		return response(""), nil
	})).RoundTrip(r); err == nil {
		t.Error("missing URL accepted")
	}
	if body.closed.Load() != 1 {
		t.Errorf("invalid request body closes %d", body.closed.Load())
	}
}

func TestLedgerSanitizesNativeErrorsAndPanics(t *testing.T) {
	const secret = "fake-credential-distinctive-7a90"
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprint("panic=", panics), func(t *testing.T) {
			bad := reg("bad", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
				if panics {
					panic(secret)
				}
				return extension.Result{}, fmt.Errorf("native credential=%s", secret)
			})
			bad.Failure = extension.FailOpen
			var seen bool
			observer := reg("observer", extension.Resolve, func(_ context.Context, in extension.Input) (extension.Result, error) {
				if strings.Contains(fmt.Sprint(in.Ledger), secret) {
					t.Error("native secret leaked into shared ledger")
				}
				for _, entry := range in.Ledger {
					if entry.Hook == "bad" {
						seen = true
						if entry.Outcome != "error" {
							t.Errorf("outcome %q", entry.Outcome)
						}
					}
				}
				return extension.Result{}, nil
			})
			observer.Owner = "other-owner"
			e := engine(t, extension.Config{}, bad, observer)
			resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
			if err != nil {
				t.Fatal(err)
			}
			read(t, resp)
			if !seen {
				t.Fatal("missing sanitized ledger entry")
			}
			// Blocking errors still retain details for the private API caller.
			bad.Failure = extension.Block
			e2 := engine(t, extension.Config{}, bad)
			_, err = e2.Transport(transport(func(*http.Request) (*http.Response, error) {
				t.Error("blocked request dispatched")
				return response(""), nil
			})).RoundTrip(request(""))
			if err == nil || !strings.Contains(err.Error(), secret) {
				t.Fatalf("private error lost detail: %v", err)
			}
		})
	}
}
