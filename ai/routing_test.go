package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func target(kind, account string) Target {
	return Target{Route: Route{Provider: kind, Model: "example-model", Account: account, Endpoint: "https://" + account + ".invalid", Protocol: kind, Version: "v1"}, Tools: true, Stream: true, MaxTokens: 1000, Temperature: true}
}
func TestPrepareAffinityCapabilitiesCacheAndCommit(t *testing.T) {
	req := baseRequest()
	a := target(Messages, "b")
	aff := Affinity{Bound: req.Target, Revision: "revision-1"}
	if _, e := Prepare(req, a, aff, false); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	aff.ProviderHeld = true
	if _, e := Prepare(req, a, aff, true); !errors.Is(e, ErrUnsupported) {
		t.Fatal("provider state migrated", e)
	}
	aff.ProviderHeld = false
	translated, e := Prepare(req, a, aff, true)
	if e != nil {
		t.Fatal(e)
	}
	if translated.Requested != req.Requested || req.Target == translated.Target {
		t.Fatal("provenance mutation")
	}
	key, e := CacheKey(translated, aff)
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Request){func(r *Request) { r.Identity.Tenant = "other" }, func(r *Request) { r.Target.Account = "different" }, func(r *Request) { r.Settings.MaxTokens++ }, func(r *Request) { r.Target.Version = "v2" }, func(r *Request) { r.Target.Model = "other" }, func(r *Request) { r.Messages[0].Text = "new" }} {
		other := clone(translated)
		mutate(&other)
		k, e := CacheKey(other, aff)
		if e != nil || k == key {
			t.Fatal("cache scope collision", e)
		}
	}
	translated.Identity.Tenant = ""
	if _, e := CacheKey(translated, aff); e == nil {
		t.Fatal("unknown cache scope accepted")
	}
	req.Tools = []ToolDefinition{{Name: "search", Schema: json.RawMessage(`{}`)}}
	a.Tools = false
	if _, e := Prepare(req, a, aff, true); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	s := &Session{Managed: ManagedFunc(func(context.Context, Request) (Result, error) { t.Fatal("postcommit dispatch"); return Result{}, nil })}
	if _, e := Reroute(context.Background(), s, baseRequest(), target(Messages, "b"), aff, true, true); e == nil {
		t.Fatal("postcommit reroute")
	}
}
func TestClassifierCandidatesCancelAndAccount(t *testing.T) {
	req := baseRequest()
	var mu sync.Mutex
	var obs []Observation
	loserStarted := make(chan struct{})
	var canceled atomic.Bool
	s := &Session{Limits: Limits{Calls: 4, Parallel: 2}, Observe: func(_ context.Context, o Observation) error {
		mu.Lock()
		defer mu.Unlock()
		obs = append(obs, o)
		return nil
	}, Managed: ManagedFunc(func(ctx context.Context, r Request) (Result, error) {
		switch r.Purpose {
		case "classifier":
			return Result{Response: Response{Text: "b"}, Served: r.Target}, nil
		case "candidate":
			if r.Target.Account == "loser" {
				close(loserStarted)
				<-ctx.Done()
				canceled.Store(true)
				return Result{Served: r.Target}, ctx.Err()
			}
			<-loserStarted
			return Result{Response: Response{Text: "winner", Calls: []ToolCall{{"selected-call", "side-effect", json.RawMessage(`{}`)}}, Usage: Usage{num(0), num(0)}}, Served: r.Target}, nil
		default:
			return Result{Served: r.Target}, errors.New("provider failed without usage")
		}
	})}
	selected, e := Classify(context.Background(), s, req, map[string]Target{"b": target(Messages, "b")})
	if e != nil || selected.Route.Account != "b" {
		t.Fatal(e)
	}
	one := clone(req)
	one.Target.Account = "loser"
	two := clone(req)
	two.Target.Account = "winner"
	got, e := Candidates(context.Background(), s, []Request{one, two}, func(r Result) bool { return r.Response.Text == "winner" })
	if e != nil || got.Served.Account != "winner" || !canceled.Load() {
		t.Fatal("selection/cancellation", e)
	}
	if _, e = s.Call(context.Background(), req); e == nil {
		t.Fatal("failed provider hidden")
	}
	if _, e = s.Call(context.Background(), req); !errors.Is(e, ErrLimit) {
		t.Fatal("classifier/candidates escaped root limit", e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(obs) != 4 {
		t.Fatal("missing observations", len(obs))
	}
	unknown, zero, failed := false, false, false
	ids := map[string]bool{}
	root := ""
	for _, o := range obs {
		if o.Request.Lineage.Call == "" || ids[o.Request.Lineage.Call] {
			t.Fatal("missing/duplicate logical call lineage")
		}
		ids[o.Request.Lineage.Call] = true
		if root == "" {
			root = o.Request.Lineage.Root
		}
		if root == "" || root != o.Request.Lineage.Root {
			t.Fatal("root lineage not shared")
		}
		if o.Result.Response.Usage.Input == nil {
			unknown = true
		} else if *o.Result.Response.Usage.Input == 0 {
			zero = true
		}
		if o.Error != "" {
			failed = true
		}
		if o.Request.Requested != req.Requested {
			t.Fatal("requested provenance changed")
		}
	}
	if !unknown || !zero || !failed {
		t.Fatal("incomplete original evidence")
	}
}
