package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestWireEnvelopeRejectsBeforeOwnedTools(t *testing.T) {
	valid := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(`{"type":"message","role":"assistant","content":[{"type":"tool_use","id":"a","name":"danger","input":{}}],"stop_reason":"tool_use"}`), &valid)
	cases := map[string]map[string]json.RawMessage{}
	for _, k := range []string{"type", "role", "stop_reason"} {
		for _, v := range []string{"", `null`, `42`, `"wrong"`} {
			m := clone(valid)
			if v == "" {
				delete(m, k)
			} else {
				m[k] = json.RawMessage(v)
			}
			cases[k+v] = m
		}
	}
	for _, v := range []string{`null`, `[]`, `42`, `"bad"`} {
		m := clone(valid)
		m["content"] = json.RawMessage(`[{"type":"tool_use","id":"a","name":"danger","input":` + v + `}]`)
		cases["input"+v] = m
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(m)
			effects := 0
			s := &Session{Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
				r, e := (Adapter{Messages}).Decode(raw)
				return Result{Response: r}, e
			})}
			c := Coordinator{Session: s, Tools: map[string]Tool{"danger": func(context.Context, Identity, json.RawMessage) (string, error) { effects++; return "bad", nil }}}
			if _, e := c.Start(context.Background(), baseRequest()); e == nil || effects != 0 {
				t.Fatalf("error=%v effects=%d", e, effects)
			}
		})
	}
	// Synthetic responses remain convenient; they are not wire envelopes.
	if _, e := (Adapter{Messages}).Encode(Response{Text: "ok"}); e != nil {
		t.Fatal(e)
	}
}

