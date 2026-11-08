package aihost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/proxy"
)

type clientEvidence struct {
	Status int
	Header http.Header
	Body   []byte
	Hints  int
}

func appendFile(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(append(b, '\n')); e != nil {
		return e
	}
	return f.Sync()
}
func testApp(dir, upstream string) *App {
	recorder := &ai.HostRecorder{Evidence: func(in extension.Input) (ai.Route, error) {
		protocol := ai.Messages
		if strings.HasSuffix(in.Request.URL, "/classify") {
			protocol = ai.Chat
		}
		if !strings.HasPrefix(in.Request.URL, upstream+"/") {
			return ai.Route{}, fmt.Errorf("untrusted served origin")
		}
		return ai.Route{Provider: protocol, Endpoint: in.Request.URL, Protocol: protocol, Account: in.Auth.Claims["tenant"] + "-account", Version: "fixture-v1"}, nil
	}, Sink: func(ctx context.Context, attempts []ai.Attempt) error {
		return appendFile(filepath.Join(dir, "attempts"), attempts)
	}}
	app := &App{Path: "/v1/chat/completions", Store: recall.Store{Dir: dir}, Target: ai.Route{Provider: "logical", Model: "requested", Endpoint: upstream + "/logical", Protocol: ai.Chat, Account: "logical", Version: "1"}, Recorder: recorder, Protocols: map[string]ai.Protocol{ai.Chat: ai.Adapter{Kind: ai.Chat}, ai.Messages: ai.Adapter{Kind: ai.Messages}}, Tools: []OwnedTool{{Definition: ai.ToolDefinition{Name: "owned", Schema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"],"additionalProperties":false}`)}, Validate: func(raw json.RawMessage) error {
		var args struct {
			Q string `json:"q"`
		}
		if err := strict(raw, &args); err != nil {
			return err
		}
		if args.Q != "safe" {
			return fmt.Errorf("invalid q")
		}
		return nil
	}, Run: func(ctx context.Context, id ai.Identity, raw json.RawMessage) (string, error) {
		err := appendFile(filepath.Join(dir, "effects"), map[string]string{"tenant": id.Tenant, "id": ai.ToolCallID(ctx)})
		return "owned archive result", err
	}}}}
	app.Select = func(ctx context.Context, s *ai.Session, req ai.Request) (ai.Request, error) {
		classifier := req
		classifier.Tools = nil
		classifier.Stream = false
		classifier.Target = ai.Route{Model: "classifier", Endpoint: upstream + "/classify", Protocol: ai.Chat}
		target := ai.Target{Route: ai.Route{Provider: ai.Messages, Model: "served", Endpoint: upstream + "/broker", Protocol: ai.Messages, Account: "selected", Version: "1"}, Tools: true, Stream: true, MaxTokens: 1000}
		selected, err := ai.Classify(ctx, s, classifier, map[string]ai.Target{"anthropic": target})
		if err != nil {
			return req, err
		}
		return ai.Prepare(req, selected, ai.Affinity{}, true)
	}
	return app
}
func gateway(t *testing.T, a *App, upstream string, extra ...extension.Registration) *httptest.Server {
	t.Helper()
	broker := extension.Registration{Owner: "broker", ID: "broker.resolve", Stage: extension.Resolve, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth == 0 {
			return extension.Result{}, nil
		}
		if in.Auth.Subject == "" || in.Auth.Claims["tenant"] == "" || !strings.HasPrefix(in.Request.URL, upstream+"/") {
			return extension.Result{}, fmt.Errorf("fresh authorization denied")
		}
		msg := in.Request
		if strings.HasSuffix(msg.URL, "/broker") {
			msg.URL = upstream + "/messages"
		}
		return extension.Result{Action: extension.Replace, Message: &msg, Credentials: http.Header{"Authorization": {"fixture " + in.Auth.Claims["tenant"]}}}, nil
	}}
	// A separate presentation owner edits delivered source chunks. The bridge
	// must decode/account originals, not these invalid/decorated upstream bytes.
	presentation := extension.Registration{Owner: "presentation", ID: "presentation.body", Stage: extension.ResponseBody, Body: extension.Unit, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
		if _, managed := ctx.Value(ingressKey{}).(ingress); !managed {
			return extension.Result{}, nil
		}
		msg := in.Response
		if in.Identity.Depth > 0 {
			msg.Body = []byte("not upstream JSON")
		} else {
			msg.Body = bytes.ReplaceAll(msg.Body, []byte("final answer"), []byte("presented answer"))
		}
		return extension.Result{Action: extension.Replace, Message: &msg}, nil
	}}
	regs := []extension.Registration{a.Registration(), broker, presentation}
	regs = append(regs, a.Recorder.Registrations()...)
	regs = append(regs, extra...)
	e, err := extension.New(extension.Config{}, regs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	p, err := proxy.NewWithEngine(upstream, e)
	if err != nil {
		t.Fatal(err)
	}
	inner := a.Handler(p)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := os.Getenv("AIHOST_TENANT")
		if tenant == "" {
			tenant = "personal"
		}
		r = r.WithContext(extension.WithAuthentication(r.Context(), extension.Authentication{Subject: "example-user", Claims: map[string]string{"tenant": tenant, "conversation": "conversation"}}))
		inner.ServeHTTP(w, r)
	}))
}

