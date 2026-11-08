package aihost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

func post(t *testing.T, url string, b []byte) (int, http.Header, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", url+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	body, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	return resp.StatusCode, resp.Header, body
}
func TestSocketFailuresBeforeToolsOrDelivery(t *testing.T) {
	for _, mode := range []string{"sink", "overflow", "malformed", "signed-migration", "spoofed-tool-identity"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Private", "secret")
				w.WriteHeader(103)
				if r.URL.Path == "/classify" {
					wire, _ := (ai.Adapter{Kind: ai.Chat}).Encode(ai.Response{Model: "classifier", Text: "anthropic", Stop: "stop"})
					w.Write(wire)
					return
				}
				if mode == "signed-migration" {
					w.Write([]byte(`{"type":"message","role":"assistant","model":"served","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"private","signature":"signed"},{"type":"tool_use","id":"owned","name":"owned","input":{"q":"safe"}}]}`))
					return
				}
				if mode == "malformed" {
					w.Write([]byte(`{"type":"message","role":"user","model":"served","stop_reason":"tool_use","content":[{"type":"tool_use","id":"owned","name":"owned","input":{"q":"safe"}}]}`))
					return
				}
				wire, _ := (ai.Adapter{Kind: ai.Messages}).Encode(ai.Response{Model: "served", Stop: "tool_use", Calls: []ai.ToolCall{{ID: "owned", Name: "owned", Arguments: json.RawMessage(`{"q":"safe","tenant":"attacker"}`)}}})
				w.Write(wire)
			}))
			defer upstream.Close()
			dir := t.TempDir()
			app := testApp(dir, upstream.URL)
			if mode == "sink" {
				app.Recorder.Sink = func(context.Context, []ai.Attempt) error { return errors.New("durable sink unavailable") }
			}
			if mode == "overflow" {
				app.Recorder.MaxBytes = 32
			}
			server := gateway(t, app, upstream.URL)
			defer server.Close()
			status, header, body := post(t, server.URL, clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false))
			if status < 400 || header.Get("X-Private") != "" || bytes.Contains(body, []byte("tool_calls")) {
				t.Fatalf("failure delivered upstream: %d %v %s", status, header, body)
			}
			if _, e := os.Stat(filepath.Join(dir, "effects")); !errors.Is(e, os.ErrNotExist) {
				t.Fatalf("owned effects before validation: %v", e)
			}
			st, e := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
			if e != nil || !st.InFlight {
				t.Fatalf("failure not crash-safe: %+v %v", st, e)
			}
			before := calls.Load()
			_, _, _ = post(t, server.URL, clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false))
			if calls.Load() != before {
				t.Fatal("failed request replay repeated upstream")
			}
		})
	}
}
func TestSocketCandidateLoserCancellationAndAccounting(t *testing.T) {
	slowStarted := make(chan struct{})
	slowCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/slow" {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			close(slowStarted)
			<-r.Context().Done()
			close(slowCancelled)
			return
		}
		if r.URL.Path == "/fast" {
			<-slowStarted
		}
		wire, _ := (ai.Adapter{Kind: ai.Chat}).Encode(ai.Response{Model: "actual-model", Text: "final answer", Stop: "stop"})
		w.Write(wire)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	app := testApp(dir, upstream.URL)
	// All three routes speak Chat; authoritative evidence is registered explicitly.
	var mu sync.Mutex
	var logical []ai.Observation
	app.Select = func(ctx context.Context, s *ai.Session, req ai.Request) (ai.Request, error) {
		s.Observe = func(_ context.Context, o ai.Observation) error {
			mu.Lock()
			logical = append(logical, o)
			mu.Unlock()
			return nil
		}
		fast, slow := req, req
		fast.Tools = nil
		slow.Tools = nil
		fast.Target = ai.Route{Model: "candidate", Endpoint: upstream.URL + "/fast", Protocol: ai.Chat}
		slow.Target = fast.Target
		slow.Target.Endpoint = upstream.URL + "/slow"
		winner, e := ai.Candidates(ctx, s, []ai.Request{fast, slow}, func(r ai.Result) bool { return r.Response.Text == "final answer" })
		if e != nil {
			return req, e
		}
		req.Target = winner.Served
		req.Target.Endpoint = upstream.URL + "/final"
		return req, nil
	}
	app.Recorder.Evidence = func(in extension.Input) (ai.Route, error) {
		return ai.Route{Provider: "custom-catalog", Endpoint: in.Request.URL, Protocol: ai.Chat, Account: "actual-account", Version: "1"}, nil
	}
	server := gateway(t, app, upstream.URL)
	defer server.Close()
	status, _, body := post(t, server.URL, clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false))
	if status != 200 {
		t.Fatalf("candidates %d %s", status, body)
	}
	select {
	case <-slowCancelled:
	case <-time.After(time.Second):
		t.Fatal("loser was not cancelled")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logical) != 3 {
		t.Fatalf("logical outcomes %d", len(logical))
	}
	failures := 0
	for _, o := range logical {
		if o.Error != "" {
			failures++
		}
	}
	if failures != 1 {
		t.Fatalf("cancelled outcome missing: %+v", logical)
	}
	st, e := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	if e != nil || st.Budget.Calls != 3 {
		t.Fatalf("loser admission lost: %+v %v", st, e)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "attempts"))
	if len(bytes.Split(bytes.TrimSpace(data), []byte{'\n'})) != 3 {
		t.Fatalf("actual attempts missing %s", data)
	}
}

func TestIngressRejectsSpoofedStateAndPreservesOrdinaryLivePath(t *testing.T) {
	app := &App{Path: "/managed", MaxRequestBytes: 32}
	var entered atomic.Bool
	handler := app.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered.Store(true); w.Write([]byte("live")) }))
	for _, body := range []string{`{"Turn":{}}`, strings.Repeat("x", 33)} {
		req := httptest.NewRequest("POST", "/managed", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 401 || entered.Load() {
			t.Fatal("untrusted identity accepted")
		}
	}
	// A never-ending body on an ordinary path is not read or buffered by App.
	req := httptest.NewRequest("POST", "/ordinary", neverRead{})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !entered.Load() || w.Body.String() != "live" {
		t.Fatal("ordinary path changed")
	}
	for _, body := range []string{`{"model":"m","messages":[],"Turn":{}}`, `{"model":"m","messages":[],"tenant":"attacker"}`, `{"model":"m","messages":[],"previous_response_id":"provider-held"}`} {
		if _, e := decodeRequest([]byte(body)); e == nil {
			t.Fatal("unsupported/spoofed state accepted")
		}
	}
}

type neverRead struct{}

func (neverRead) Read([]byte) (int, error) { panic("ordinary body was read") }
