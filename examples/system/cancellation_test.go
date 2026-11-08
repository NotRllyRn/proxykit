package system

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/extension"
)

func TestCombinedCancellationPersistsPartialOriginalBeforeStopping(t *testing.T) {
	started, canceled, recorded := make(chan struct{}), make(chan struct{}), make(chan struct{})
	observed := make(chan struct{})
	var observedOnce atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		if r.Header.Get("Authorization") != "Bearer synthetic-personal-classify" {
			t.Error("missing broker credential")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"actual-classify","choices":[`)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	f := setup(t, dir, upstream.URL, "")
	f.cfg.Host.Registrations = append(f.cfg.Host.Registrations, extension.Registration{Owner: "cancel-fixture", ID: "cancel-fixture.original", Stage: extension.OriginalBody, Body: extension.Unit, After: []string{"ai-recorder.body"}, Handler: func(context.Context, extension.Input) (extension.Result, error) {
		if observedOnce.CompareAndSwap(false, true) {
			close(observed)
		}
		return extension.Result{}, nil
	}})
	sink := f.app.Recorder.Sink
	f.app.Recorder.Sink = func(ctx context.Context, attempts []ai.Attempt) error {
		err := sink(ctx, attempts)
		close(recorded)
		return err
	}
	server := serve(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", bytes.NewReader(wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	req.Header.Set("Authorization", "Bearer token-personal")
	req.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("classifier not started")
	}
	select {
	case <-observed:
	case <-time.After(3 * time.Second):
		t.Fatal("original prefix not observed")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client error %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client did not cancel")
	}
	for _, signal := range []chan struct{}{canceled, recorded} {
		select {
		case <-signal:
		case <-time.After(3 * time.Second):
			t.Fatal("cancellation/required recording did not finish")
		}
	}
	batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("partial original missing %+v", batches)
	}
	a := batches[0][0]
	if a.Complete || a.Error == "" || a.Usage.Input != nil || a.Usage.Output != nil || len(a.Body) == 0 {
		t.Fatalf("partial cancellation evidence %+v", a)
	}
	if _, err := os.Stat(filepath.Join(dir, "effects")); !os.IsNotExist(err) {
		t.Fatal("cancellation ran effect")
	}
	// Wait for Run's failed callback to unwind, then current-revision replay must
	// still reject uncertain work rather than restart inference with a new budget.
	deadline := time.Now().Add(time.Second)
	for !state(t, dir).InFlight && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	st := state(t, dir)
	if !st.InFlight {
		t.Fatal("canceled work not durably quarantined")
	}
	expectDenied(t, post(t, server.URL, "personal", st.Revision, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
}

func TestCombinedHostRootAttemptsRemainShared(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		b, _ := (ai.Adapter{Kind: ai.Chat}).Encode(ai.Response{Model: "actual-classify", Text: "messages", Stop: "stop"})
		w.Write(b)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	f := setup(t, dir, upstream.URL, "")
	// Root plus one classifier is the whole root budget. A second scoped call
	// must not create a fresh transport/root or bypass the engine's attempt bound.
	f.cfg.Host.Execution = extension.Config{Attempts: 2}
	f.app.Select = func(ctx context.Context, s *ai.Session, req ai.Request) (ai.Request, error) {
		req.Target = route(upstream.URL, "personal", "classify")
		req.Tools = nil
		req.Stream = false
		if _, err := s.Call(ctx, req); err != nil {
			return req, err
		}
		_, err := s.Call(ctx, req)
		return req, err
	}
	server := serve(t, f)
	expectDenied(t, post(t, server.URL, "personal", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	if calls.Load() != 1 {
		t.Fatalf("host budget bypass: %d socket calls", calls.Load())
	}
	if !state(t, dir).InFlight {
		t.Fatal("failed admission not quarantined")
	}
}
