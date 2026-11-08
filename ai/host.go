package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
)

// Attempt is upstream evidence, NOT a Session outcome or external Delivery.
// Body and headers come exclusively from Observe/OriginalBody host snapshots.
type Attempt struct {
	Logical           Lineage
	Caller            Identity
	Identity          extension.Identity
	Request, Response extension.Message
	Served            Route
	Body              []byte
	Usage             Usage
	Error             string
	Complete          bool // bounded source consumed to completion, not a peer acknowledgement
}
type AttemptSink func(context.Context, []Attempt) error

// HostRecorder is independent of presentation. Evidence must resolve the actual
// post-routing request to a trusted route catalog (including account/protocol),
// never echo the requested target. Ambiguous provenance must fail explicitly.
// Configure before registration; callbacks must be concurrency safe.
type HostRecorder struct {
	Owner    string
	MaxBytes int
	Evidence func(extension.Input) (Route, error)
	Sink     AttemptSink // required synchronous durable handoff
}
type captureKey struct{}
type hostCapture struct {
	logical  Lineage
	caller   Identity
	mu       sync.Mutex
	attempts []Attempt
	err      error
	bytes    int
}

func (r *HostRecorder) bound() int {
	if r.MaxBytes <= 0 {
		return 4 << 20
	}
	return r.MaxBytes
}
func (r *HostRecorder) Registrations() []extension.Registration {
	owner := r.Owner
	if owner == "" {
		owner = "ai-recorder"
	}
	return []extension.Registration{
		{Owner: owner, ID: owner + ".observe", Stage: extension.Observe, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
			c, _ := ctx.Value(captureKey{}).(*hostCapture)
			if c == nil {
				return extension.Result{}, nil
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			a := Attempt{Logical: c.logical, Caller: c.caller, Identity: in.Identity, Request: in.Request, Response: in.Response}
			if in.Err != nil {
				a.Error = in.Err.Error()
			}
			if r.Evidence == nil {
				c.err = errors.New("missing authoritative route evidence")
			} else {
				route, err := r.Evidence(in)
				if err != nil {
					c.err = err
				} else if route.Endpoint != in.Request.URL || route.Protocol == "" || route.Account == "" {
					c.err = errors.New("ambiguous served route evidence")
				} else {
					a.Served = route
				}
			}
			c.attempts = append(c.attempts, a)
			return extension.Result{}, c.err
		}},
		{Owner: owner, ID: owner + ".body", Stage: extension.OriginalBody, Body: extension.Unit, Handler: func(ctx context.Context, in extension.Input) (extension.Result, error) {
			c, _ := ctx.Value(captureKey{}).(*hostCapture)
			if c == nil {
				return extension.Result{}, nil
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			for i := range c.attempts {
				a := &c.attempts[i]
				if a.Identity.Exchange == in.Identity.Exchange {
					if len(in.Response.Body) > r.bound()-c.bytes {
						c.err = ErrLimit
						return extension.Result{}, c.err
					}
					a.Body = append(a.Body, in.Response.Body...)
					c.bytes += len(in.Response.Body)
					return extension.Result{}, nil
				}
			}
			return extension.Result{}, errors.New("original body without observation")
		}},
	}
}

// HostManaged is a scoped bridge, constructed inside an owned registration.
// Bind CallChild to extension.Input.Call from that live invocation so each call
// reenters normal authentication, routing, authorization and accounting hooks.
// Like other trusted callbacks, this cannot police an unrelated implementation.
type HostManaged struct {
	CallChild func(context.Context, *http.Request) (*http.Response, error)
	Recorder  *HostRecorder
	Protocols map[string]Protocol
}

func (h HostManaged) Call(ctx context.Context, req Request) (result Result, err error) {
	if h.CallChild == nil || h.Recorder == nil || h.Recorder.Sink == nil {
		return result, errors.New("missing managed host/required recorder")
	}
	p := h.Protocols[req.Target.Protocol]
	if p == nil || p.Name() != req.Target.Protocol {
		return result, fmt.Errorf("%w: target protocol", ErrUnsupported)
	}
	wire, err := p.EncodeRequest(req)
	if err != nil {
		return result, err
	}
	child, err := http.NewRequestWithContext(ctx, http.MethodPost, req.Target.Endpoint, bytes.NewReader(wire))
	if err != nil {
		return result, err
	}
	child.Header.Set("Content-Type", "application/json")
	c := &hostCapture{logical: req.Lineage, caller: req.Identity}
	ctx = context.WithValue(ctx, captureKey{}, c)
	child = child.WithContext(ctx)
	// Parent httptrace can be inherited through ctx. Coordinated ingress must use
	// a selection gate to suppress informational callbacks until final selection.
	// This bridge never copies returned child headers to the client.
	resp, callErr := h.CallChild(ctx, child)
	if resp != nil {
		var n int64
		n, err = io.Copy(io.Discard, io.LimitReader(resp.Body, int64(h.Recorder.bound())+1))
		if n > int64(h.Recorder.bound()) {
			err = errors.Join(err, ErrLimit)
		}
		closeErr := resp.Body.Close()
		callErr = errors.Join(callErr, err, closeErr)
	}
	c.mu.Lock()
	attempts := clone(c.attempts)
	captureErr := c.err
	c.mu.Unlock()
	if len(attempts) == 0 {
		callErr = errors.Join(callErr, errors.New("no original upstream evidence (local or denied response)"))
	}
	var decodeErr error
	for i := range attempts {
		a := &attempts[i]
		protocol := h.Protocols[a.Served.Protocol]
		if protocol == nil || protocol.Name() != a.Served.Protocol {
			decodeErr = errors.Join(decodeErr, ErrUnsupported)
			continue
		}
		var decoded Response
		typ, _, _ := mime.ParseMediaType(a.Response.Header.Get("Content-Type"))
		var e error
		switch typ {
		case "application/json":
			decoded, e = protocol.Decode(a.Body)
		case "text/event-stream":
			sp, ok := protocol.(StreamingProtocol)
			if !ok {
				e = ErrUnsupported
			} else {
				decoded, _, e = sp.DecodeStream(bytes.NewReader(a.Body), StreamLimits{Bytes: h.Recorder.bound()})
			}
		default:
			e = fmt.Errorf("%w: response MIME %q", ErrUnsupported, typ)
		}
		a.Usage = decoded.Usage
		if decoded.Model == "" {
			e = errors.Join(e, fmt.Errorf("%w: missing actual served model evidence", ErrUnsupported))
		}
		if a.Response.Status < 200 || a.Response.Status >= 300 {
			e = errors.Join(e, fmt.Errorf("upstream status %d", a.Response.Status))
		}
		if e != nil {
			if a.Error == "" {
				a.Error = e.Error()
			} else {
				a.Error = errors.Join(errors.New(a.Error), e).Error()
			}
		}
		a.Complete = callErr == nil && captureErr == nil && len(attempts) == 1
		decodeErr = errors.Join(decodeErr, e)
		// Response model is provider evidence, not a requested-model guess.
		a.Served.Model = decoded.Model
		if i == len(attempts)-1 {
			result = Result{Response: decoded, Served: a.Served}
		}
	}
	if len(attempts) > 0 && callErr != nil {
		last := &attempts[len(attempts)-1]
		last.Error = callErr.Error()
	}
	if len(attempts) > 1 {
		decodeErr = errors.Join(decodeErr, fmt.Errorf("%w: ambiguous multiple upstream sources", ErrUnsupported))
	}
	// Sink runs before any logical success or selected response can escape. A
	// cancellation does not suppress required evidence; persistence has its own bound.
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	sinkErr := h.Recorder.Sink(persist, clone(attempts))
	cancel()
	err = errors.Join(callErr, captureErr, decodeErr, sinkErr, ctx.Err())
	return result, err
}
