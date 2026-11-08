package recall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
)

func key() Key { return Key{"personal-usage", "example-conversation"} }
func identity() ai.Identity {
	return ai.Identity{Tenant: key().Tenant, Conversation: key().Conversation, Caller: "Example user"}
}
func messages() []ai.Message {
	return []ai.Message{{Role: "user", Text: "original context to archive"}}
}
func route(kind, account string) ai.Route {
	return ai.Route{Provider: kind, Model: "example-model", Account: account, Endpoint: "https://" + account + ".invalid", Protocol: kind, Version: "1"}
}
func req() ai.Request {
	return ai.Request{Identity: identity(), Requested: ai.Route{Model: "requested-alias"}, Target: route(ai.Chat, "a"), Messages: messages(), Settings: ai.Settings{MaxTokens: 100}}
}
func TestArchiveReduceCASIsolationAndFailure(t *testing.T) {
	ctx := context.Background()
	s := Store{Dir: t.TempDir()}
	st, e := s.Archive(ctx, key(), 0, "recall", messages(), ai.Affinity{Bound: req().Target, Revision: "1"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Reduce(ctx, key(), st.Revision, "recall", func(ms []ai.Message) ([]ai.Message, error) {
		ms[0].Text = "mutated before failure"
		return nil, errors.New("reduction failure")
	}); e == nil {
		t.Fatal("ignored error")
	}
	got, e := s.Read(ctx, key())
	if e != nil || got.Working[0].Text != messages()[0].Text {
		t.Fatal("archive rollback failure", e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Reduce(ctx, key(), st.Revision, "recall", func([]ai.Message) ([]ai.Message, error) { return []ai.Message{{Role: "user", Text: "summary"}}, nil })
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	wins, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			wins++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	got, e = s.Read(ctx, key())
	if e != nil || got.Working[0].Text != "summary" || !reflect.DeepEqual(got.Archive[0], messages()) {
		t.Fatal("archive lost", e)
	}
	other := key()
	other.Tenant = "work-laptop"
	isolated, e := s.Read(ctx, other)
	if e != nil || isolated.Revision != 0 || len(isolated.Archive) != 0 {
		t.Fatal("tenant leak", e)
	}
	if _, e = s.Read(ctx, Key{Conversation: "missing-tenant"}); e == nil {
		t.Fatal("implicit identity")
	}
	if _, e = s.Archive(ctx, key(), got.Revision, "other-compactor", messages(), ai.Affinity{}); e == nil {
		t.Fatal("conflicting compactor accepted")
	}
	small := s
	small.MaxBytes = 1
	if _, e = small.Archive(ctx, other, 0, "recall", messages(), ai.Affinity{}); !errors.Is(e, ai.ErrLimit) {
		t.Fatal(e)
	}
	isolated, _ = s.Read(ctx, other)
	if isolated.Revision != 0 {
		t.Fatal("failed archive committed")
	}
	badPath := filepath.Join(t.TempDir(), "not-a-directory")
	if e = os.WriteFile(badPath, []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = (Store{Dir: badPath}).Archive(ctx, key(), 0, "recall", messages(), ai.Affinity{}); e == nil {
		t.Fatal("archive failure ignored")
	}
	signedKey := Key{"personal-usage", "signed"}
	signed, e := s.Archive(ctx, signedKey, 0, "recall", messages(), ai.Affinity{ProviderHeld: true})
	if e != nil {
		t.Fatal(e)
	}
	called := false
	if _, e = s.Reduce(ctx, signedKey, signed.Revision, "recall", func(ms []ai.Message) ([]ai.Message, error) { called = true; return ms, nil }); !errors.Is(e, ai.ErrUnsupported) || called {
		t.Fatal("unsafe signed state reduction", e)
	}
}

// appendObserver is a synchronous durable-handoff fixture, not a production
// telemetry store. Each line records the detached original before decoration.
func appendObserver(path string) ai.Observer {
	return func(_ context.Context, o ai.Observation) error {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		if e = json.NewEncoder(f).Encode(o); e != nil {
			return e
		}
		return f.Sync()
	}
}
func TestCombinedCoordinatorReplayRestartRerouteObserver(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s := Store{Dir: dir}
	st, e := s.Archive(ctx, key(), 0, "recall", messages(), ai.Affinity{Bound: req().Target, Revision: "1"})
	if e != nil {
		t.Fatal(e)
	}
	st, e = s.Reduce(ctx, key(), st.Revision, "recall", func(ms []ai.Message) ([]ai.Message, error) {
		return []ai.Message{{Role: "user", Text: "summary of original"}}, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	r := req()
	r.Messages = st.Working
	r.Stream = true
	managed := &ai.Session{Observe: appendObserver(filepath.Join(dir, "observations.jsonl")), Managed: ai.ManagedFunc(func(_ context.Context, r ai.Request) (ai.Result, error) {
		if r.Target.Account != "a" {
			return ai.Result{}, errors.New("fixture denied route")
		}
		response := ai.Response{ID: "mixed", Model: r.Target.Model, Calls: []ai.ToolCall{{ID: "search-a", Name: "search", Arguments: json.RawMessage(`{"tenant":"attacker"}`)}, {ID: "shell-a", Name: "client-shell", Arguments: json.RawMessage(`{}`)}, {ID: "search-b", Name: "search", Arguments: json.RawMessage(`{}`)}}}
		b, e := (ai.Adapter{Kind: ai.Chat}).EncodeStream(response)
		if e != nil {
			return ai.Result{}, e
		}
		parsed, e := (ai.Adapter{Kind: ai.Chat}).DecodeBuffered(b, true, ai.StreamLimits{})
		return ai.Result{Response: parsed, Served: r.Target}, e
	})}
	var mu sync.Mutex
	executions := 0
	coordinator := ai.Coordinator{Session: managed, Tools: map[string]ai.Tool{"search": func(_ context.Context, id ai.Identity, _ json.RawMessage) (string, error) {
		mu.Lock()
		executions++
		mu.Unlock()
		if id != identity() {
			return "", errors.New("identity spoof")
		}
		return "archived original found", nil
	}}}
	turn, e := coordinator.Start(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if executions != 2 || len(turn.Pending) != 1 {
		t.Fatal("mixed handoff")
	}
	st, e = s.SaveTurn(ctx, key(), st.Revision, turn)
	if e != nil {
		t.Fatal(e)
	}
	// New OS process reads private state and actual archive, reroutes before
	// commitment, merges exact client replay, and continues without rerunning tools.
	runChild(t, dir, "resume", nil)
	got, e := s.Read(ctx, key())
	if e != nil {
		t.Fatal(e)
	}
	if got.Turn == nil || got.Turn.Response.Text != "final after restart" || len(got.Turn.Pending) != 0 || got.Revision != st.Revision+1 {
		t.Fatalf("bad restart: %+v", got)
	}
	if !reflect.DeepEqual(got.Archive[0], messages()) {
		t.Fatal("archive changed")
	}
	// Selected Anthropic output is rendered in the original Chat wire contract.
	presented := got.Turn.Response
	presented.Text = "decorated final"
	wire, e := (ai.Adapter{Kind: ai.Chat}).EncodeStream(presented)
	if e != nil {
		t.Fatal(e)
	}
	client, e := (ai.Adapter{Kind: ai.Chat}).DecodeBuffered(wire, true, ai.StreamLimits{})
	if e != nil || client.Text != "decorated final" {
		t.Fatal("client protocol substitution failed", e)
	}
	data, e := os.ReadFile(filepath.Join(dir, "observations.jsonl"))
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("want five attributed calls, got %d", len(lines))
	}
	unknown, measured, denied := false, false, false
	root := ""
	ids := map[string]bool{}
	for _, line := range lines {
		var o ai.Observation
		if e = json.Unmarshal([]byte(line), &o); e != nil {
			t.Fatal(e)
		}
		if root == "" {
			root = o.Request.Lineage.Root
		}
		if root == "" || o.Request.Lineage.Root != root || ids[o.Request.Lineage.Call] || o.Request.Lineage.Call == "" {
			t.Fatal("lost restart lineage")
		}
		ids[o.Request.Lineage.Call] = true
		if o.Request.Requested.Model != "requested-alias" {
			t.Fatal("lost requested provenance")
		}
		if o.Result.Response.Usage.Input == nil {
			unknown = true
		} else if *o.Result.Response.Usage.Input == 0 {
			measured = true
		}
		if o.Error != "" {
			denied = true
		}
		if o.Request.Purpose == "tool-continuation" && o.Request.Target.Account == "b" && o.Result.Response.Text != "final after restart" {
			t.Fatal("presentation changed original observation")
		}
	}
	if !unknown || !measured || !denied {
		t.Fatal("lost accounting distinction")
	}
}
func runChild(t *testing.T, dir, mode string, extra []string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecallFreshProcess$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "RECALL_TEST_DIR="+dir, "RECALL_TEST_MODE="+mode)
	cmd.Env = append(cmd.Env, extra...)
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fresh process: %v\n%s", e, b)
	}
}
func TestRecallFreshProcess(t *testing.T) {
	dir := os.Getenv("RECALL_TEST_DIR")
	if dir == "" {
		t.Skip("subprocess fixture")
	}
	ctx := context.Background()
	s := Store{Dir: dir}
	st, e := s.Read(ctx, key())
	if e != nil {
		t.Fatal(e)
	}
	switch os.Getenv("RECALL_TEST_MODE") {
	case "resume":
		if st.Turn == nil || len(st.Archive) != 1 {
			t.Fatal("missing persisted state")
		}
		r := req()
		r.Lineage = st.Turn.Lineage
		r.Messages = st.Original
		r.Stream = true
		session := &ai.Session{Observe: appendObserver(filepath.Join(dir, "observations.jsonl")), Managed: ai.ManagedFunc(func(_ context.Context, r ai.Request) (ai.Result, error) {
			if r.Target.Account != "a" && r.Target.Account != "b" {
				return ai.Result{Served: r.Target}, errors.New("fixture denied destination")
			}
			if r.Purpose == "classifier" {
				return ai.Result{Response: ai.Response{Text: "portable"}, Served: r.Target}, nil
			}
			if e := ai.ValidateMessages(r.Messages); e != nil {
				return ai.Result{}, e
			}
			text := "reroute probe"
			if r.Purpose == "tool-continuation" {
				if len(r.Messages) < 5 {
					t.Fatal("private continuation absent")
				}
				text = "final after restart"
			}
			zero := int64(0)
			response := ai.Response{ID: "final", Model: r.Target.Model, Text: text, Usage: ai.Usage{Input: &zero, Output: &zero}}
			a := ai.Adapter{Kind: r.Target.Protocol}
			b, e := a.EncodeStream(response)
			if e != nil {
				return ai.Result{}, e
			}
			parsed, e := a.DecodeBuffered(b, true, ai.StreamLimits{})
			return ai.Result{Response: parsed, Served: r.Target}, e
		})}
		b := ai.Target{Route: route(ai.Messages, "b"), Tools: true, Stream: true, MaxTokens: 1000}
		selected, e := ai.Classify(ctx, session, r, map[string]ai.Target{"portable": b})
		if e != nil {
			t.Fatal(e)
		}
		denied := selected
		denied.Route.Account = "denied"
		if _, e = ai.Reroute(ctx, session, r, denied, st.Affinity, true, false); e == nil {
			t.Fatal("managed authorization bypass")
		}
		outcome, e := ai.Reroute(ctx, session, r, selected, st.Affinity, true, false)
		if e != nil {
			t.Fatal(e)
		}
		if outcome.Served != b.Route {
			t.Fatal("wrong served provenance")
		}
		r.Target = b.Route
		replay := append(append([]ai.Message(nil), st.Turn.Public...), ai.Message{Role: "tool", ToolID: "shell-a", Text: "client result"})
		c := ai.Coordinator{Session: session, Tools: map[string]ai.Tool{"search": func(context.Context, ai.Identity, json.RawMessage) (string, error) {
			t.Fatal("replayed internal tool")
			return "", nil
		}}}
		if _, e = s.Continue(ctx, key(), st.Revision, c, r, replay); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Continue(ctx, key(), st.Revision, c, r, replay); !errors.Is(e, ErrConflict) {
			t.Fatal("replayed revision admitted", e)
		}
	case "archive-only":
		if st.Working[0].Text != messages()[0].Text || len(st.Archive) != 1 {
			t.Fatal("archive not restart-safe")
		}
	case "cas":
		_, e = s.Reduce(ctx, key(), 1, "recall", func([]ai.Message) ([]ai.Message, error) {
			return []ai.Message{{Role: "user", Text: "process summary"}}, nil
		})
		if e != nil && !errors.Is(e, ErrConflict) {
			t.Fatal(e)
		}
	default:
		t.Fatal("unknown fixture mode")
	}
}
func TestArchiveRestartAndCrossProcessConflict(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: dir}
	if _, e := s.Archive(context.Background(), key(), 0, "recall", messages(), ai.Affinity{}); e != nil {
		t.Fatal(e)
	}
	runChild(t, dir, "archive-only", nil)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); runChild(t, dir, "cas", nil) }()
	}
	wg.Wait()
	st, e := s.Read(context.Background(), key())
	if e != nil || st.Revision != 2 || st.Working[0].Text != "process summary" {
		t.Fatal("cross-process CAS failed", e, st.Revision)
	}
}
