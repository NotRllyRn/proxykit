package aihost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/extension"
)

func TestSocketCancellationRecordsOriginalAndStopsAdmission(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"partial":`))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	app := testApp(dir, upstream.URL)
	recorded := make(chan []ai.Attempt, 1)
	sink := app.Recorder.Sink
	app.Recorder.Sink = func(ctx context.Context, attempts []ai.Attempt) error {
		e := sink(ctx, attempts)
		recorded <- attempts
		return e
	}
	seen := make(chan struct{}, 1)
	observer := extension.Registration{Owner: "test", ID: "test.observed", Stage: extension.OriginalBody, Body: extension.Unit, Handler: func(context.Context, extension.Input) (extension.Result, error) {
		select {
		case seen <- struct{}{}:
		default:
		}
		return extension.Result{}, nil
	}}
	server := gateway(t, app, upstream.URL, observer)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", bytes.NewReader(clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false)))
	req.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		resp, e := http.DefaultClient.Do(req)
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		done <- e
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("no upstream dispatch")
	}
	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("original body not observed")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("caller cancellation ignored")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream not cancelled")
	}
	// Sink completion is an explicit awaited event, not a timing assumption.
	// A cancelled client can return before its server-side required capture finishes.
	var attempts []ai.Attempt
	select {
	case attempts = <-recorded:
	case <-time.After(2 * time.Second):
		t.Fatal("required recording did not finish")
	}
	evidence, _ := json.Marshal(attempts)
	if len(attempts) != 1 || attempts[0].Complete || attempts[0].Error == "" || string(attempts[0].Body) != `{"partial":` {
		t.Fatalf("cancelled original evidence %s", evidence)
	}
}
func TestOrdinarySocketPassthroughStaysLive(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	app := testApp(t.TempDir(), upstream.URL)
	server := gateway(t, app, upstream.URL)
	defer server.Close()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, e := client.Get(server.URL + "/ordinary")
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	prefix := make([]byte, 5)
	if _, e := io.ReadFull(resp.Body, prefix); e != nil || string(prefix) != "first" {
		t.Fatalf("ordinary path buffered/changed: %s %v", prefix, e)
	}
}

func TestLocalChildResponseCannotMasqueradeAsUpstream(t *testing.T) {
	// Even valid normalized JSON supplied locally must not become original attempt evidence.
	recorder := &ai.HostRecorder{Sink: func(context.Context, []ai.Attempt) error { return nil }}
	bridge := ai.HostManaged{Recorder: recorder, Protocols: map[string]ai.Protocol{ai.Chat: ai.Adapter{Kind: ai.Chat}}, CallChild: func(context.Context, *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewBufferString(`{"model":"fake","choices":[{"message":{"role":"assistant","content":"fake"},"finish_reason":"stop"}]}`))}, nil
	}}
	_, e := bridge.Call(context.Background(), ai.Request{Target: ai.Route{Protocol: ai.Chat, Endpoint: "http://example.invalid", Model: "m"}})
	if e == nil {
		t.Fatal("local response accepted without host observation")
	}
}
