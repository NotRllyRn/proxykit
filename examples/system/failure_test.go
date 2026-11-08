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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/brokerapp"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

func state(t *testing.T, dir string) recall.State {
	t.Helper()
	s, err := (recall.Store{Dir: filepath.Join(dir, "recall")}).Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	must(t, err)
	return s
}
func replay(t *testing.T, st recall.State) []byte {
	t.Helper()
	return wire(t, append(st.Turn.Public, ai.Message{Role: "tool", ToolID: "client-id", Text: "client result"}))
}
func expectDenied(t *testing.T, result clientEvidence) {
	t.Helper()
	if result.Status < 400 || bytes.Contains(result.Body, []byte("tool_calls")) || bytes.Contains(result.Body, []byte("final answer")) {
		t.Fatalf("unsafe delivery %d %s", result.Status, result.Body)
	}
}

func TestCombinedRecordingFailuresAfterClassificationAndOnResume(t *testing.T) {
	for _, resumed := range []bool{false, true} {
		for _, mode := range []string{"sink", "capture"} {
			t.Run(mode+"/resumed="+map[bool]string{true: "yes", false: "no"}[resumed], func(t *testing.T) {
				p := newProvider()
				upstream := httptest.NewServer(p)
				defer upstream.Close()
				dir := t.TempDir()
				request := wire(t, []ai.Message{{Role: "user", Text: "original"}})
				rev := uint64(0)
				prior := []byte(nil)
				if resumed {
					first := process(t, dir, upstream.URL, "personal", "", 0, request)
					if first.Status != 200 {
						t.Fatalf("handoff %s", first.Body)
					}
					st := state(t, dir)
					rev = st.Revision
					request = replay(t, st)
					prior, _ = os.ReadFile(filepath.Join(dir, "effects"))
				}
				// This is set before this process starts and before any concurrent socket.
				p.oversize = mode == "capture"
				result := process(t, dir, upstream.URL, "personal", mode, rev, request)
				expectDenied(t, result)
				effects, _ := os.ReadFile(filepath.Join(dir, "effects"))
				if !bytes.Equal(effects, prior) {
					t.Fatalf("failed source executed effect: %s", effects)
				}
				st := state(t, dir)
				if !st.InFlight || (resumed && st.Turn == nil) || (!resumed && st.Turn != nil) {
					t.Fatalf("missing uncertain quarantine %+v", st)
				}
				before := p.count()
				expectDenied(t, process(t, dir, upstream.URL, "personal", mode, st.Revision, request))
				if p.count() != before {
					t.Fatal("quarantine admitted replay")
				}
				batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
				want := 4
				if resumed {
					want = 5
				}
				if mode == "capture" {
					want++
				}
				if len(batches) != want {
					t.Fatalf("recording stages: %d want %d", len(batches), want)
				}
				if len(batches[0]) != 1 || batches[0][0].Served.Model != "actual-classify" {
					t.Fatal("did not classify/record before selected failure")
				}
				if mode == "capture" {
					last := batches[len(batches)-1]
					if len(last) != 1 || last[0].Complete || last[0].Error == "" || len(last[0].Body) > 1600 {
						t.Fatalf("bounded failed evidence %+v", last)
					}
				}
			})
		}
	}
}

func TestCombinedArchiveFailureStopsInference(t *testing.T) {
	p := newProvider()
	upstream := httptest.NewServer(p)
	defer upstream.Close()
	dir := t.TempDir()
	expectDenied(t, process(t, dir, upstream.URL, "personal", "archive", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	if p.count() != 0 || len(lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))) != 0 {
		t.Fatal("inference preceded durable archive")
	}
	if _, err := os.Stat(filepath.Join(dir, "reduced")); !os.IsNotExist(err) {
		t.Fatal("reduced after archive failure")
	}
	if st := state(t, dir); st.Revision != 0 || len(st.Archive) != 0 {
		t.Fatal("failed archive published")
	}
}

func TestCombinedReauthorizationAndDurableBudgetOnFreshProcess(t *testing.T) {
	for _, mode := range []string{"identity", "catalog", "endpoint", "budget", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			p := newProvider()
			upstream := httptest.NewServer(p)
			defer upstream.Close()
			dir := t.TempDir()
			profile := ""
			if mode == "budget" {
				profile = mode
			}
			if mode == "deadline" {
				profile = mode
			}
			first := process(t, dir, upstream.URL, "personal", profile, 0, wire(t, []ai.Message{{Role: "user", Text: "original"}}))
			if first.Status != 200 {
				t.Fatalf("handoff %d %s", first.Status, first.Body)
			}
			st := state(t, dir)
			request := replay(t, st)
			before := p.count()
			prior, _ := os.ReadFile(filepath.Join(dir, "effects"))
			switch mode {
			case "identity":
				must(t, os.WriteFile(filepath.Join(dir, "deny-identity"), nil, 0600))
			case "catalog", "endpoint":
				store, err := brokerapp.OpenStore(filepath.Join(dir, "broker"))
				must(t, err)
				r := route(upstream.URL, "personal", "messages")
				active := false
				endpoint := r.Endpoint
				if mode == "endpoint" {
					active = true
					endpoint = upstream.URL + "/probe"
				}
				must(t, store.Activate(brokerapp.Account{ID: r.Account, Label: "Personal usage", Callers: []string{"personal"}, Endpoint: endpoint, Provider: r.Provider, Model: r.Model, Active: active, Credential: brokerapp.Credential{Token: "synthetic-" + r.Account, Expires: time.Now().Add(time.Hour)}}))
			case "deadline":
				time.Sleep(time.Until(st.Budget.Deadline) + 10*time.Millisecond)
			}
			expectDenied(t, process(t, dir, upstream.URL, "personal", profile, st.Revision, request))
			if p.count() != before {
				t.Fatal("revoked/exhausted continuation reached upstream")
			}
			effects, _ := os.ReadFile(filepath.Join(dir, "effects"))
			if !bytes.Equal(effects, prior) {
				t.Fatal("effect replayed")
			}
			current := state(t, dir)
			expectDenied(t, process(t, dir, upstream.URL, "personal", profile, current.Revision, request))
			if p.count() != before {
				t.Fatal("revoked replay admitted")
			}
			if mode == "identity" && current.Revision != st.Revision {
				t.Fatal("revoked identity accessed/mutated continuation")
			}
		})
	}
}

