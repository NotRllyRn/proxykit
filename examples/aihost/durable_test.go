package aihost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
)

func mixedUpstream(t *testing.T, calls *atomic.Int32, observers ...func([]byte)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		for _, observe := range observers {
			observe(body)
		}
		w.Header().Set("Content-Type", "application/json")
		response := ai.Response{Model: "served", Stop: "tool_use", Calls: []ai.ToolCall{{ID: "owned-id", Name: "owned", Arguments: json.RawMessage(`{"q":"safe"}`)}, {ID: "client-id", Name: "client-shell", Arguments: json.RawMessage(`{}`)}}}
		adapter := ai.Adapter{Kind: ai.Messages}
		if r.URL.Path == "/classify" {
			adapter.Kind = ai.Chat
			response = ai.Response{Model: "classifier", Stop: "stop", Text: "anthropic"}
		}
		wire, e := adapter.Encode(response)
		if e != nil {
			t.Error(e)
		}
		w.Write(wire)
	}))
}
func TestSocketDurableLimitsCannotResetOnRestart(t *testing.T) {
	t.Setenv("AIHOST_LIMIT_CALLS", "2")
	var calls atomic.Int32
	upstream := mixedUpstream(t, &calls)
	defer upstream.Close()
	dir := t.TempDir()
	first := runProcess(t, dir, upstream.URL, "personal", 0, clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false))
	if first.Status != 200 {
		t.Fatalf("handoff %d %s", first.Status, first.Body)
	}
	store := recall.Store{Dir: dir}
	key := recall.Key{Tenant: "personal", Conversation: "conversation"}
	st, e := store.Read(context.Background(), key)
	if e != nil {
		t.Fatal(e)
	}
	replay := append(st.Turn.Public, ai.Message{Role: "tool", ToolID: "client-id", Text: "client result"})
	final := runProcess(t, dir, upstream.URL, "personal", st.Revision, clientWire(t, replay, false))
	if final.Status < 400 || calls.Load() != 2 {
		t.Fatalf("restart reset admission: %d calls=%d %s", final.Status, calls.Load(), final.Body)
	}
	st, e = store.Read(context.Background(), key)
	if e != nil || !st.InFlight {
		t.Fatalf("failed continuation not quarantined: %+v %v", st, e)
	}
	// A different configured limit is not an implicit permission to reset counters.
	t.Setenv("AIHOST_LIMIT_CALLS", "20")
	again := runProcess(t, dir, upstream.URL, "personal", st.Revision, clientWire(t, replay, false))
	if again.Status < 400 || calls.Load() != 2 {
		t.Fatal("uncertain reservation bypassed")
	}
}
func TestSocketCrashUncertainOwnedEffectRejectedOnRestart(t *testing.T) {
	var calls atomic.Int32
	upstream := mixedUpstream(t, &calls)
	defer upstream.Close()
	dir := t.TempDir()
	wire := clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false)
	os.WriteFile(filepath.Join(dir, "request"), wire, 0600)
	cmd := exec.Command(os.Args[0], "-test.run=^TestSocketProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "AIHOST_CHILD=1", "AIHOST_DIR="+dir, "AIHOST_UPSTREAM="+upstream.URL, "AIHOST_TENANT=personal", "AIHOST_REV=0", "AIHOST_CRASH_AFTER_TOOL=1")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("crash helper %v %s", e, b)
	}
	store := recall.Store{Dir: dir}
	st, e := store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	if e != nil || !st.InFlight || st.Turn != nil {
		t.Fatalf("crash reservation %+v %v", st, e)
	}
	evidence, e := os.ReadFile(filepath.Join(dir, "effects"))
	if e != nil {
		t.Fatal(e)
	}
	retry := runProcess(t, dir, upstream.URL, "personal", st.Revision, wire)
	if retry.Status < 400 || calls.Load() != 2 {
		t.Fatalf("crash replayed: %d calls=%d", retry.Status, calls.Load())
	}
	after, _ := os.ReadFile(filepath.Join(dir, "effects"))
	if !bytes.Equal(after, evidence) {
		t.Fatal("owned effect duplicated after crash")
	}
}
