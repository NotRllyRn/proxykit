package system

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/extension"
)

// SSE is fully buffered by the selected managed call. This checks assembly of
// split upstream events, required originals and private tool continuation, NOT
// incremental client-facing semantic streaming.
func TestCombinedBufferedSplitSSEContinuation(t *testing.T) {
	p := newProvider()
	p.stream = true
	upstream := httptest.NewServer(p)
	defer upstream.Close()
	dir := t.TempDir()
	first := process(t, dir, upstream.URL, "personal", "", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}}))
	if first.Status != 200 {
		t.Fatalf("handoff: %d %s", first.Status, first.Body)
	}
	handoff, err := (ai.Adapter{Kind: ai.Chat}).Decode(first.Body)
	must(t, err)
	if len(handoff.Calls) != 1 || handoff.Calls[0].ID != "client-id" || bytes.Contains(first.Body, []byte("recall-id")) {
		t.Fatalf("unsafe handoff %s", first.Body)
	}
	st := state(t, dir)
	if st.Turn == nil || st.Budget.Calls != 5 {
		t.Fatalf("missing durable handoff: %+v", st)
	}
	second := process(t, dir, upstream.URL, "personal", "", st.Revision, replay(t, st))
	if second.Status != 200 {
		t.Fatalf("continuation: %d %s", second.Status, second.Body)
	}
	final, err := (ai.Adapter{Kind: ai.Chat}).Decode(second.Body)
	must(t, err)
	if final.Text != "presented answer" || final.Usage.Output == nil || *final.Usage.Output != 900 {
		t.Fatalf("presentation: %s", second.Body)
	}
	effects, err := os.ReadFile(filepath.Join(dir, "effects"))
	must(t, err)
	if string(effects) != "personal:recall-id\n" {
		t.Fatalf("effects: %s", effects)
	}
	batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
	selected := 0
	for _, batch := range batches {
		if len(batch) != 1 || !strings.HasSuffix(batch[0].Request.URL, "/messages") {
			continue
		}
		a := batch[0]
		selected++
		if !a.Complete || a.Response.Header.Get("Content-Type") != "text/event-stream" || !bytes.Contains(a.Body, []byte("event: message_start")) || a.Served.Protocol != ai.Messages || a.Usage.Output == nil || *a.Usage.Output != 7 {
			t.Fatalf("original SSE evidence: %+v", a)
		}
		decoded, _, err := (ai.Adapter{Kind: ai.Messages}).DecodeStream(bytes.NewReader(a.Body), ai.StreamLimits{Bytes: 4 << 20})
		must(t, err)
		if selected == 1 && (len(decoded.Calls) != 2 || decoded.Calls[0].ID != "recall-id" || decoded.Calls[1].ID != "client-id") {
			t.Fatalf("mixed SSE tools: %+v", decoded.Calls)
		}
		if selected == 2 && (decoded.Text != "final answer" || len(decoded.Calls) != 0) {
			t.Fatalf("resumed SSE: %+v", decoded)
		}
	}
	if selected != 2 {
		t.Fatalf("selected originals = %d", selected)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	resumed := false
	for _, r := range p.requests {
		if r.Path == "messages" && bytes.Contains(r.Body, []byte("tool_result")) {
			resumed = bytes.Contains(r.Body, []byte("private recalled")) && bytes.Contains(r.Body, []byte("client result"))
		}
	}
	if !resumed {
		t.Fatal("private and client results not both sent on resumed socket")
	}
}

