package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

// Tool receives host identity separately from model arguments. ToolCallID(ctx)
// supplies the stable call ID for application-owned idempotency keys.
type Tool func(context.Context, Identity, json.RawMessage) (string, error)
type toolCallKey struct{}

func ToolCallID(ctx context.Context) string { id, _ := ctx.Value(toolCallKey{}).(string); return id }

type Coordinator struct {
	Session *Session
	Tools   map[string]Tool
	// Validate checks every selected call before ANY tool effect. Applications
	// use this for declared-tool membership and strict owned argument schemas.
	Validate func(ToolCall) error
}

// Turn is private durable state. Public is exactly the client-visible transcript;
// Working includes hidden continuations. Persist it with a revision before
// accepting replay. Never accept a Turn supplied by an unauthenticated client.
type Turn struct {
	Lineage         Lineage
	Identity        Identity
	Served          Route // authoritative last managed destination, persisted at handoff
	Public, Working []Message
	Pending         []ToolCall
	Results         []Message
	Response        Response
	Rounds          int
}

func (c Coordinator) Start(ctx context.Context, req Request) (Turn, error) {
	if c.Session == nil {
		return Turn{}, errors.New("missing session")
	}
	if _, err := json.Marshal(req); err != nil {
		return Turn{}, err
	}
	if err := ValidateMessages(req.Messages); err != nil {
		return Turn{}, err
	}
	if req.Lineage.Root == "" {
		var err error
		req.Lineage.Root, err = c.Session.rootID()
		if err != nil {
			return Turn{}, err
		}
	}
	return c.run(ctx, clone(req), Turn{Lineage: req.Lineage, Identity: req.Identity, Public: clone(req.Messages), Working: clone(req.Messages)})
}

