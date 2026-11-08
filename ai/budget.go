package ai

import (
	"errors"
	"time"
)

// Budget is private application state, never a client-supplied continuation.
// Persist only at quiescent handoff. A durable reservation must precede effects;
// snapshots alone cannot resolve a crash between an effect and its checkpoint.
type Budget struct {
	Limits                   Limits
	Calls, ToolCalls, Output int
	Root                     string
	Deadline                 time.Time
}

func (s *Session) Budget() (Budget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != 0 {
		return Budget{}, errors.New("cannot checkpoint active session")
	}
	return Budget{Limits: s.Limits.defaults(), Calls: s.calls, ToolCalls: s.toolCalls, Output: s.output, Root: s.root}, nil
}

// RestoreBudget refuses changed limits and nonempty sessions. The application
// owns a durable wall-clock deadline separately from per-invocation Timeout.
func (s *Session) RestoreBudget(b Budget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != 0 || s.calls != 0 || s.toolCalls != 0 || s.output != 0 || s.root != "" {
		return errors.New("session already used")
	}
	if b.Limits != s.Limits.defaults() || b.Calls < 0 || b.ToolCalls < 0 || b.Output < 0 || (b.Calls > 0 && b.Root == "") {
		return errors.New("invalid or changed durable session limits")
	}
	s.calls, s.toolCalls, s.output, s.root = b.Calls, b.ToolCalls, b.Output, b.Root
	return nil
}