// Shutdown cancels an active managed child and waits for the cooperative required
// recorder callback. The callback is released explicitly: no forced Go kill.
func TestCombinedShutdownWaitsForRequiredRecorder(t *testing.T) {
	arrived, canceled, sinkEntered, release := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"actual-classify","choices":[`)
		w.(http.Flusher).Flush()
		close(arrived)
		<-r.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	f := setup(t, dir, upstream.URL, "")
	var cleaned atomic.Int32
	f.cfg.Host.Registrations = append(f.cfg.Host.Registrations, extension.Registration{Owner: "shutdown", ID: "shutdown.cleanup", Stage: extension.Finish, Handler: func(context.Context, extension.Input) (extension.Result, error) { return extension.Result{}, nil }, Cleanup: func(context.Context) error { cleaned.Add(1); return nil }})
	original := f.app.Recorder.Sink
	f.app.Recorder.Sink = func(ctx context.Context, batch []ai.Attempt) error {
		close(sinkEntered)
		<-release
		return original(ctx, batch)
	}
	// Serve through the same real host, retaining its Close handle.
	var err error
	f.cfg.Host.Catalog, err = net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	f.cfg.Host.Enrollment, err = net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	f.cfg.Host.ServiceToken = "synthetic-service"
	f.cfg.Host.RetryInterval = time.Hour
	host, err := New(f.broker, f.cfg)
	must(t, err)
	server := httptest.NewServer(host)
	defer server.Close()
	req, err := http.NewRequest("POST", server.URL+"/v1/chat/completions", bytes.NewReader(wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	must(t, err)
	req.Header.Set("Authorization", "Bearer token-personal")
	req.Header.Set("Content-Type", "application/json")
	requestDone := make(chan struct{})
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		close(requestDone)
	}()
	waitSignal(t, arrived, "inference")
	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closeDone <- host.Close(ctx)
	}()
	waitSignal(t, canceled, "upstream cancellation")
	waitSignal(t, sinkEntered, "required recorder")
	select {
	case err := <-closeDone:
		t.Fatalf("shutdown completed before recorder release: %v", err)
	default:
	}
	if cleaned.Load() != 0 {
		t.Fatal("cleanup before active recorder exit")
	}
	expectDenied(t, post(t, server.URL, "personal", 0, wire(t, []ai.Message{{Role: "user", Text: "another"}})))
	close(release)
	select {
	case err := <-closeDone:
		must(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown stuck")
	}
	waitSignal(t, requestDone, "request completion")
	if cleaned.Load() != 1 {
		t.Fatalf("cleanup count %d", cleaned.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "effects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("new effect after shutdown")
	}
	batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
	// Cancellation before response headers produces no original snapshot; the
	// required sink still acknowledges the empty batch before shutdown exits.
	if len(batches) != 1 || len(batches[0]) != 0 {
		t.Fatalf("unexpected pre-header original: %+v", batches)
	}
	if !state(t, dir).InFlight {
		t.Fatal("canceled turn not quarantined")
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

// A valid endpoint alone is insufficient to authorize a protocol or account.
func TestCombinedTransportErrorDoesNotSelectOrRetry(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer upstream.Close()
	dir := t.TempDir()
	f := setup(t, dir, upstream.URL, "")
	f.app.Select = func(_ context.Context, _ *ai.Session, req ai.Request) (ai.Request, error) {
		req.Target = route(upstream.URL, "personal", "messages")
		return req, nil
	}
	server := serve(t, f)
	expectDenied(t, post(t, server.URL, "personal", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	if calls.Load() != 1 {
		t.Fatalf("transport failure retried: %d", calls.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "effects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed transport ran effect")
	}
	if !state(t, dir).InFlight {
		t.Fatal("transport uncertainty not quarantined")
	}
	batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
	if len(batches) != 1 || len(batches[0]) != 1 || batches[0][0].Complete || batches[0][0].Error == "" || batches[0][0].Usage.Output != nil || len(batches[0][0].Body) != 0 {
		t.Fatalf("transport error evidence: %+v", batches)
	}
}

func TestCombinedSameEndpointTupleMismatchRejected(t *testing.T) {
	for _, kind := range []string{"uncataloged", "wrong-account", "wrong-protocol"} {
		t.Run(kind, func(t *testing.T) {
			p := newProvider()
			upstream := httptest.NewServer(p)
			defer upstream.Close()
			dir := t.TempDir()
			f := setup(t, dir, upstream.URL, "")
			good := route(upstream.URL, "personal", "messages")
			bad := good
			switch kind {
			case "uncataloged":
				bad.Version = "not-cataloged"
			case "wrong-account":
				bad.Account = "work-messages"
			case "wrong-protocol":
				bad.Protocol = ai.Chat
				f.cfg.Routes = append(f.cfg.Routes, bad)
			}
			f.app.Select = func(_ context.Context, _ *ai.Session, req ai.Request) (ai.Request, error) {
				req.Target = bad
				return req, nil
			}
			server := serve(t, f)
			result := post(t, server.URL, "personal", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}}))
			expectDenied(t, result)
			if p.count() != 0 && kind != "wrong-protocol" {
				t.Fatalf("unauthorized tuple reached socket: %d", p.count())
			}
			if kind == "wrong-protocol" && p.count() != 1 {
				t.Fatalf("protocol mismatch not exercised on same endpoint: %d", p.count())
			}
			if _, err := os.Stat(filepath.Join(dir, "effects")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("mismatch executed effect")
			}
			if !state(t, dir).InFlight {
				t.Fatal("mismatch not quarantined")
			}
			before := p.count()
			expectDenied(t, post(t, server.URL, "personal", state(t, dir).Revision, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
			if p.count() != before {
				t.Fatal("mismatch replay admitted")
			}
		})
	}
}