func TestCombinedOrderingAndUnsupportedPersistedState(t *testing.T) {
	p := newProvider()
	upstream := httptest.NewServer(p)
	defer upstream.Close()
	for _, stage := range []extension.Stage{extension.Prepare, extension.Resolve, extension.Upstream} {
		f := setup(t, t.TempDir(), upstream.URL, "")
		f.cfg.Host.Registrations = append(f.cfg.Host.Registrations, extension.Registration{Owner: "policy", ID: "policy", Stage: stage, Handler: func(context.Context, extension.Input) (extension.Result, error) { return extension.Result{}, nil }})
		if _, err := New(f.broker, f.cfg); err == nil {
			t.Fatalf("unprotected root ordering accepted stage %d", stage)
		}
		must(t, f.broker.Close(context.Background()))
	}
	f := setup(t, t.TempDir(), upstream.URL, "")
	server := serve(t, f)
	// Provider-held references cannot be dropped/reinterpreted under another
	// protocol/account, including on a root that otherwise has a valid identity.
	request := []byte(`{"model":"requested","messages":[{"role":"user","content":"original"}],"max_tokens":100,"previous_response_id":"provider-held"}`)
	expectDenied(t, post(t, server.URL, "personal", 0, request))
	if p.count() != 0 {
		t.Fatal("unsupported provider state reached inference")
	}
	// Validate supported policy placement by installing an actual blocking policy.
	g := setup(t, t.TempDir(), upstream.URL, "")
	var visited atomic.Int32
	g.cfg.Host.Registrations = append(g.cfg.Host.Registrations, extension.Registration{Owner: "policy", ID: "policy", Stage: extension.Prepare, Before: []string{"ai-app.coordinate"}, Handler: func(context.Context, extension.Input) (extension.Result, error) {
		visited.Add(1)
		return extension.Result{}, errors.New("blocked")
	}})
	blocked := serve(t, g)
	expectDenied(t, post(t, blocked.URL, "personal", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	if visited.Load() != 1 || p.count() != 0 {
		t.Fatal("blocking policy bypassed")
	}
}

func TestCombinedNestedOriginalsStillReject(t *testing.T) {
	p := newProvider()
	upstream := httptest.NewServer(p)
	defer upstream.Close()
	dir := t.TempDir()
	f := setup(t, dir, upstream.URL, "")
	var nested atomic.Bool
	f.cfg.Host.Registrations = append(f.cfg.Host.Registrations, extension.Registration{Owner: "nested", ID: "nested", Stage: extension.ResponseStart, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
		if !strings.HasSuffix(in.Request.URL, "/messages") || !nested.CompareAndSwap(false, true) {
			return extension.Result{}, nil
		}
		// Same authorized target, but an independent original source. There is no
		// authoritative selected-source identity in the core snapshot contract.
		b, _ := (ai.Adapter{Kind: ai.Messages}).EncodeRequest(ai.Request{Target: route(upstream.URL, "personal", "messages"), Messages: []ai.Message{{Role: "user", Text: "nested"}}, Settings: ai.Settings{MaxTokens: 100}})
		r, _ := http.NewRequestWithContext(ctx, "POST", in.Request.URL, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		resp, err := in.Call(ctx, r)
		if err != nil {
			return extension.Result{}, err
		}
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, resp.Body)
		return extension.Result{}, err
	}})
	server := serve(t, f)
	expectDenied(t, post(t, server.URL, "personal", 0, wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	batches := lines[[]ai.Attempt](t, filepath.Join(dir, "attempts"))
	last := batches[len(batches)-1]
	if len(last) != 2 || last[0].Identity.Exchange == last[1].Identity.Exchange || last[0].Logical.Call != last[1].Logical.Call {
		t.Fatalf("multiple original evidence %+v", last)
	}
	if _, err := os.Stat(filepath.Join(dir, "effects")); !os.IsNotExist(err) {
		t.Fatal("ambiguous source ran tool")
	}
	if !state(t, dir).InFlight {
		t.Fatal("ambiguous source not quarantined")
	}
}

func TestCombinedIngressAuthenticationCannotBeSpoofed(t *testing.T) {
	p := newProvider()
	upstream := httptest.NewServer(p)
	defer upstream.Close()
	f := setup(t, t.TempDir(), upstream.URL, "")
	server := serve(t, f)
	r, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", bytes.NewReader(wire(t, []ai.Message{{Role: "user", Text: "original"}})))
	r.Header.Set("X-Caller-ID", "personal")
	r.Header.Set("X-Tenant", "personal")
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	must(t, err)
	resp.Body.Close()
	if resp.StatusCode != 401 || p.count() != 0 {
		t.Fatal("public headers became identity")
	}
}
