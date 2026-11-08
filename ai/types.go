// Package ai contains optional, buffered AI application primitives. It does not
// perform HTTP dispatch or own final client delivery.
package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrUnsupported = errors.New("unsupported semantics")
var ErrLimit = errors.New("AI limit exceeded")

// Identity must be supplied by the authenticated host, never model arguments.
type Identity struct{ Tenant, Conversation, Caller string }
type Route struct{ Provider, Model, Account, Endpoint, Protocol, Version string }
type Settings struct {
	MaxTokens   int
	Temperature *float64
}
type ToolCall struct {
	ID, Name  string
	Arguments json.RawMessage
}
type Message struct {
	Role, Text, ToolID string
	Calls              []ToolCall
}
type Usage struct{ Input, Output *int64 } // nil means unknown, including on failed calls.
type Response struct {
	ID, Model, Text, Stop string
	Calls                 []ToolCall
	Usage                 Usage
	// Extra retains root fields in the source protocol. Opaque retains the full
	// original response when its content cannot safely be transformed.
	Source   string
	Original json.RawMessage `json:",omitempty"`
	Extra    map[string]json.RawMessage
	Opaque   json.RawMessage `json:",omitempty"`
	// Events retains buffered wire evidence, including on semantic decode errors.
	Events []Event `json:",omitempty"`
}

// Lineage identifies a logical call. Host retries/attempts retain separate IDs.
type Lineage struct{ Root, Parent, Call string }
type Request struct {
	Lineage           Lineage
	Requested, Target Route
	Identity          Identity
	Messages          []Message
	Settings          Settings
	Tools             []ToolDefinition
	Stream            bool
	Purpose           string
}
type ToolDefinition struct {
	Name, Description string
	Schema            json.RawMessage
}
type Result struct {
	Response Response
	Served   Route
}

// Managed is the ONLY inference boundary used here (including classifiers,
// candidates and continuations). The bridge must run host authentication,
// hooks, root budgets, lineage, original observations and response processing.
// Return only complete bounded responses; decode SSE before returning. It must
// obey ctx and reject late results. No credentials are stored in these types.
type Managed interface {
	Call(context.Context, Request) (Result, error)
}
type ManagedFunc func(context.Context, Request) (Result, error)

func (f ManagedFunc) Call(c context.Context, r Request) (Result, error) { return f(c, r) }

// Observation is a detached, original per-call snapshot, not final delivery.
type Observation struct {
	Request Request
	Result  Result
	Error   string
}
type Observer func(context.Context, Observation) error

// Session shares admission across routing and tool continuations. Negative
// limits explicitly disable that bound; zero selects conservative defaults.
type Limits struct {
	Calls, ToolCalls, Rounds, Parallel, OutputBytes int
	Timeout                                         time.Duration
}

func (l Limits) defaults() Limits {
	if l.Calls == 0 {
		l.Calls = 16
	}
	if l.ToolCalls == 0 {
		l.ToolCalls = 32
	}
	if l.Rounds == 0 {
		l.Rounds = 8
	}
	if l.Parallel == 0 {
		l.Parallel = 4
	}
	if l.OutputBytes == 0 {
		l.OutputBytes = 1 << 20
	}
	if l.Timeout == 0 {
		l.Timeout = 30 * time.Second
	}
	return l
}

type Session struct {
	Managed                          Managed
	Observe                          Observer
	Limits                           Limits
	mu                               sync.Mutex
	calls, toolCalls, active, output int
	root                             string
}

func (s *Session) Call(ctx context.Context, req Request) (Result, error) {
	if _, err := json.Marshal(req); err != nil {
		return Result{}, err
	}
	l := s.Limits.defaults()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Result{}, err
	}
	callID := hex.EncodeToString(id[:])
	s.mu.Lock()
	if (l.Calls > 0 && s.calls >= l.Calls) || (l.Parallel > 0 && s.active >= l.Parallel) {
		s.mu.Unlock()
		return Result{}, ErrLimit
	}
	if s.root == "" {
		s.root = req.Lineage.Root
		if s.root == "" {
			s.root = callID
		}
	}
	if req.Lineage.Root != "" && req.Lineage.Root != s.root {
		s.mu.Unlock()
		return Result{}, errors.New("session root lineage mismatch")
	}
	s.calls++
	s.active++
	if req.Lineage.Root == "" {
		req.Lineage.Root = s.root
	}
	if req.Lineage.Parent == "" {
		req.Lineage.Parent = req.Lineage.Call
	}
	req.Lineage.Call = callID
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active--; s.mu.Unlock() }()
	if s.Managed == nil {
		return Result{}, errors.New("missing managed inference bridge")
	}
	if l.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.Timeout)
		defer cancel()
	}
	// JSON snapshots prevent cooperating bridges/observers from changing callers.
	req = clone(req)
	result, err := s.Managed.Call(ctx, clone(req))
	if _, e := json.Marshal(result); e != nil {
		// Invalid RawMessage cannot be safely snapshotted. Host wire capture remains
		// authoritative; retain route/usage and publish a failed logical outcome.
		err = errors.Join(err, e)
		result = Result{Served: result.Served, Response: Response{Usage: result.Response.Usage}}
	}
	result = clone(result)
	if e := validateResponse(result.Response); e != nil {
		err = errors.Join(err, e)
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	obs := Observation{Request: req, Result: clone(result)}
	if err != nil {
		obs.Error = err.Error()
	}
	if s.Observe != nil {
		if e := s.Observe(ctx, obs); e != nil {
			return Result{}, fmt.Errorf("required observation: %w", e)
		}
	}
	b, e := json.Marshal(result.Response)
	if e != nil {
		return Result{}, e
	}
	s.mu.Lock()
	s.output += len(b)
	over := l.OutputBytes > 0 && s.output > l.OutputBytes
	s.mu.Unlock()
	if over {
		return Result{}, ErrLimit
	}
	// Observation describes the original outcome, not this later deadline.
	if e := ctx.Err(); e != nil {
		return Result{}, e
	}
	return result, err
}
func (s *Session) rootID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.root == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return "", err
		}
		s.root = hex.EncodeToString(id[:])
	}
	return s.root, nil
}
func clone[T any](v T) T {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	var out T
	if e = json.Unmarshal(b, &out); e != nil {
		panic(e)
	}
	return out
}
func ValidateMessages(ms []Message) error {
	pending := map[string]bool{}
	seen := map[string]bool{}
	for _, m := range ms {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" && m.Role != "tool" {
			return errors.New("unsupported message role")
		}
		if len(m.Calls) > 0 && m.Role != "assistant" || m.ToolID != "" && m.Role != "tool" {
			return errors.New("invalid message tool fields")
		}
		if m.Role == "tool" {
			if !pending[m.ToolID] {
				return errors.New("unmatched tool result")
			}
			delete(pending, m.ToolID)
			continue
		}
		if len(pending) > 0 {
			return errors.New("missing tool results before next message")
		}
		for _, c := range m.Calls {
			if c.ID == "" || c.Name == "" || seen[c.ID] || !json.Valid(c.Arguments) {
				return errors.New("invalid or duplicate tool call ID/arguments")
			}
			seen[c.ID] = true
			pending[c.ID] = true
		}
	}
	if len(pending) > 0 {
		return errors.New("unresolved tool calls")
	}
	return nil
}
