package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestBudgetRestoreAdmissionAndValidation(t *testing.T) {
	managed := ManagedFunc(func(_ context.Context, r Request) (Result, error) {
		return Result{Response: Response{Text: "answer"}, Served: r.Target}, nil
	})
	first := &Session{Managed: managed, Limits: Limits{Calls: 2}}
	if _, e := first.Call(context.Background(), baseRequest()); e != nil {
		t.Fatal(e)
	}
	budget, e := first.Budget()
	if e != nil || budget.Calls != 1 || budget.Output == 0 || budget.Root == "" {
		t.Fatalf("snapshot %+v %v", budget, e)
	}
	next := &Session{Managed: managed, Limits: Limits{Calls: 2}}
	if e := next.RestoreBudget(budget); e != nil {
		t.Fatal(e)
	}
	if _, e := next.Call(context.Background(), baseRequest()); e != nil {
		t.Fatal(e)
	}
	if _, e := next.Call(context.Background(), baseRequest()); !errors.Is(e, ErrLimit) {
		t.Fatalf("reset admission %v", e)
	}
	after, e := next.Budget()
	if e != nil || after.Calls != 2 || after.Output <= budget.Output || after.Root != budget.Root {
		t.Fatalf("noncumulative %+v %v", after, e)
	}
	if e := next.RestoreBudget(budget); e == nil {
		t.Fatal("used session restored")
	}
	changed := &Session{Limits: Limits{Calls: 3}}
	if e := changed.RestoreBudget(budget); e == nil {
		t.Fatal("changed limits reset")
	}
	for _, bad := range []Budget{func() Budget { b := budget; b.Root = ""; return b }(), func() Budget { b := budget; b.Output = -1; return b }()} {
		fresh := &Session{Limits: Limits{Calls: 2}}
		if e := fresh.RestoreBudget(bad); e == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	started, release := make(chan struct{}), make(chan struct{})
	active := &Session{Managed: ManagedFunc(func(_ context.Context, r Request) (Result, error) {
		close(started)
		<-release
		return Result{Served: r.Target}, nil
	})}
	done := make(chan struct{})
	go func() { defer close(done); active.Call(context.Background(), baseRequest()) }()
	<-started
	if _, e := active.Budget(); e == nil {
		t.Fatal("active budget checkpointed")
	}
	close(release)
	<-done
}
func TestAllSelectedToolValidationPrecedesEffects(t *testing.T) {
	effects := 0
	session := &Session{Managed: ManagedFunc(func(_ context.Context, r Request) (Result, error) {
		return Result{Served: r.Target, Response: Response{Calls: []ToolCall{{ID: "valid", Name: "owned", Arguments: json.RawMessage(`{}`)}, {ID: "invalid", Name: "owned", Arguments: json.RawMessage(`{}`)}}}}, nil
	})}
	coordinator := Coordinator{Session: session, Validate: func(call ToolCall) error {
		if call.ID == "invalid" {
			return errors.New("denied")
		}
		return nil
	}, Tools: map[string]Tool{"owned": func(context.Context, Identity, json.RawMessage) (string, error) { effects++; return "effect", nil }}}
	if _, e := coordinator.Start(context.Background(), baseRequest()); e == nil || effects != 0 {
		t.Fatalf("validation after effects: %d %v", effects, e)
	}
}
