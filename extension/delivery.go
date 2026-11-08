package extension

import (
	"context"
	"errors"
	"net/http"
	"sync"
)

// DeliveryEvidence measures calls accepted by the proxy's ResponseWriter, not
// receipt by a remote peer. Bytes excludes headers, trailers and hijacked I/O.
// Status is the final WriteHeader/implicit Write status (zero if none).
// Returned means ServeHTTP returned normally; Aborted means it unwound a panic.
// A normal return alone does not imply success: inspect WriteError and ReadError.
// Hijacked means subsequent connection I/O is outside this accounting boundary.
type DeliveryEvidence struct {
	Status                      int
	Bytes                       int64
	WriteError                  error
	FlushError                  error
	ReadError                   error
	Returned, Aborted, Hijacked bool
}

type deliveryKey struct{}
type deliveryInvocation struct {
	hook  Registration
	input Input
}
type deliveryCapture struct {
	mu       sync.Mutex
	calls    []deliveryInvocation
	captured bool
}

// TrackDelivery scopes evidence to this external proxy invocation. Complete must
// run after ReverseProxy.ServeHTTP (including panic unwinding/body close). It is
// idempotent and does not consume a native invocation slot while forwarding.
// Internal child transports do not emit Delivery. Finish remains reader-only.
// Delivery hooks are bounded best-effort observations: errors cannot undo writes.
func (e *Engine) TrackDelivery(r *http.Request) (*http.Request, func(DeliveryEvidence) error) {
	c := &deliveryCapture{}
	var once sync.Once
	var result error
	complete := func(d DeliveryEvidence) error {
		once.Do(func() {
			c.mu.Lock()
			calls := c.calls
			c.mu.Unlock()
			for _, call := range calls {
				in := call.input
				evidence := d
				evidence.ReadError = in.Err
				in.Delivery = &evidence
				// Reader release cancels its context. Boundary observations instead use
				// engine lifetime plus the same invocation timeout/admission as all hooks.
				out, err := e.invoke(e.ctx, call.hook, in)
				if err == nil && (out.Action != Pass || out.Metadata != nil || out.Credentials != nil) {
					err = ErrUnsupported
				}
				result = errors.Join(result, err)
			}
		})
		return result
	}
	return r.WithContext(context.WithValue(r.Context(), deliveryKey{}, c)), complete
}

func (x *exchange) captureDelivery() {
	// A consumed child body is not an external delivery, even though it inherits
	// the external request context. Each observation retains the root identity and
	// detached provenance/ledger of the effective response.
	if x.id.Parent != "" {
		return
	}
	c, _ := x.ctx.Value(deliveryKey{}).(*deliveryCapture)
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.captured {
		return
	}
	c.captured = true
	for _, h := range x.engine.hooks {
		if h.Stage != Delivery || !applicable(h, x.response.Header) {
			continue
		}
		in := x.input(h)
		in.Request.Body = nil
		in.Response.Body = nil
		in.Call = nil // terminal evidence cannot initiate further transport work
		c.calls = append(c.calls, deliveryInvocation{h, in})
	}
}