func chatSSE(choice string, root string) []byte {
	return []byte("data: {\"choices\":[" + choice + "]" + root + "}\n\ndata: [DONE]\n\n")
}
func TestBufferedStreamUnknownEvidenceAndMetadata(t *testing.T) {
	a := Adapter{Chat}
	for _, choice := range []string{
		`{"index":0,"delta":{"content":"ok"},"finish_reason":"stop","future":{"signature":"keep"}}`,
		`{"index":0,"delta":{"content":"ok","future":{}},"finish_reason":"stop"}`,
		`{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"x","arguments":"{}","future":{}}}]},"finish_reason":"tool_calls"}`,
		`{"index":0,"delta":{"tool_calls":[{"index":0,"id":"a","type":"function","function":{"name":"x","arguments":"{}"},"future":{}}]},"finish_reason":"tool_calls"}`,
	} {
		r, e := a.DecodeBuffered(chatSSE(choice, ""), true, StreamLimits{})
		if e == nil || len(r.Events) != 2 || !strings.Contains(r.Events[0].Data, "future") {
			t.Fatalf("lost unsupported evidence: %+v %v", r, e)
		}
	}
	choice := `{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}`
	if _, e := a.DecodeBuffered(chatSSE(choice, `,"future":{}`), true, StreamLimits{}); e == nil {
		t.Fatal("root unknown accepted")
	}
	r, e := a.DecodeBuffered(chatSSE(choice, `,"system_fingerprint":"signed","service_tier":"default"`), true, StreamLimits{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := a.Encode(r)
	if e != nil || !strings.Contains(string(b), `"system_fingerprint":"signed"`) || !strings.Contains(string(b), `"service_tier":"default"`) {
		t.Fatalf("metadata lost: %s %v", b, e)
	}
	if _, e = (Adapter{Messages}).Encode(r); !errors.Is(e, ErrUnsupported) {
		t.Fatal("metadata converted", e)
	}
	if _, e = a.EncodeStream(r); !errors.Is(e, ErrUnsupported) {
		t.Fatal("metadata synthesized", e)
	}
}
func TestMalformedKnownWireFields(t *testing.T) {
	a := Adapter{Chat}
	for _, delta := range []string{
		`{"role":"user","content":"ok","tool_calls":"not-an-array"}`,
		`{"role":"user","content":"ok"}`, `{"role":null}`, `{"content":42}`,
		`{"tool_calls":null}`, `{"tool_calls":[null]}`, `{"tool_calls":[{"index":0,"function":"bad"}]}`,
		`{"tool_calls":[{"index":0,"id":42}]}`,
		`{"tool_calls":[{"index":0,"function":{"name":null,"arguments":42}}]}`,
	} {
		if _, e := a.DecodeBuffered(chatSSE(`{"index":0,"delta":`+delta+`,"finish_reason":"stop"}`, ""), true, StreamLimits{}); e == nil {
			t.Fatalf("accepted %s", delta)
		}
	}
	for _, message := range []string{`{"role":"assistant","tool_calls":"bad"}`, `{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"x","arguments":{}}}]}`, `{"role":null}`} {
		if _, e := a.Decode([]byte(`{"choices":[{"message":` + message + `,"finish_reason":"tool_calls"}]}`)); e == nil {
			t.Fatalf("ordinary accepted %s", message)
		}
	}
	for _, root := range []string{`,"id":42`, `,"model":null`, `,"created":"now"`, `,"usage":[]`} {
		if _, e := a.DecodeBuffered(chatSSE(`{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}`, root), true, StreamLimits{}); e == nil {
			t.Fatalf("accepted %s", root)
		}
		raw := `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]` + root + `}`
		if _, e := a.Decode([]byte(raw)); e == nil {
			t.Fatalf("ordinary accepted %s", root)
		}
	}
	anth := Adapter{Messages}
	validStream, _ := anth.EncodeStream(Response{Text: "ok"})
	es, _ := ReadEvents(strings.NewReader(string(validStream)), StreamLimits{})
	for _, raw := range []string{"", `null`, `{}`, `{"type":"message","role":"user","content":[]}`, `{"type":"message","role":"assistant","content":"bad"}`} {
		copyEvents := clone(es)
		m, _ := object([]byte(copyEvents[0].Data))
		if raw == "" {
			delete(m, "message")
		} else {
			m["message"] = json.RawMessage(raw)
		}
		b, _ := json.Marshal(m)
		copyEvents[0].Data = string(b)
		if _, e := anth.DecodeBuffered(WriteEvents(copyEvents), true, StreamLimits{}); e == nil {
			t.Fatalf("accepted start message %s", raw)
		}
	}
	for _, response := range []Response{{Text: "ok"}, {Calls: []ToolCall{{"a", "x", json.RawMessage(`{}`)}}}} {
		a := Adapter{Messages}
		b, e := a.EncodeStream(response)
		if e != nil {
			t.Fatal(e)
		}
		key := `"text":"ok"`
		if len(response.Calls) > 0 {
			key = `"partial_json":"{}"`
		}
		for _, bad := range []string{`null`, `42`, `{}`} {
			field := strings.Split(key, ":")[0]
			mutated := strings.Replace(string(b), key, field+":"+bad, 1)
			if _, e := a.DecodeBuffered([]byte(mutated), true, StreamLimits{}); e == nil {
				t.Fatalf("accepted %s", mutated)
			}
		}
	}
}
func TestObservationCancellationPreventsSuccess(t *testing.T) {
	for _, coordinator := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "coordinator"}[coordinator], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := false
			s := &Session{Limits: Limits{Timeout: -1}, Managed: ManagedFunc(func(context.Context, Request) (Result, error) {
				return Result{Response: Response{Text: "original"}}, nil
			}), Observe: func(ctx context.Context, o Observation) error {
				observed = o.Error == "" && o.Result.Response.Text == "original"
				// Cancel at the observation boundary, not on a timer that can expire
				// before dispatch on a loaded race-test runner. A nil observer return
				// must not turn a canceled operation into a successful answer.
				cancel()
				<-ctx.Done()
				return nil
			}}
			var e error
			if coordinator {
				_, e = (Coordinator{Session: s}).Start(ctx, baseRequest())
			} else {
				_, e = s.Call(ctx, baseRequest())
			}
			if !errors.Is(e, context.Canceled) || !observed {
				t.Fatalf("error %v original observed %v", e, observed)
			}
		})
	}
}
func TestUsageEditsAndNonfiniteSettings(t *testing.T) {
	a := Adapter{Chat}
	r, e := a.Decode([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	if e != nil {
		t.Fatal(e)
	}
	for _, u := range []Usage{{}, {num(0), nil}, {nil, num(0)}, {num(2), num(3)}} {
		r.Usage = u
		b, e := a.Encode(r)
		if e != nil {
			t.Fatal(e)
		}
		m, _ := object(b)
		usage := get[map[string]json.RawMessage](m, "usage")
		if u.Input == nil && len(usage["prompt_tokens"]) > 0 || u.Output == nil && len(usage["completion_tokens"]) > 0 {
			t.Fatalf("stale usage %s", b)
		}
		if u.Input == nil || u.Output == nil {
			if len(usage["total_tokens"]) > 0 {
				t.Fatalf("stale total %s", b)
			}
		} else if string(usage["total_tokens"]) != "5" {
			t.Fatalf("wrong total %s", b)
		}
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		req := baseRequest()
		req.Settings.Temperature = &v
		for _, kind := range []string{Chat, Messages} {
			if _, e := (Adapter{kind}).EncodeRequest(req); e == nil {
				t.Fatal("nonfinite setting accepted")
			}
		}
	}
}
