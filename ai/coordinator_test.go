package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func baseRequest() Request {
	return Request{Identity: Identity{"tenant", "conversation", "caller"}, Requested: Route{Model: "alias"}, Target: Route{Provider: "p", Model: "model", Account: "account", Endpoint: "https://example.invalid", Protocol: Chat, Version: "1"}, Settings: Settings{MaxTokens: 100}, Messages: []Message{{Role: "user", Text: "question"}}}
}
func TestCoordinatorMixedReplayBothWires(t *testing.T) {
	for _, kind := range []string{Chat, Messages} {
		for _, stream := range []bool{false, true} {
			t.Run(kind+map[bool]string{true: "/stream", false: "/json"}[stream], func(t *testing.T) {
				req := baseRequest()
				req.Target.Protocol = kind
				req.Stream = stream
				var inference, tools int
				var observed []Observation
				s := &Session{Observe: func(_ context.Context, o Observation) error {
					observed = append(observed, clone(o))
					o.Result.Response.Text = "observer mutation"
					return nil
				}}
				s.Managed = ManagedFunc(func(ctx context.Context, r Request) (Result, error) {
					inference++
					var response Response
					if inference == 1 {
						response = Response{ID: "mixed", Calls: []ToolCall{{"search-1", "search", json.RawMessage(`{"Tenant":"evil"}`)}, {"client-1", "shell", json.RawMessage(`{}`)}, {"search-2", "search", json.RawMessage(`{}`)}}}
					} else {
						if e := ValidateMessages(r.Messages); e != nil {
							t.Fatal(e)
						}
						ms := r.Messages
						last := ms[len(ms)-3:]
						if last[0].ToolID != "search-1" || last[1].ToolID != "client-1" || last[2].ToolID != "search-2" {
							t.Fatalf("wrong result ordering %+v", last)
						}
						response = Response{ID: "final", Text: "complete", Usage: Usage{num(0), num(5)}}
					}
					a := Adapter{kind}
					var b []byte
					var e error
					if stream {
						b, e = a.EncodeStream(response)
					} else {
						b, e = a.Encode(response)
					}
					if e != nil {
						return Result{}, e
					}
					parsed, e := a.DecodeBuffered(b, stream, StreamLimits{})
					return Result{parsed, r.Target}, e
				})
				var mu sync.Mutex
				c := Coordinator{Session: s, Tools: map[string]Tool{"search": func(ctx context.Context, id Identity, arg json.RawMessage) (string, error) {
					mu.Lock()
					tools++
					mu.Unlock()
					if ToolCallID(ctx) == "" {
						t.Error("missing trusted call ID")
					}
					if id != req.Identity {
						t.Error("untrusted identity")
					}
					return "found", nil
				}}}
				turn, e := c.Start(context.Background(), req)
				if e != nil {
					t.Fatal(e)
				}
				if inference != 1 || tools != 2 || len(turn.Pending) != 1 || len(turn.Response.Calls) != 1 || turn.Response.Calls[0].ID != "client-1" {
					t.Fatalf("bad handoff %+v", turn)
				}
				replay := append(clone(turn.Public), Message{Role: "tool", ToolID: "client-1", Text: "client result"})
				invalid := clone(replay)
				invalid[0].Text = "different"
				if _, e = c.Resume(context.Background(), req, turn, invalid); e == nil {
					t.Fatal("accepted changed replay")
				}
				impersonated := req
				impersonated.Identity.Tenant = "other"
				if _, e = c.Resume(context.Background(), impersonated, turn, replay); e == nil {
					t.Fatal("accepted identity change")
				}
				final, e := c.Resume(context.Background(), req, turn, replay)
				if e != nil {
					t.Fatal(e)
				}
				if final.Response.Text != "complete" || inference != 2 || tools != 2 || len(observed) != 2 {
					t.Fatalf("bad continuation %+v", final)
				}
				if observed[0].Result.Response.Usage.Input != nil || observed[1].Result.Response.Usage.Input == nil || *observed[1].Result.Response.Usage.Input != 0 {
					t.Fatal("usage distinction lost")
				}
				if observed[0].Request.Requested != req.Requested || observed[1].Result.Served != req.Target {
					t.Fatal("provenance lost")
				}
			})
		}
	}
}
func TestCoordinatorLimitsCancellationAndDuplicateIDs(t *testing.T) {
	t.Run("duplicates", func(t *testing.T) {
		s := &Session{Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
			return Result{Response: Response{Calls: []ToolCall{{"same", "search", json.RawMessage(`{}`)}, {"same", "search", json.RawMessage(`{}`)}}}}, nil
		})}
		executed := false
		c := Coordinator{s, map[string]Tool{"search": func(context.Context, Identity, json.RawMessage) (string, error) { executed = true; return "", nil }}}
		if _, e := c.Start(context.Background(), baseRequest()); e == nil || executed {
			t.Fatal("duplicate IDs executed")
		}
	})
	t.Run("rounds", func(t *testing.T) {
		var i int
		s := &Session{Limits: Limits{Rounds: 1}, Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
			i++
			return Result{Response: Response{Calls: []ToolCall{{"one", "search", json.RawMessage(`{}`)}}}}, nil
		})}
		c := Coordinator{s, map[string]Tool{"search": func(context.Context, Identity, json.RawMessage) (string, error) { return "ok", nil }}}
		if _, e := c.Start(context.Background(), baseRequest()); !errors.Is(e, ErrLimit) || i != 1 {
			t.Fatal(e, i)
		}
	})
	t.Run("output", func(t *testing.T) {
		s := &Session{Limits: Limits{OutputBytes: 1}, Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
			return Result{Response: Response{Text: "too long"}}, nil
		})}
		if _, e := s.Call(context.Background(), baseRequest()); !errors.Is(e, ErrLimit) {
			t.Fatal(e)
		}
	})
	t.Run("parallel-cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var active, peak atomic.Int32
		started := make(chan struct{}, 2)
		var inference atomic.Int32
		s := &Session{Limits: Limits{Parallel: 2}, Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
			inference.Add(1)
			return Result{Response: Response{Calls: []ToolCall{{"a", "search", json.RawMessage(`{}`)}, {"b", "search", json.RawMessage(`{}`)}, {"c", "search", json.RawMessage(`{}`)}}}}, nil
		})}
		c := Coordinator{s, map[string]Tool{"search": func(ctx context.Context, _ Identity, _ json.RawMessage) (string, error) {
			n := active.Add(1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			<-ctx.Done()
			active.Add(-1)
			return "", ctx.Err()
		}}}
		done := make(chan error, 1)
		go func() { _, e := c.Start(ctx, baseRequest()); done <- e }()
		<-started
		<-started
		cancel()
		if e := <-done; !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
		if peak.Load() != 2 || active.Load() != 0 || inference.Load() != 1 {
			t.Fatal("cancellation/admission failed")
		}
	})
}
func TestSessionTimeoutRetainsCapacityAndOriginalObservation(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	s := &Session{Limits: Limits{Parallel: 1, Timeout: 10 * time.Millisecond}, Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
		close(started)
		<-release
		return Result{Response: Response{Text: "late"}}, nil
	})}
	done := make(chan error, 1)
	go func() { _, e := s.Call(context.Background(), baseRequest()); done <- e }()
	<-started
	time.Sleep(20 * time.Millisecond)
	if _, e := s.Call(context.Background(), baseRequest()); !errors.Is(e, ErrLimit) {
		t.Fatal("capacity released while code running", e)
	}
	close(release)
	if e := <-done; !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("late result accepted", e)
	}
	s = &Session{Managed: ManagedFunc(func(_ context.Context, r Request) (Result, error) {
		r.Messages[0].Text = "bridge mutation"
		return Result{Response: Response{Text: "original"}}, nil
	}), Observe: func(_ context.Context, o Observation) error {
		if !reflect.DeepEqual(o.Request.Messages, baseRequest().Messages) || o.Result.Response.Text != "original" {
			t.Error("not detached original")
		}
		return errors.New("disk unavailable")
	}}
	if _, e := s.Call(context.Background(), baseRequest()); e == nil {
		t.Fatal("required observer failure ignored")
	}
}
