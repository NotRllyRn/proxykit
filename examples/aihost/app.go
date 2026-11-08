// Package aihost is an optional single-host managed AI application example.
// It wraps an existing ProxyKit handler; it is not another HTTP proxy.
package aihost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

type OwnedTool struct {
	Definition ai.ToolDefinition
	Validate   func(json.RawMessage) error // required, strict schema/identity rejection
	Run        ai.Tool
}
type App struct {
	Path            string // exact declared coordination path; other paths remain live
	MaxRequestBytes int64
	Store           recall.Store
	Limits          ai.Limits
	Lifetime        time.Duration // total continuation lifetime, persisted across restart
	Target          ai.Route
	Protocols       map[string]ai.Protocol
	Recorder        *ai.HostRecorder
	Tools           []OwnedTool
	// Select may classify/reroute/candidate using ONLY the supplied scoped Session.
	// Trusted deployment code; never a caller-supplied destination/commit bool.
	Select func(context.Context, *ai.Session, ai.Request) (ai.Request, error)
	// Reduce is optional; originals are durably archived before it is called.
	Reduce func([]ai.Message) ([]ai.Message, error)
}
type ingress struct {
	req      ai.Request
	revision uint64
}
type ingressKey struct{}

// Handler must run AFTER trusted authentication middleware supplies tenant,
// conversation and subject via extension.WithAuthentication. No identity headers
// or caller-supplied Turn are interpreted. Revision is an untrusted CAS token.
func (a *App) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Path == "" || r.URL.Path != a.Path {
			next.ServeHTTP(w, r)
			return
		}
		auth, ok := extension.AuthenticationFromContext(r.Context())
		if !ok || auth.Subject == "" || auth.Claims["tenant"] == "" || auth.Claims["conversation"] == "" {
			http.Error(w, "trusted identity required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "unsupported managed request", http.StatusUnsupportedMediaType)
			return
		}
		max := a.MaxRequestBytes
		if max <= 0 {
			max = 1 << 20
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, max+1))
		r.Body.Close()
		if err != nil || int64(len(b)) > max {
			http.Error(w, "bounded request rejected", http.StatusRequestEntityTooLarge)
			return
		}
		req, err := decodeRequest(b)
		if err != nil {
			http.Error(w, "unsupported managed semantics: "+err.Error(), http.StatusBadRequest)
			return
		}
		rev := uint64(0)
		if raw := r.Header.Get("X-Recall-Revision"); raw != "" {
			rev, err = strconv.ParseUint(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid revision", 400)
				return
			}
		}
		req.Identity = ai.Identity{Tenant: auth.Claims["tenant"], Conversation: auth.Claims["conversation"], Caller: auth.Subject}
		req.Target = a.Target
		req.Requested.Provider = "client" // desired route is distinct from actual Served evidence
		req.Requested.Endpoint = r.URL.Path
		req.Requested.Version = "chat-subset-v1"
		r = r.WithContext(context.WithValue(r.Context(), ingressKey{}, ingress{req, rev}))
		r.Body = http.NoBody
		r.ContentLength = 0
		r.Header.Del("Content-Length")
		// All informational headers are discarded, even if a managed child inherits
		// ReverseProxy's httptrace callback. Only a final selected local response can
		// commit. Ordinary passthrough never uses this gate or buffers its body.
		next.ServeHTTP(&selectionWriter{ResponseWriter: w, header: make(http.Header)}, r)
	})
}

type selectionWriter struct {
	http.ResponseWriter
	header    http.Header
	committed bool
}

