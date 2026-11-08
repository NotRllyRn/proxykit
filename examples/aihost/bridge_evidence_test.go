package aihost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

// Unlike the classifier-first failures, these failures target a tool-producing
// selected response, including the first inference after a durable client handoff.
func TestSocketRequiredRecordingFailureAtSelectedAndResumedInference(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		for _, mode := range []string{"sink", "capture"} {
			t.Run(fmt.Sprintf("resumed=%v/%s", resumed, mode), func(t *testing.T) {
				var calls, selected atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Type", "application/json")
					if r.URL.Path == "/classify" {
						wire, _ := (ai.Adapter{Kind: ai.Chat}).Encode(ai.Response{Model: "classifier", Text: "anthropic", Stop: "stop"})
						w.Write(wire)
						return
					}
					n := selected.Add(1)
					response := ai.Response{Model: "served", Stop: "tool_use", Calls: []ai.ToolCall{{ID: "failed-owned", Name: "owned", Arguments: json.RawMessage(`{"q":"safe"}`)}}}
					if resumed && n == 1 {
						response.Calls = []ai.ToolCall{{ID: "prior-owned", Name: "owned", Arguments: json.RawMessage(`{"q":"safe"}`)}, {ID: "client-id", Name: "client-shell", Arguments: json.RawMessage(`{}`)}}
					} else if mode == "capture" {
						response.Text = strings.Repeat("x", 2048)
					}
					wire, _ := (ai.Adapter{Kind: ai.Messages}).Encode(response)
					w.Write(wire)
				}))
				defer upstream.Close()
				dir := t.TempDir()
				app := testApp(dir, upstream.URL)
				app.Recorder.MaxBytes = 1024 // classifier and initial mixed response fit
				durable := app.Recorder.Sink
				var mu sync.Mutex
				var batches [][]ai.Attempt
				app.Recorder.Sink = func(ctx context.Context, attempts []ai.Attempt) error {
					mu.Lock()
					batches = append(batches, attempts)
					mu.Unlock()
					if mode == "sink" && len(attempts) == 1 && bytes.Contains(attempts[0].Body, []byte("failed-owned")) {
						return errors.New("selected durable sink unavailable")
					}
					return durable(ctx, attempts)
				}
				server := gateway(t, app, upstream.URL)
				defer server.Close()
				key := recall.Key{Tenant: "personal", Conversation: "conversation"}
				wire := clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false)
				revision := uint64(0)
				priorEffects := []byte(nil)
				priorTurn := []byte("null")
				if resumed {
					status, _, body := post(t, server.URL, wire)
					if status != 200 {
						t.Fatalf("initial handoff %d %s", status, body)
					}
					st, err := app.Store.Read(context.Background(), key)
					if err != nil || st.Turn == nil || st.InFlight {
						t.Fatalf("handoff: %+v %v", st, err)
					}
					revision = st.Revision
					priorTurn, err = json.Marshal(st.Turn)
					if err != nil {
						t.Fatal(err)
					}
					wire = clientWire(t, append(st.Turn.Public, ai.Message{Role: "tool", ToolID: "client-id", Text: "client result"}), false)
					priorEffects, err = os.ReadFile(filepath.Join(dir, "effects"))
					if err != nil || len(bytes.Split(bytes.TrimSpace(priorEffects), []byte{'\n'})) != 1 {
						t.Fatalf("initial effect: %s %v", priorEffects, err)
					}
				}
				request := func() (int, []byte) {
					req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", bytes.NewReader(wire))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Recall-Revision", strconv.FormatUint(revision, 10))
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					if err != nil {
						t.Fatal(err)
					}
					return resp.StatusCode, body
				}
				status, body := request()
				if status < 400 || bytes.Contains(body, []byte("tool_calls")) || bytes.Contains(body, []byte("failed-owned")) {
					t.Fatalf("failed response selected: %d %s", status, body)
				}
				effects, err := os.ReadFile(filepath.Join(dir, "effects"))
				if !bytes.Equal(effects, priorEffects) || (!resumed && !errors.Is(err, os.ErrNotExist)) || (resumed && err != nil) {
					t.Fatalf("effects attributable to failed response: %s %v", effects, err)
				}
				st, err := app.Store.Read(context.Background(), key)
				expectedRevision := revision + 1 // Run's durable in-flight checkpoint
				if !resumed {
					expectedRevision++ // initial Archive
				}
				if err != nil || !st.InFlight || st.Revision != expectedRevision {
					t.Fatalf("not durably quarantined: %+v %v", st, err)
				}
				checkpoint, err := json.Marshal(st.Turn)
				if err != nil || !bytes.Equal(checkpoint, priorTurn) {
					t.Fatalf("failed response checkpointed as success: %s %v", checkpoint, err)
				}
				before := calls.Load()
				status, _ = request()
				// Also retry with the persisted revision, ruling out revision conflict as
				// the sole reason no new upstream inference is admitted.
				revision = st.Revision
				currentStatus, _ := request()
				if status < 400 || currentStatus < 400 || calls.Load() != before {
					t.Fatal("quarantined replay admitted upstream")
				}
				mu.Lock()
				defer mu.Unlock()
				want := 2
				if resumed {
					want = 3
				}
				if len(batches) != want || len(batches[0]) != 1 || !batches[0][0].Complete || batches[0][0].Served.Model != "classifier" {
					t.Fatalf("classification not recorded first: %+v", batches)
				}
				failed := batches[len(batches)-1]
				if len(failed) != 1 || !strings.HasSuffix(failed[0].Request.URL, "/messages") {
					t.Fatalf("wrong failure phase: %+v", failed)
				}
				if mode == "sink" && (!failed[0].Complete || !bytes.Contains(failed[0].Body, []byte("failed-owned"))) {
					t.Fatalf("sink did not receive tool response: %+v", failed)
				}
				if mode == "capture" && (failed[0].Complete || len(failed[0].Body) > 1024 || failed[0].Error == "") {
					t.Fatalf("capture failure not recorded: %+v", failed)
				}
			})
		}
	}
}

