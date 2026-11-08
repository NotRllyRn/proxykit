package system

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/aihost"
	"github.com/NotRllyRn/proxykit/examples/brokerapp"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

func route(upstream, tenant, path string) ai.Route {
	protocol := ai.Messages
	if path == "classify" {
		protocol = ai.Chat
	}
	return ai.Route{Provider: "fixture", Model: "actual-" + path, Account: tenant + "-" + path, Endpoint: upstream + "/" + path, Protocol: protocol, Version: "v1"}
}
func routes(upstream string) []ai.Route {
	var out []ai.Route
	for _, tenant := range []string{"personal", "work"} {
		for _, path := range []string{"classify", "winner", "loser", "probe", "messages"} {
			out = append(out, route(upstream, tenant, path))
		}
	}
	return out
}
func target(r ai.Route) ai.Target {
	return ai.Target{Route: r, Tools: true, Stream: true, MaxTokens: 1000}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func readLines[T any](path string) ([]T, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []T
	for _, line := range bytes.Split(bytes.TrimSpace(b), []byte{'\n'}) {
		var v T
		if err := json.Unmarshal(line, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func lines[T any](t *testing.T, path string) []T {
	t.Helper()
	out, err := readLines[T](path)
	must(t, err)
	return out
}
func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

type fixture struct {
	app    *aihost.App
	cfg    Config
	store  *brokerapp.Store
	broker *brokerapp.Broker
	dir    string
}

func setup(t *testing.T, dir, upstream, mode string) fixture {
	t.Helper()
	store, err := brokerapp.OpenStore(filepath.Join(dir, "broker"))
	must(t, err)
	for _, r := range routes(upstream) {
		a := brokerapp.Account{ID: r.Account, Label: "Example user", Callers: []string{strings.SplitN(r.Account, "-", 2)[0]}, Endpoint: r.Endpoint, Provider: r.Provider, Model: r.Model, Active: true, Credential: brokerapp.Credential{Token: "synthetic-" + r.Account, Expires: time.Now().Add(time.Hour)}}
		// Reopen is single-process ownership; preserve catalog revocations on replay.
		if _, err := os.Stat(filepath.Join(dir, "catalog-initialized")); os.IsNotExist(err) {
			must(t, store.Activate(a))
		}
	}
	must(t, os.WriteFile(filepath.Join(dir, "catalog-initialized"), nil, 0600))
	var endpoints []string
	for _, r := range routes(upstream) {
		endpoints = append(endpoints, r.Endpoint)
	}
	b, err := brokerapp.New(store, brokerapp.Config{CallerTokens: map[string]string{"token-personal": "personal", "token-work": "work"}, AllowedEndpoints: endpoints})
	must(t, err)
	recorder := &ai.HostRecorder{Sink: FileSink(filepath.Join(dir, "attempts"))}
	originalSink := recorder.Sink
	recorder.Sink = func(ctx context.Context, attempts []ai.Attempt) error {
		if mode == "sink" {
			for _, a := range attempts {
				if strings.HasSuffix(a.Request.URL, "/messages") {
					return errors.New("recorder unavailable")
				}
			}
		}
		return originalSink(ctx, attempts)
	}
	if mode == "capture" {
		recorder.MaxBytes = 1600
	}
	app := &aihost.App{Path: "/v1/chat/completions", Store: recall.Store{Dir: filepath.Join(dir, "recall")}, Target: route(upstream, "personal", "messages"), Protocols: map[string]ai.Protocol{ai.Chat: ai.Adapter{Kind: ai.Chat}, ai.Messages: ai.Adapter{Kind: ai.Messages}}, Recorder: recorder}
	app.Tools = []aihost.OwnedTool{{Definition: ai.ToolDefinition{Name: "recall", Schema: json.RawMessage(`{"type":"object","additionalProperties":false}`)}, Validate: func(raw json.RawMessage) error {
		if string(raw) != "{}" {
			return errors.New("invalid recall arguments")
		}
		return nil
	}, Run: func(ctx context.Context, id ai.Identity, _ json.RawMessage) (string, error) {
		// The actual durable archive is read by an owned recall tool, not a fake
		// Managed response. Its private result must survive client replay/restart.
		st, err := app.Store.Read(ctx, recall.Key{Tenant: id.Tenant, Conversation: id.Conversation})
		if err != nil {
			return "", err
		}
		if len(st.Original) == 0 {
			return "", errors.New("archive absent")
		}
		batches, err := readLines[[]ai.Attempt](filepath.Join(dir, "attempts"))
		if err != nil {
			return "", err
		}
		selectedRecorded := false
		for _, batch := range batches {
			for _, a := range batch {
				if strings.HasSuffix(a.Request.URL, "/messages") && a.Complete && bytes.Contains(a.Body, []byte("recall-id")) {
					selectedRecorded = true
				}
			}
		}
		if !selectedRecorded {
			return "", errors.New("effect preceded required selected recording")
		}
		f, err := os.OpenFile(filepath.Join(dir, "effects"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		_, err = fmt.Fprintln(f, id.Tenant+":"+ai.ToolCallID(ctx))
		if err == nil {
			err = f.Sync()
		}
		f.Close()
		return "private recalled " + st.Original[0].Text, err
	}}}
	app.Select = func(ctx context.Context, s *ai.Session, req ai.Request) (ai.Request, error) {
		tenant := req.Identity.Tenant
		classifier := req
		classifier.Target = route(upstream, tenant, "classify")
		classifier.Tools = nil
		classifier.Stream = false
		chosen, err := ai.Classify(ctx, s, classifier, map[string]ai.Target{"messages": target(route(upstream, tenant, "messages"))})
		if err != nil {
			return req, err
		}
		candidate := req
		candidate.Tools = nil
		candidate.Stream = false
		one, two := candidate, candidate
		one.Target = route(upstream, tenant, "winner")
		two.Target = route(upstream, tenant, "loser")
		if _, err = ai.Candidates(ctx, s, []ai.Request{one, two}, func(r ai.Result) bool { return r.Response.Text == "winner" }); err != nil {
			return req, err
		}
		if _, err = ai.Reroute(ctx, s, candidate, target(route(upstream, tenant, "probe")), ai.Affinity{}, true, false); err != nil {
			return req, err
		}
		return ai.Prepare(req, chosen, ai.Affinity{}, true)
	}
	if mode == "budget" {
		app.Limits.Calls = 5
	}
	if mode == "deadline" {
		app.Lifetime = 2 * time.Second
	}
	if mode == "archive" {
		// Read of an absent state succeeds; Archive itself fails its finite
		// serialized bound, before publication or reduction/inference.
		app.Store.MaxBytes = 1
		app.Reduce = func(messages []ai.Message) ([]ai.Message, error) {
			return messages, os.WriteFile(filepath.Join(dir, "reduced"), nil, 0600)
		}
	}
	cfg := Config{App: app, Routes: routes(upstream), Identity: func(ctx context.Context, subject string) (ai.Identity, error) {
		if _, err := os.Stat(filepath.Join(dir, "deny-identity")); err == nil {
			return ai.Identity{}, brokerapp.ErrDenied
		}
		if subject != "personal" && subject != "work" {
			return ai.Identity{}, brokerapp.ErrDenied
		}
		return ai.Identity{Caller: subject, Tenant: subject, Conversation: "conversation"}, nil
	}}
	// The independently owned presentation hook corrupts child delivery bytes and
	// decorates final client usage. Original recording must remain authoritative.
	cfg.Host.Registrations = []extension.Registration{{Owner: "presentation", ID: "presentation.body", Stage: extension.ResponseBody, Body: extension.Unit, Handler: func(_ context.Context, in extension.Input) (extension.Result, error) {
		msg := in.Response
		if in.Identity.Depth > 0 {
			msg.Body = []byte("decorated, not provider JSON")
		} else {
			msg.Body = bytes.ReplaceAll(msg.Body, []byte("final answer"), []byte("presented answer"))
			msg.Body = bytes.ReplaceAll(msg.Body, []byte(`"completion_tokens":7`), []byte(`"completion_tokens":900`))
		}
		return extension.Result{Action: extension.Replace, Message: &msg}, nil
	}}, {Owner: "credential-audit", ID: "credential-audit.observe", Stage: extension.Observe, Handler: func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Request.Header.Get("Authorization") != "" || in.Requested.Header.Get("Authorization") != "" || in.Request.Header.Get("Cookie") != "" || in.Request.Header.Get("X-Account-ID") != "" {
			return extension.Result{}, errors.New("unrelated hook saw credentials")
		}
		return extension.Result{}, nil
	}}}
	return fixture{app, cfg, store, b, dir}
}
func serve(t *testing.T, f fixture) *httptest.Server {
	t.Helper()
	var err error
	f.cfg.Host.Catalog, err = net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	f.cfg.Host.Enrollment, err = net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	f.cfg.Host.ServiceToken = "synthetic-service"
	f.cfg.Host.RetryInterval = time.Hour
	h, err := New(f.broker, f.cfg)
	must(t, err)
	server := httptest.NewServer(h)
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		must(t, h.Close(ctx))
	})
	return server
}

type clientEvidence struct {
	Status int
	Header http.Header
	Body   []byte
}

func post(t *testing.T, url, tenant string, rev uint64, wire []byte) clientEvidence {
	t.Helper()
	r, err := http.NewRequest("POST", url+"/v1/chat/completions", bytes.NewReader(wire))
	must(t, err)
	r.Header.Set("Authorization", "Bearer token-"+tenant)
	r.Header.Set("Cookie", "private-ingress-cookie")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Recall-Revision", strconv.FormatUint(rev, 10))
	r.Header.Set("X-Tenant", "spoofed")
	r.Header.Set("X-Caller-ID", "work")
	r.Header.Set("X-Account-ID", "work-messages")
	resp, err := http.DefaultClient.Do(r)
	must(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	must(t, err)
	return clientEvidence{resp.StatusCode, resp.Header, body}
}
func wire(t *testing.T, messages []ai.Message) []byte {
	t.Helper()
	b, err := (ai.Adapter{Kind: ai.Chat}).EncodeRequest(ai.Request{Target: ai.Route{Model: "requested-client-model"}, Messages: messages, Settings: ai.Settings{MaxTokens: 100}, Tools: []ai.ToolDefinition{{Name: "client-shell", Schema: json.RawMessage(`{"type":"object"}`)}}})
	must(t, err)
	return b
}

// This process owns a new broker, engine, coordinator and servers on every run.
func TestProcessHelper(t *testing.T) {
	if os.Getenv("SYSTEM_CHILD") != "1" {
		t.Skip("process helper")
	}
	dir, upstream := os.Getenv("SYSTEM_DIR"), os.Getenv("SYSTEM_UPSTREAM")
	f := setup(t, dir, upstream, os.Getenv("SYSTEM_MODE"))
	server := serve(t, f)
	b, err := os.ReadFile(filepath.Join(dir, "request"))
	must(t, err)
	rev, err := strconv.ParseUint(os.Getenv("SYSTEM_REV"), 10, 64)
	must(t, err)
	result := post(t, server.URL, os.Getenv("SYSTEM_TENANT"), rev, b)
	must(t, writeJSON(filepath.Join(dir, "client"), result))
}
func process(t *testing.T, dir, upstream, tenant, mode string, rev uint64, b []byte) clientEvidence {
	t.Helper()
	must(t, os.WriteFile(filepath.Join(dir, "request"), b, 0600))
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "SYSTEM_CHILD=1", "SYSTEM_DIR="+dir, "SYSTEM_UPSTREAM="+upstream, "SYSTEM_TENANT="+tenant, "SYSTEM_MODE="+mode, "SYSTEM_REV="+strconv.FormatUint(rev, 10))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v %s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "client"))
	must(t, err)
	var result clientEvidence
	must(t, json.Unmarshal(raw, &result))
	return result
}

type upstreamRequest struct {
	Account, Path string
	Body          []byte
}
type provider struct {
	mu       sync.Mutex
	requests []upstreamRequest
	loser    map[string]chan struct{}
	canceled map[string]chan struct{}
	oversize bool
	stream   bool // fixture-only: complete Messages SSE split across socket writes
}

func newProvider() *provider {
	return &provider{loser: map[string]chan struct{}{"personal": make(chan struct{}), "work": make(chan struct{})}, canceled: map[string]chan struct{}{"personal": make(chan struct{}), "work": make(chan struct{})}}
}
func (p *provider) count() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.requests) }
func (p *provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	account := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer synthetic-")
	tenant := strings.SplitN(account, "-", 2)[0]
	path := strings.TrimPrefix(r.URL.Path, "/")
	p.mu.Lock()
	p.requests = append(p.requests, upstreamRequest{account, path, body})
	p.mu.Unlock()
	if account != tenant+"-"+path || (tenant != "personal" && tenant != "work") {
		http.Error(w, "wrong socket credential", 403)
		return
	}
	if path == "loser" {
		close(p.loser[tenant])
		<-r.Context().Done()
		close(p.canceled[tenant])
		return
	}
	if path == "winner" {
		select {
		case <-p.loser[tenant]:
		case <-r.Context().Done():
			return
		}
	}
	response := ai.Response{ID: "response-" + path, Model: "actual-" + path, Text: path, Stop: "end_turn"}
	kind := ai.Messages
	if path == "classify" {
		kind = ai.Chat
		response.Text = "messages"
		response.Stop = "stop"
	}
	if path == "messages" {
		response.Text = "final answer"
		input, output := int64(11), int64(7)
		response.Usage = ai.Usage{Input: &input, Output: &output}
		if !bytes.Contains(body, []byte("tool_result")) {
			response.Text = ""
			response.Stop = "tool_use"
			response.Calls = []ai.ToolCall{{ID: "recall-id", Name: "recall", Arguments: json.RawMessage(`{}`)}, {ID: "client-id", Name: "client-shell", Arguments: json.RawMessage(`{}`)}}
		}
		if p.oversize {
			response.Text = strings.Repeat("x", 3000)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Private", "provider-private")
	encoded, err := (ai.Adapter{Kind: kind}).Encode(response)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if path == "messages" && p.stream {
		encoded, err = (ai.Adapter{Kind: kind}).EncodeStream(response)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Deliberately split inside an event's JSON, not merely between events.
		cut := bytes.Index(encoded, []byte("message_start")) + len("message_")
		if cut <= len("message_") || cut >= len(encoded) {
			panic("missing fixture SSE event")
		}
		w.Write(encoded[:cut])
		w.(http.Flusher).Flush()
		w.Write(encoded[cut:])
		return
	}
	w.Write(encoded)
}

func TestCombinedTwoTenantProcessRestart(t *testing.T) {
	p := newProvider()
	upstream := httptest.NewServer(p)
	defer upstream.Close()
	dir := t.TempDir()
	for _, tenant := range []string{"personal", "work"} {
		first := process(t, dir, upstream.URL, tenant, "", 0, wire(t, []ai.Message{{Role: "user", Text: tenant + " original context"}}))
		if first.Status != 200 || first.Header.Get("X-Private") != "" {
			t.Fatalf("handoff: %+v %s", first, first.Body)
		}
		decoded, err := (ai.Adapter{Kind: ai.Chat}).Decode(first.Body)
		must(t, err)
		if len(decoded.Calls) != 1 || decoded.Calls[0].ID != "client-id" || bytes.Contains(first.Body, []byte("recall-id")) || bytes.Contains(first.Body, []byte("private recalled")) {
			t.Fatalf("bad public handoff: %s", first.Body)
		}
		st, err := (recall.Store{Dir: filepath.Join(dir, "recall")}).Read(context.Background(), recall.Key{Tenant: tenant, Conversation: "conversation"})
		must(t, err)
		if st.Budget == nil || st.Budget.Calls != 5 || st.Affinity.Bound.Account != tenant+"-messages" {
			t.Fatalf("checkpoint: %+v", st)
		}
		replay := append(st.Turn.Public, ai.Message{Role: "tool", ToolID: "client-id", Text: "client result"})
		second := process(t, dir, upstream.URL, tenant, "", st.Revision, wire(t, replay))
		if second.Status != 200 {
			t.Fatalf("restart: %d %s", second.Status, second.Body)
		}
		final, err := (ai.Adapter{Kind: ai.Chat}).Decode(second.Body)
		must(t, err)
		if final.Text != "presented answer" || final.Usage.Output == nil || *final.Usage.Output != 900 {
			t.Fatalf("delivery decoration: %s", second.Body)
		}
		completed, err := (recall.Store{Dir: filepath.Join(dir, "recall")}).Read(context.Background(), recall.Key{Tenant: tenant, Conversation: "conversation"})
		must(t, err)
		if completed.Budget.Calls != 6 || completed.Budget.Deadline != st.Budget.Deadline || completed.Affinity.Bound != st.Affinity.Bound {
			t.Fatal("continuation budget/deadline/affinity changed")
		}
	}
	effects, err := os.ReadFile(filepath.Join(dir, "effects"))
	must(t, err)
	if string(effects) != "personal:recall-id\nwork:recall-id\n" {
		t.Fatalf("duplicate/cross-tenant effect: %s", effects)
	}
	batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
	if len(batches) != 12 {
		t.Fatalf("attempt batches=%d", len(batches))
	}
	known, unknown, canceled := 0, 0, 0
	for _, batch := range batches {
		if len(batch) == 0 {
			// Canceled transport with no upstream headers has no original source.
			canceled++
			continue
		}
		if len(batch) != 1 {
			t.Fatalf("unexpected source ambiguity: %+v", batch)
		}
		a := batch[0]
		if a.Served.Account != a.Caller.Tenant+"-"+strings.TrimPrefix(strings.TrimPrefix(a.Request.URL, upstream.URL), "/") || a.Served.Endpoint != a.Request.URL || a.Request.Header.Get("Authorization") != "" {
			t.Fatalf("bad provenance %+v", a)
		}
		if strings.HasSuffix(a.Request.URL, "/messages") {
			known++
			if a.Usage.Output == nil || *a.Usage.Output != 7 || !a.Complete {
				t.Fatalf("original usage %+v", a)
			}
		} else if strings.HasSuffix(a.Request.URL, "/loser") {
			canceled++
			if a.Usage.Output != nil || a.Error == "" {
				t.Fatalf("canceled evidence %+v", a)
			}
		} else {
			unknown++
			if a.Usage.Input != nil || a.Usage.Output != nil {
				t.Fatalf("invented usage %+v", a)
			}
		}
	}
	if known != 4 || unknown != 6 || canceled != 2 {
		t.Fatalf("usage counts %d %d %d", known, unknown, canceled)
	}
	for _, tenant := range []string{"personal", "work"} {
		select {
		case <-p.canceled[tenant]:
		case <-time.After(time.Second):
			t.Fatal("candidate loser did not cancel")
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, req := range p.requests {
		if req.Path == "messages" {
			if !bytes.Contains(req.Body, []byte(`"max_tokens":100`)) || bytes.Contains(req.Body, []byte("requested-client-model")) {
				t.Fatalf("selected Messages encoding: %s", req.Body)
			}
			if bytes.Contains(req.Body, []byte("tool_result")) && (!bytes.Contains(req.Body, []byte("private recalled ")) || !bytes.Contains(req.Body, []byte("client result"))) {
				t.Fatalf("private replay lost: %s", req.Body)
			}
		}
	}
}