func (w *selectionWriter) Header() http.Header { return w.header }
func (w *selectionWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		clear(w.header)
		return
	}
	if w.committed {
		return
	}
	w.committed = true
	for k, v := range w.header {
		w.ResponseWriter.Header()[k] = append([]string(nil), v...)
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *selectionWriter) Write(b []byte) (int, error) {
	if !w.committed {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}

// Registration is an owned root handler. It never parses a whole body in host
// hooks: only Handler's explicitly declared coordination path is buffered.
func (a *App) Registration() extension.Registration {
	return extension.Registration{Owner: "ai-app", ID: "ai-app.coordinate", Stage: extension.Prepare, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
		input, ok := ctx.Value(ingressKey{}).(ingress)
		if !ok || in.Identity.Depth != 0 {
			return extension.Result{}, nil
		}
		req := input.req
		if in.Auth.Subject != req.Identity.Caller || in.Auth.Claims["tenant"] != req.Identity.Tenant || in.Auth.Claims["conversation"] != req.Identity.Conversation {
			return extension.Result{}, errors.New("managed identity mismatch")
		}
		session := &ai.Session{Managed: ai.HostManaged{CallChild: in.Call, Recorder: a.Recorder, Protocols: a.Protocols}, Limits: a.Limits}
		c := ai.Coordinator{Session: session, Tools: map[string]ai.Tool{}}
		definitions := map[string]bool{}
		for _, t := range req.Tools {
			definitions[t.Name] = true
		}
		validators := map[string]func(json.RawMessage) error{}
		for _, t := range a.Tools {
			if t.Run == nil || t.Validate == nil || t.Definition.Name == "" || definitions[t.Definition.Name] {
				return extension.Result{}, errors.New("invalid/colliding owned tool declaration")
			}
			definitions[t.Definition.Name] = true
			validators[t.Definition.Name] = t.Validate
			c.Tools[t.Definition.Name] = t.Run
			req.Tools = append(req.Tools, t.Definition)
		}
		c.Validate = func(call ai.ToolCall) error {
			if !definitions[call.Name] {
				return errors.New("undeclared selected tool")
			}
			var args map[string]json.RawMessage
			if json.Unmarshal(call.Arguments, &args) != nil || args == nil {
				return errors.New("tool arguments must be object")
			}
			if validate := validators[call.Name]; validate != nil {
				return validate(call.Arguments)
			}
			return nil
		}
		key := recall.Key{Tenant: req.Identity.Tenant, Conversation: req.Identity.Conversation}
		st, err := a.Store.Read(ctx, key)
		if err != nil {
			return extension.Result{}, err
		}
		if st.Revision != input.revision {
			return extension.Result{}, recall.ErrConflict
		}
		if st.InFlight {
			return extension.Result{}, recall.ErrUncertain
		}
		if st.Turn != nil && st.Budget == nil {
			return extension.Result{}, errors.New("missing authoritative continuation budget")
		}
		if st.Turn == nil {
			st, err = a.Store.Archive(ctx, key, st.Revision, "ai-app", req.Messages, ai.Affinity{Bound: req.Target})
			if err == nil && a.Reduce != nil {
				st, err = a.Store.Reduce(ctx, key, st.Revision, "ai-app", a.Reduce)
			}
			if err != nil {
				return extension.Result{}, err
			}
		}
		st, err = a.Store.Run(ctx, key, st.Revision, func(saved recall.State) (ai.Turn, ai.Budget, error) {
			deadline := time.Now().Add(a.Lifetime)
			if a.Lifetime <= 0 {
				deadline = time.Now().Add(5 * time.Minute)
			}
			if saved.Budget != nil {
				if err := session.RestoreBudget(*saved.Budget); err != nil {
					return ai.Turn{}, ai.Budget{}, err
				}
				deadline = saved.Budget.Deadline
			}
			if deadline.IsZero() || !time.Now().Before(deadline) {
				return ai.Turn{}, ai.Budget{}, ai.ErrLimit
			}
			scoped, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			replay := req.Messages
			if saved.Turn != nil {
				// The selected destination remains pinned across handoff. Migration of
				// signed/provider-held state requires a different explicitly supported app.
				req.Target = saved.Affinity.Bound
			} else {
				req.Messages = saved.Working
			}
			if saved.Turn == nil && a.Select != nil {
				var e error
				req, e = a.Select(scoped, session, req)
				if e != nil {
					return ai.Turn{}, ai.Budget{}, e
				}
			}
			var turn ai.Turn
			var e error
			if saved.Turn == nil {
				prefix := len(req.Messages)
				turn, e = c.Start(scoped, req)
				if e == nil {
					// Reduction changes private upstream context, never the client's
					// replay prefix. Preserve the archived public original verbatim.
					turn.Public = append(append([]ai.Message(nil), saved.Original...), turn.Public[prefix:]...)
				}
			} else {
				turn, e = c.Resume(scoped, req, *saved.Turn, replay)
			}
			if e != nil {
				return turn, ai.Budget{}, e
			}
			budget, e := session.Budget()
			budget.Deadline = deadline
			return turn, budget, e
		})
		if err != nil {
			return extension.Result{}, err
		}
		p := a.Protocols[ai.Chat]
		if p == nil {
			return extension.Result{}, ai.ErrUnsupported
		}
		var wire []byte
		typ := "application/json"
		if req.Stream {
			sp, ok := p.(ai.StreamingProtocol)
			if !ok {
				return extension.Result{}, ai.ErrUnsupported
			}
			wire, err = sp.EncodeStream(st.Turn.Response)
			typ = "text/event-stream"
		} else {
			wire, err = p.Encode(st.Turn.Response)
		}
		if err != nil {
			return extension.Result{}, err
		}
		return extension.Result{Action: extension.Respond, Message: &extension.Message{Status: 200, Header: http.Header{"Content-Type": {typ}, "X-Recall-Revision": {fmt.Sprint(st.Revision)}}, Body: wire}}, nil
	}}
}