// Each child creates a new engine, Session and HTTP gateway. No injected fake
// Managed implementation is involved; parent upstream survives process restart.
func TestSocketProcessHelper(t *testing.T) {
	if os.Getenv("AIHOST_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	dir, upstream := os.Getenv("AIHOST_DIR"), os.Getenv("AIHOST_UPSTREAM")
	app := testApp(dir, upstream)
	if raw := os.Getenv("AIHOST_LIMIT_CALLS"); raw != "" {
		app.Limits.Calls, _ = strconv.Atoi(raw)
	}
	if os.Getenv("AIHOST_CRASH_AFTER_TOOL") == "1" {
		original := app.Tools[0].Run
		app.Tools[0].Run = func(ctx context.Context, id ai.Identity, raw json.RawMessage) (string, error) {
			_, err := original(ctx, id, raw)
			if err != nil {
				return "", err
			}
			os.Exit(0)
			return "", nil
		}
	}
	server := gateway(t, app, upstream)
	defer server.Close()
	wire, err := os.ReadFile(filepath.Join(dir, "request"))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", bytes.NewReader(wire))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Recall-Revision", os.Getenv("AIHOST_REV"))
	req.Header.Set("X-Tenant", "spoofed")
	var evidence clientEvidence
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{Got1xxResponse: func(int, textproto.MIMEHeader) error { evidence.Hints++; return nil }}))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Status = resp.StatusCode
	evidence.Header = resp.Header
	evidence.Body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := appendFile(filepath.Join(dir, "clients"), evidence); err != nil {
		t.Fatal(err)
	}
}
func runProcess(t *testing.T, dir, upstream, tenant string, rev uint64, wire []byte) clientEvidence {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "request"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSocketProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "AIHOST_CHILD=1", "AIHOST_DIR="+dir, "AIHOST_UPSTREAM="+upstream, "AIHOST_TENANT="+tenant, "AIHOST_REV="+strconv.FormatUint(rev, 10))
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("child: %v %s", e, b)
	}
	b, e := os.ReadFile(filepath.Join(dir, "clients"))
	if e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte{'\n'})
	var result clientEvidence
	if e = json.Unmarshal(lines[len(lines)-1], &result); e != nil {
		t.Fatal(e)
	}
	return result
}
func clientWire(t *testing.T, messages []ai.Message, stream bool) []byte {
	t.Helper()
	b, e := (ai.Adapter{Kind: ai.Chat}).EncodeRequest(ai.Request{Target: ai.Route{Model: "requested"}, Messages: messages, Stream: stream, Settings: ai.Settings{MaxTokens: 100}, Tools: []ai.ToolDefinition{{Name: "client-shell", Schema: json.RawMessage(`{"type":"object"}`)}}})
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestRealSocketMixedRestartJSONAndSSE(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var mu sync.Mutex
			var requests []struct {
				Tenant, Path string
				Body         []byte
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				tenant := strings.TrimPrefix(r.Header.Get("Authorization"), "fixture ")
				mu.Lock()
				requests = append(requests, struct {
					Tenant, Path string
					Body         []byte
				}{tenant, r.URL.Path, body})
				mu.Unlock()
				w.Header().Set("X-Private", "never deliver")
				w.Header().Set("Link", "private")
				w.WriteHeader(103)
				w.Header().Del("Link")
				var req map[string]json.RawMessage
				json.Unmarshal(body, &req)
				response := ai.Response{ID: "actual-id", Model: "served", Text: "final answer", Stop: "end_turn"}
				if r.URL.Path == "/classify" {
					w.Header().Set("Content-Type", "application/json")
					wire, _ := (ai.Adapter{Kind: ai.Chat}).Encode(ai.Response{ID: "classify", Model: "classifier", Text: "anthropic", Stop: "stop"})
					w.Write(wire)
					return
				}
				if !bytes.Contains(body, []byte("tool_result")) {
					response.Text = ""
					response.Stop = "tool_use"
					response.Calls = []ai.ToolCall{{ID: "owned-id", Name: "owned", Arguments: json.RawMessage(`{"q":"safe"}`)}, {ID: "client-id", Name: "client-shell", Arguments: json.RawMessage(`{}`)}}
					zero := int64(0)
					response.Usage.Output = &zero
				}
				var wire []byte
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					wire, _ = (ai.Adapter{Kind: ai.Messages}).EncodeStream(response)
				} else {
					w.Header().Set("Content-Type", "application/json")
					wire, _ = (ai.Adapter{Kind: ai.Messages}).Encode(response)
				}
				w.Write(wire)
			}))
			defer upstream.Close()
			dir := t.TempDir()
			for _, tenant := range []string{"personal", "work"} {
				first := runProcess(t, dir, upstream.URL, tenant, 0, clientWire(t, []ai.Message{{Role: "user", Text: "original context"}}, stream))
				if first.Status != 200 || first.Hints != 0 || first.Header.Get("X-Private") != "" || first.Header.Get("Link") != "" {
					t.Fatalf("handoff headers: %+v %s", first, first.Body)
				}
				decoded, e := (ai.Adapter{Kind: ai.Chat}).DecodeBuffered(first.Body, stream, ai.StreamLimits{})
				if e != nil || len(decoded.Calls) != 1 || decoded.Calls[0].ID != "client-id" {
					t.Fatalf("handoff %s %v", first.Body, e)
				}
				store := recall.Store{Dir: dir}
				key := recall.Key{Tenant: tenant, Conversation: "conversation"}
				st, e := store.Read(context.Background(), key)
				if e != nil {
					t.Fatal(e)
				}
				if st.Budget == nil || st.Budget.Calls != 2 || st.Budget.ToolCalls != 1 || st.InFlight {
					t.Fatalf("budget %+v", st)
				}
				logicalRoot := st.Turn.Lineage.Root
				replay := append(st.Turn.Public, ai.Message{Role: "tool", ToolID: "client-id", Text: "client result"})
				final := runProcess(t, dir, upstream.URL, tenant, st.Revision, clientWire(t, replay, stream))
				decoded, e = (ai.Adapter{Kind: ai.Chat}).DecodeBuffered(final.Body, stream, ai.StreamLimits{})
				if e != nil || decoded.Text != "presented answer" || final.Status != 200 || final.Hints != 0 {
					t.Fatalf("final %d %s %v", final.Status, final.Body, e)
				}
				st, e = store.Read(context.Background(), key)
				if e != nil || st.Budget.Calls != 3 || st.Budget.ToolCalls != 1 || st.Affinity.Bound.Endpoint != upstream.URL+"/messages" || st.Turn.Served.Model != "served" || st.Turn.Served.Account != tenant+"-account" || st.Turn.Served.Protocol != ai.Messages || st.Turn.Lineage.Root != logicalRoot {
					t.Fatalf("durable state %+v %v", st, e)
				}
			}
			effects, _ := os.ReadFile(filepath.Join(dir, "effects"))
			if len(bytes.Split(bytes.TrimSpace(effects), []byte{'\n'})) != 2 {
				t.Fatalf("duplicate effects %s", effects)
			}
			attempts, _ := os.ReadFile(filepath.Join(dir, "attempts"))
			var captured []ai.Attempt
			for _, line := range bytes.Split(bytes.TrimSpace(attempts), []byte{'\n'}) {
				var batch []ai.Attempt
				if e := json.Unmarshal(line, &batch); e != nil {
					t.Fatal(e)
				}
				captured = append(captured, batch...)
			}
			if len(captured) != 6 {
				t.Fatalf("attempts %d", len(captured))
			}
			if captured[1].Usage.Input != nil || captured[1].Usage.Output == nil || *captured[1].Usage.Output != 0 || captured[2].Usage.Output != nil {
				t.Fatalf("unknown/zero usage lost: %+v", captured)
			}
			for _, a := range captured {
				if bytes.Contains(a.Body, []byte("not upstream")) || bytes.Contains(a.Body, []byte("presented answer")) || a.Response.Status != 200 || a.Identity.Exchange == "" || !a.Complete || a.Error != "" || a.Served.Account != a.Caller.Tenant+"-account" {
					t.Fatalf("not original %+v", a)
				}
			}
			if !bytes.Contains(captured[2].Body, []byte("final answer")) || captured[2].Served.Model != "served" {
				t.Fatal("actual original output/model lost")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 6 {
				t.Fatalf("requests %d", len(requests))
			}
			for i, request := range requests {
				if request.Tenant == "" || request.Tenant == "spoofed" {
					t.Fatal("authentication missing")
				}
				if i%3 == 2 {
					for _, text := range []string{"owned-id", "owned archive result", "client-id", "client result", "tool_result"} {
						if !bytes.Contains(request.Body, []byte(text)) {
							t.Fatalf("actual continuation missing %q: %s", text, request.Body)
						}
					}
				}
				if request.Path == "/broker" || request.Path == "/logical" {
					t.Fatal("reroute not applied")
				}
			}
		})
	}
}