// Candidates in failure_test.go are separate HostManaged.Call invocations.
// Here one selected invocation sees its own original plus a nested Input.Call's
// original, and the nested response replaces its parent's body.
func TestSocketNestedResponseReplacementRejectsMultipleOriginalSources(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		response := ai.Response{Model: "served", Stop: "tool_use", Calls: []ai.ToolCall{{ID: "nested-owned", Name: "owned", Arguments: json.RawMessage(`{"q":"safe"}`)}}}
		kind := ai.Messages
		if r.URL.Path == "/classify" {
			kind = ai.Chat
			response = ai.Response{Model: "classifier", Text: "anthropic", Stop: "stop"}
		}
		wire, _ := (ai.Adapter{Kind: kind}).Encode(response)
		w.Write(wire)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	app := testApp(dir, upstream.URL)
	durable := app.Recorder.Sink
	var mu sync.Mutex
	var batches [][]ai.Attempt
	app.Recorder.Sink = func(ctx context.Context, attempts []ai.Attempt) error {
		mu.Lock()
		batches = append(batches, attempts)
		mu.Unlock()
		return durable(ctx, attempts)
	}
	var nestedStarted, replaced atomic.Bool
	nested := extension.Registration{Owner: "nested", ID: "nested.replace", Stage: extension.ResponseBody, Body: extension.Unit, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth != 1 || !strings.HasSuffix(in.Request.URL, "/messages") || !nestedStarted.CompareAndSwap(false, true) {
			return extension.Result{}, nil
		}
		req, err := http.NewRequestWithContext(ctx, "POST", upstream.URL+"/nested", strings.NewReader(`{}`))
		if err != nil {
			return extension.Result{}, err
		}
		child, err := in.Call(ctx, req)
		if err != nil {
			return extension.Result{}, err
		}
		defer child.Body.Close()
		body, err := io.ReadAll(child.Body)
		if err != nil {
			return extension.Result{}, err
		}
		msg := in.Response
		msg.Body = body
		replaced.Store(true)
		return extension.Result{Action: extension.Replace, Message: &msg}, nil
	}}
	// Observe logical errors to distinguish the explicit ambiguity branch from
	// rejection merely caused by the deliberately invalid presentation bytes.
	originalSelect := app.Select
	var logicalMu sync.Mutex
	var outcomes []ai.Observation
	app.Select = func(ctx context.Context, s *ai.Session, req ai.Request) (ai.Request, error) {
		s.Observe = func(_ context.Context, o ai.Observation) error {
			logicalMu.Lock()
			outcomes = append(outcomes, o)
			logicalMu.Unlock()
			return nil
		}
		return originalSelect(ctx, s, req)
	}
	server := gateway(t, app, upstream.URL, nested)
	defer server.Close()
	wire := clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false)
	status, _, body := post(t, server.URL, wire)
	if status < 400 || bytes.Contains(body, []byte("tool_calls")) || !replaced.Load() {
		t.Fatalf("nested replacement escaped: %d %s", status, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "effects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("nested tools executed: %v", err)
	}
	st, err := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	if err != nil || !st.InFlight {
		t.Fatalf("not quarantined: %+v %v", st, err)
	}
	before := calls.Load()
	post(t, server.URL, wire)
	if before != 3 || calls.Load() != before {
		t.Fatalf("unexpected upstream admission: %d -> %d", before, calls.Load())
	}
	logicalMu.Lock()
	defer logicalMu.Unlock()
	if len(outcomes) != 2 || outcomes[0].Error != "" || !strings.Contains(outcomes[1].Error, "ambiguous multiple upstream sources") {
		t.Fatalf("wrong logical rejection: %+v", outcomes)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 2 || len(batches[0]) != 1 || len(batches[1]) != 2 {
		t.Fatalf("not two sources inside one call: %+v", batches)
	}
	a, b := batches[1][0], batches[1][1]
	if a.Logical != b.Logical || a.Identity.Exchange == b.Identity.Exchange || b.Identity.Parent != a.Identity.Exchange || a.Identity.Depth != 1 || b.Identity.Depth != 2 {
		t.Fatalf("not nested provenance: %+v %+v", a, b)
	}
	if !strings.HasSuffix(a.Request.URL, "/messages") || !strings.HasSuffix(b.Request.URL, "/nested") || a.Complete || b.Complete {
		t.Fatalf("wrong original sources: %+v %+v", a, b)
	}
	for _, attempt := range batches[1] {
		if !bytes.Contains(attempt.Body, []byte("nested-owned")) || attempt.Served.Model != "served" {
			t.Fatalf("original missing: %+v", attempt)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "attempts"))
	if err != nil || len(bytes.Split(bytes.TrimSpace(data), []byte{'\n'})) != 2 {
		t.Fatalf("sink not durable: %s %v", data, err)
	}
}
