package extension

import (
	"context"
	"net/http"
)

// OwnsCapability only routes to a possible owner; Bind still validates scope
// liveness atomically with its own lookup. Tokens never authorize fresh roots.
func (e *Engine) OwnsCapability(r *http.Request) bool {
	token := r.Header.Get(capabilityHeader)
	if token == "" {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.capabilities[token]
	return ok
}
func HasCapability(r *http.Request) bool { return r.Header.Get(capabilityHeader) != "" }

// WaitIdle is for generation owners after ingress leases reach zero. It does
// not cancel descendants or underlying handlers. No new roots may be admitted.
func (e *Engine) WaitIdle(ctx context.Context) error {
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
