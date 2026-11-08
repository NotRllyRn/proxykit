package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func TestConservativeUnsupportedAndMalformedRejection(t *testing.T) {
	for _, raw := range []string{
		`{"choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":-1}}`,
		`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":"unknown"}}`,
		`{"choices":[{"message":{"role":"tool","content":"wrong role"},"finish_reason":"stop"}]}`,
	} {
		if _, e := (Adapter{Chat}).Decode([]byte(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	// Signed or reordered blocks can pass unchanged, but never be flattened into
	// a new semantic history, even if the adapter recognizes adjacent text.
	reordered := []byte(`{"type":"message","role":"assistant","content":[{"type":"tool_use","id":"a","name":"search","input":{}},{"type":"text","text":"after tool"}],"stop_reason":"tool_use"}`)
	r, e := (Adapter{Messages}).Decode(reordered)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (Adapter{Messages}).Encode(r); !errors.Is(e, ErrUnsupported) {
		t.Fatal("reordered blocks flattened", e)
	}
	unknown := Response{Text: "x", Source: Chat, Extra: map[string]json.RawMessage{"future": json.RawMessage(`{"signature":"x"}`)}}
	if _, e = (Adapter{Chat}).EncodeStream(unknown); !errors.Is(e, ErrUnsupported) {
		t.Fatal("unknown fields lost in synthetic stream", e)
	}
	invalidStream := "data: {\"choices\":[{\"index\":\"bad\",\"delta\":{\"content\":\"x\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	if _, _, e = (Adapter{Chat}).DecodeStream(strings.NewReader(invalidStream), StreamLimits{}); e == nil {
		t.Fatal("invalid index accepted")
	}
	req := baseRequest()
	req.Tools = []ToolDefinition{{Name: "invalid", Schema: json.RawMessage(`{`)}}
	s := &Session{Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
		t.Fatal("malformed request dispatched")
		return Result{}, nil
	})}
	if _, e = s.Call(context.Background(), req); e == nil {
		t.Fatal("malformed request accepted")
	}
	if _, e = (Coordinator{Session: s}).Start(context.Background(), req); e == nil {
		t.Fatal("malformed coordinator request accepted")
	}
	var observed bool
	s = &Session{Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
		return Result{Served: baseRequest().Target, Response: Response{Opaque: json.RawMessage(`{`)}}, nil
	}), Observe: func(_ context.Context, o Observation) error {
		observed = o.Error != "" && o.Result.Response.Usage.Input == nil
		return nil
	}}
	if _, e = s.Call(context.Background(), baseRequest()); e == nil || !observed {
		t.Fatal("malformed failed outcome not observed", e)
	}
}
func TestRootToolCallBudgetAndDisabledBounds(t *testing.T) {
	var count atomic.Int32
	s := &Session{Limits: Limits{ToolCalls: 1, Parallel: 1}, Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
		return Result{Response: Response{Calls: []ToolCall{{"a", "search", json.RawMessage(`{}`)}, {"b", "search", json.RawMessage(`{}`)}}}}, nil
	})}
	c := Coordinator{Session: s, Tools: map[string]Tool{"search": func(context.Context, Identity, json.RawMessage) (string, error) { count.Add(1); return "ok", nil }}}
	if _, e := c.Start(context.Background(), baseRequest()); !errors.Is(e, ErrLimit) || count.Load() != 1 {
		t.Fatal("tool budget failed", e, count.Load())
	}
	// All defaults have explicit per-bound negative opt-out. It does not opt out
	// of ctx cancellation or release charges while trusted code is still running.
	disabled := Limits{Calls: -1, ToolCalls: -1, Rounds: -1, Parallel: -1, OutputBytes: -1, Timeout: -1}.defaults()
	if disabled.Calls != -1 || disabled.ToolCalls != -1 || disabled.Timeout != -1 {
		t.Fatal("opt-out ignored")
	}
}
