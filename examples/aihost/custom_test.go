package aihost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

// exampleProtocol demonstrates a separately registered wire format. It is a
// fixture, not another provider certification or an implicit universal adapter.
type exampleProtocol struct{}

func (exampleProtocol) Name() string { return "example-protocol" }
func (exampleProtocol) EncodeRequest(r ai.Request) ([]byte, error) {
	if r.Stream || len(r.Messages) != 1 || len(r.Messages[0].Calls) != 0 {
		return nil, ai.ErrUnsupported
	}
	return json.Marshal(map[string]string{"prompt": r.Messages[0].Text, "model": r.Target.Model})
}
func (exampleProtocol) Decode(b []byte) (ai.Response, error) {
	var wire struct {
		Answer string `json:"answer"`
		Model  string `json:"model"`
	}
	if e := strict(b, &wire); e != nil {
		return ai.Response{}, e
	}
	return ai.Response{Text: wire.Answer, Model: wire.Model, Stop: "stop"}, nil
}
func (exampleProtocol) Encode(r ai.Response) ([]byte, error) {
	return json.Marshal(map[string]string{"answer": r.Text, "model": r.Model})
}
func TestCustomProtocolRegistrationUsesSameHostPipeline(t *testing.T) {
	received := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actual, _ := io.ReadAll(r.Body)
		received <- actual
		if r.Header.Get("Authorization") != "fixture personal" {
			t.Error("custom call escaped authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"answer":"final answer","model":"custom-served"}`))
	}))
	defer upstream.Close()
	app := testApp(t.TempDir(), upstream.URL)
	app.Select = nil
	app.Tools = nil
	app.Target = ai.Route{Provider: "example", Endpoint: upstream.URL + "/custom", Model: "custom-requested", Protocol: "example-protocol", Account: "catalog-account", Version: "1"}
	app.Protocols["example-protocol"] = exampleProtocol{}
	app.Recorder.Evidence = func(in extension.Input) (ai.Route, error) {
		return ai.Route{Provider: "example", Endpoint: in.Request.URL, Protocol: "example-protocol", Account: "actual-account", Version: "1"}, nil
	}
	server := gateway(t, app, upstream.URL)
	defer server.Close()
	status, _, wire := post(t, server.URL, clientWire(t, []ai.Message{{Role: "user", Text: "custom prompt"}}, false))
	if status != 200 {
		t.Fatalf("custom %d %s", status, wire)
	}
	response, e := (ai.Adapter{Kind: ai.Chat}).Decode(wire)
	if e != nil || response.Text != "presented answer" || response.Model != "custom-served" {
		t.Fatalf("custom output %s %v", wire, e)
	}
	actual := <-received
	var sent map[string]string
	if json.Unmarshal(actual, &sent) != nil || sent["prompt"] != "custom prompt" || sent["model"] != "custom-requested" {
		t.Fatalf("actual custom request %s", actual)
	}
	state, e := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	if e != nil || state.Turn.Served.Protocol != "example-protocol" {
		t.Fatalf("custom provenance %+v %v", state, e)
	}
}
