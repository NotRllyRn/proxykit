package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func num(v int64) *int64 { return &v }
func TestProtocolRoundTripAndStreams(t *testing.T) {
	for _, kind := range []string{Chat, Messages} {
		t.Run(kind, func(t *testing.T) {
			a := Adapter{kind}
			r := Response{ID: "answer", Model: "example", Text: "hello\nworld", Calls: []ToolCall{{"one", "search", json.RawMessage(`{"q":"a"}`)}, {"two", "search", json.RawMessage(`{"q":"b"}`)}}, Usage: Usage{num(0), num(7)}}
			b, e := a.Encode(r)
			if e != nil {
				t.Fatal(e)
			}
			got, e := a.Decode(b)
			if e != nil {
				t.Fatal(e)
			}
			if got.Text != r.Text || !reflect.DeepEqual(got.Calls, r.Calls) || got.Usage.Input == nil || *got.Usage.Input != 0 {
				t.Fatalf("bad ordinary response: %+v", got)
			}
			stream, e := a.EncodeStream(r)
			if e != nil {
				t.Fatal(e)
			}
			inc, _, e := a.DecodeStream(bytes.NewReader(stream), StreamLimits{})
			if e != nil {
				t.Fatalf("%s: %v", stream, e)
			}
			if inc.Text != r.Text || !reflect.DeepEqual(inc.Calls, r.Calls) || inc.Usage.Input == nil || *inc.Usage.Input != 0 || *inc.Usage.Output != 7 {
				t.Fatalf("incremental/final mismatch: %+v", inc)
			}
			for _, end := range []int{len(stream) / 2, len(stream) - 1} {
				if _, _, e = a.DecodeStream(bytes.NewReader(stream[:end]), StreamLimits{}); e == nil {
					t.Fatal("accepted truncation")
				}
			}
			if _, _, e = a.DecodeStream(bytes.NewReader(stream), StreamLimits{Bytes: 20}); !errors.Is(e, ErrLimit) {
				t.Fatal(e)
			}
		})
	}
}
func TestUnknownOpaqueAndUsage(t *testing.T) {
	a := Adapter{Chat}
	raw := []byte(`{"id":"a","model":"m","created":123,"x_future":{"v":1},"choices":[{"index":0,"message":{"role":"assistant","content":"old"},"finish_reason":"stop"}]}`)
	r, e := a.Decode(raw)
	if e != nil {
		t.Fatal(e)
	}
	if r.Usage.Input != nil || r.Usage.Output != nil {
		t.Fatal("missing usage became zero")
	}
	r.Text = "new"
	b, e := a.Encode(r)
	if e != nil {
		t.Fatal(e)
	}
	m, _ := object(b)
	if string(m["created"]) != "123" || string(m["x_future"]) != `{"v":1}` {
		t.Fatalf("lost unknown fields: %s", b)
	}
	if _, e = (Adapter{Messages}).Encode(r); !errors.Is(e, ErrUnsupported) {
		t.Fatal("unsafe migration", e)
	}
	signed := []byte(`{"type":"message","role":"assistant","id":"a","content":[{"type":"thinking","thinking":"private","signature":"signed"},{"type":"text","text":"ok"}],"usage":{"input_tokens":0,"output_tokens":0},"stop_reason":"end_turn"}`)
	ar, e := (Adapter{Messages}).Decode(signed)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (Adapter{Messages}).Encode(ar); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
	original, e := (Adapter{Messages}).Passthrough(ar)
	if e != nil || !bytes.Equal(original, signed) {
		t.Fatal("opaque loss")
	}
	d, e := ParseDocument(Messages, signed)
	if e != nil {
		t.Fatal(e)
	}
	d, e = d.Replace("model", json.RawMessage(`"served"`))
	if e != nil {
		t.Fatal(e)
	}
	b, e = d.Encode(Messages)
	if e != nil || !bytes.Contains(b, []byte(`"signature":"signed"`)) {
		t.Fatal("signed state lost", e)
	}
	if _, e = d.Encode(Chat); !errors.Is(e, ErrUnsupported) {
		t.Fatal(e)
	}
}
func TestSSEUnknownAndMalformed(t *testing.T) {
	raw := ": keepalive\nretry: 1000\nid: event-1\nevent: future\ndata: {\ndata: \"x\":1}\n\n"
	es, e := ReadEvents(strings.NewReader(raw), StreamLimits{})
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(WriteEvents(es), []byte("event: future\nid: event-1\n: keepalive\nretry: 1000\ndata: {\ndata: \"x\":1}\n\n")) {
		t.Fatal("unknown event loss")
	}
	_, ret, e := (Adapter{Messages}).DecodeStream(strings.NewReader(raw), StreamLimits{})
	if !errors.Is(e, ErrUnsupported) || len(ret) != 1 {
		t.Fatal("unknown was silently accepted/discarded", e)
	}
	for _, s := range []string{"data: {}", "data: {}\n", "data: [DONE]\n\n", "data: {bad}\n\n"} {
		if _, _, e := (Adapter{Chat}).DecodeStream(strings.NewReader(s), StreamLimits{}); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if _, e = ReadEvents(strings.NewReader(raw), StreamLimits{EventBytes: 10}); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
}
func TestRequestTranslationAndValidation(t *testing.T) {
	req := Request{Target: Route{Model: "m"}, Settings: Settings{MaxTokens: 100}, Tools: []ToolDefinition{{Name: "search", Schema: json.RawMessage(`{"type":"object"}`)}}, Messages: []Message{{Role: "user", Text: "q"}, {Role: "assistant", Calls: []ToolCall{{"a", "search", json.RawMessage(`{}`)}, {"b", "search", json.RawMessage(`{}`)}}}, {Role: "tool", ToolID: "a", Text: "1"}, {Role: "tool", ToolID: "b", Text: "2"}}}
	for _, k := range []string{Chat, Messages} {
		b, e := (Adapter{k}).EncodeRequest(req)
		if e != nil || !json.Valid(b) {
			t.Fatal(k, e)
		}
		if k == Messages && !bytes.Contains(b, []byte(`"tool_use_id":"b"`)) {
			t.Fatal(string(b))
		}
	}
	req.Messages = append(req.Messages, Message{Role: "tool", ToolID: "a"})
	if _, e := (Adapter{Chat}).EncodeRequest(req); e == nil {
		t.Fatal("duplicate result accepted")
	}
}