// Resume accepts an exact replay prefix plus one result for each client call.
// Private results are merged by ID in original call order, never by tool name.
func (c Coordinator) Resume(ctx context.Context, req Request, t Turn, replay []Message) (Turn, error) {
	if c.Session == nil || t.Identity != req.Identity || len(t.Pending) == 0 {
		return Turn{}, errors.New("invalid continuation identity/state")
	}
	if _, err := json.Marshal(struct {
		Req    Request
		Turn   Turn
		Replay []Message
	}{req, t, replay}); err != nil {
		return Turn{}, err
	}
	if req.Lineage.Root != "" && req.Lineage.Root != t.Lineage.Root {
		return Turn{}, errors.New("continuation root lineage mismatch")
	}
	req.Lineage = t.Lineage
	t = clone(t)
	replay = clone(replay)
	if len(replay) != len(t.Public)+len(t.Pending) || !reflect.DeepEqual(replay[:len(t.Public)], t.Public) {
		return Turn{}, errors.New("replay prefix conflict")
	}
	results := map[string]Message{}
	for _, m := range t.Results {
		results[m.ToolID] = m
	}
	allowed := map[string]bool{}
	for _, p := range t.Pending {
		allowed[p.ID] = true
	}
	for _, m := range replay[len(t.Public):] {
		if m.Role != "tool" || !allowed[m.ToolID] {
			return Turn{}, errors.New("unexpected/duplicate replay result")
		}
		delete(allowed, m.ToolID)
		results[m.ToolID] = m
	}
	if len(allowed) > 0 {
		return Turn{}, errors.New("missing client result")
	}
	for _, call := range t.Working[len(t.Working)-1].Calls {
		m, ok := results[call.ID]
		if !ok {
			return Turn{}, errors.New("missing private result")
		}
		t.Working = append(t.Working, m)
	}
	t.Public = replay
	t.Pending = nil
	t.Results = nil
	req.Messages = t.Working
	if err := ValidateMessages(req.Messages); err != nil {
		return Turn{}, err
	}
	return c.run(ctx, req, t)
}
func (c Coordinator) run(ctx context.Context, req Request, t Turn) (Turn, error) {
	l := c.Session.Limits.defaults()
	if l.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.Timeout)
		defer cancel()
	}
	for {
		if ctx.Err() != nil {
			return t, ctx.Err()
		}
		if l.Rounds > 0 && t.Rounds >= l.Rounds {
			return t, ErrLimit
		}
		t.Rounds++
		req.Messages = clone(t.Working)
		req.Purpose = "tool-continuation"
		result, err := c.Session.Call(ctx, req)
		if err != nil {
			return t, err
		}
		t.Served = result.Served
		r := result.Response
		if err := Portable(r); err != nil {
			return t, err
		}
		if len(r.Calls) == 0 {
			t.Response = r
			t.Public = append(t.Public, Message{Role: "assistant", Text: r.Text})
			t.Working = append(t.Working, Message{Role: "assistant", Text: r.Text})
			return t, nil
		}
		// Validate IDs against all previous private turns before any side effect.
		next := append(clone(t.Working), Message{Role: "assistant", Text: r.Text, Calls: clone(r.Calls)})
		if err := ValidateMessages(append(clone(next), toolPlaceholders(r.Calls)...)); err != nil {
			return t, err
		}
		if c.Validate != nil {
			for _, call := range r.Calls {
				if err := c.Validate(clone(call)); err != nil {
					return t, err
				}
			}
		}
		t.Working = next
		results, err := c.execute(ctx, req.Identity, r.Calls, l)
		if err != nil {
			return t, err
		}
		var pending []ToolCall
		for _, call := range r.Calls {
			if c.Tools[call.Name] == nil {
				pending = append(pending, call)
			}
		}
		if len(pending) > 0 {
			// Internal calls/results remain private; only unresolved calls are handed off.
			r.Calls = clone(pending)
			t.Response = r
			t.Pending = pending
			t.Results = results
			t.Public = append(t.Public, Message{Role: "assistant", Text: r.Text, Calls: clone(pending)})
			return t, nil
		}
		t.Working = append(t.Working, results...)
	}
}
func (c Coordinator) execute(ctx context.Context, id Identity, calls []ToolCall, l Limits) ([]Message, error) {
	var owned []ToolCall
	for _, call := range calls {
		if c.Tools[call.Name] != nil {
			owned = append(owned, call)
		}
	}
	n := l.Parallel
	if n < 0 || n > len(owned) {
		n = len(owned)
	}
	if n == 0 {
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make([]Message, len(owned))
	errs := make([]error, len(owned))
	var wg sync.WaitGroup
	// Fixed workers, no goroutine per queued call. Admission stops on cancellation.
	jobs := make(chan int)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					errs[i] = ctx.Err()
					continue
				}
				call := owned[i]
				if err := c.Session.beginTool(); err != nil {
					errs[i] = err
					cancel()
					continue
				}
				text, err := c.Tools[call.Name](context.WithValue(ctx, toolCallKey{}, call.ID), id, append(json.RawMessage(nil), call.Arguments...))
				c.Session.mu.Lock()
				c.Session.active--
				c.Session.mu.Unlock()
				if err == nil {
					err = c.Session.chargeOutput(len(text))
				}
				if ctx.Err() != nil {
					err = ctx.Err()
				}
				errs[i] = err
				out[i] = Message{Role: "tool", ToolID: call.ID, Text: text}
				if err != nil {
					cancel()
				}
			}
		}()
	}
dispatch:
	for i := range owned {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("tool execution: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Session) beginTool() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.Limits.defaults()
	if l.Parallel > 0 && s.active >= l.Parallel || l.ToolCalls > 0 && s.toolCalls >= l.ToolCalls {
		return ErrLimit
	}
	s.active++
	s.toolCalls++
	return nil
}
func (s *Session) chargeOutput(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output += n
	l := s.Limits.defaults()
	if l.OutputBytes > 0 && s.output > l.OutputBytes {
		return ErrLimit
	}
	return nil
}
