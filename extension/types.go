// Package extension executes trusted native hooks. It is not a sandbox: handlers
// must cooperate with cancellation and are responsible for their own side effects.
package extension

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Stage uint8

const (
	Prepare Stage = iota
	Resolve
	Upstream
	Observe       // immutable original attempt response; replacement is forbidden
	ResponseStart // before client commitment; may request a child
	OriginalBody  // immutable bounded upstream byte observations, before decoration
	ResponseBody
	Finish   // transport reader completion, NOT external delivery; replacement forbidden
	Delivery // proxy-boundary evidence, only when TrackDelivery is used
)

type BodyMode uint8

const (
	NoBody BodyMode = iota
	Whole
	Unit
)

type Failure uint8

const (
	Block Failure = iota
	FailOpen
)

type Action uint8

const (
	Pass Action = iota
	Replace
	Respond
	Child
)

var (
	ErrClosed      = errors.New("extension: engine closed")
	ErrOverload    = errors.New("extension: invocation capacity exhausted")
	ErrLimit       = errors.New("extension: configured limit exceeded")
	ErrUnsupported = errors.New("extension: unsupported transformation")
)

// Message is a detached replacement snapshot, not a patch. Body is populated
// only for Whole/Unit hooks. URL must be absolute HTTP(S) for request replacements.
// Credentials are intentionally absent; routing credentials have a separate path.
type Message struct {
	Method string
	URL    string
	Header http.Header
	Status int
	Body   []byte
}
type Identity struct {
	Root, Exchange, Parent string
	Attempt, Depth         int
}
type Entry struct {
	Identity                Identity
	Hook                    string
	Stage                   Stage
	Outcome                 string
	RequestedURL, ServedURL string
	Status                  int
}
type Authentication struct {
	Subject string
	Claims  map[string]string
}
type Input struct {
	Identity   Identity
	Requested  Message // original request facts, with no retained body
	Request    Message
	Response   Message
	Metadata   map[string]string // only this registration's owner namespace
	Ledger     []Entry
	Truncated  bool
	Provenance []Identity
	Auth       Authentication
	Err        error             // transport/read error for terminal observers
	Delivery   *DeliveryEvidence // only at Delivery; detached, not a peer acknowledgement
	// Call executes a fresh request through the ordinary chain with shared root
	// budgets (not external Delivery). It is nil in Delivery snapshots.
	// Authority expires at handler return/timeout even with context.Background;
	// admitted calls are canceled too. Consume/close responses before returning.
	// Returned Result{Action: Child} instead executes under exchange lifetime.
	// It never forwards parent upstream credentials or owner metadata.
	Call func(context.Context, *http.Request) (*http.Response, error)
}
type Result struct {
	Action   Action
	Message  *Message
	Metadata map[string]string // nil preserves; non-nil replaces only the owner's map
	// Credentials are accepted only in Resolve/Upstream and are installed after
	// all request hooks, so unrelated transforms cannot read upstream secrets.
	Credentials http.Header
	Request     *http.Request // Child only; response-start only
}
type Handler func(context.Context, Input) (Result, error)
type Registration struct {
	Owner, ID               string
	Stage                   Stage
	Formats                 []string // MIME types; empty matches all, no check callback
	Requires, Before, After []string // Requires must exist; absent Before/After ignored
	Body                    BodyMode
	Failure                 Failure
	Handler                 Handler
	Setup                   func(context.Context) error
	Cleanup                 func(context.Context) error
}

// Zero means a finite default. Negative values explicitly disable a limit.
// Disable only for trusted deployments prepared to lose that bound.
// UnitBytes is the exception: it must remain positive (negative is rejected).
type Config struct {
	InvocationTimeout, OperationTimeout                                            time.Duration
	Admission, BodyBytes, UnitBytes, MetadataBytes, LedgerEntries, Attempts, Depth int
}

type authKey struct{}

// WithAuthentication is a trusted ingress integration point, not header auth.
func WithAuthentication(ctx context.Context, a Authentication) context.Context {
	a.Claims = copyMap(a.Claims)
	return context.WithValue(ctx, authKey{}, a)
}

// AuthenticationFromContext returns a detached copy for trusted ingress middleware.
// To reuse managed HTTP authentication, call Engine.Bind before that middleware.
func AuthenticationFromContext(ctx context.Context) (Authentication, bool) {
	a, ok := ctx.Value(authKey{}).(Authentication)
	a.Claims = copyMap(a.Claims)
	return a, ok
}

func copyMap(m map[string]string) map[string]string {
	n := make(map[string]string, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}
func cloneMessage(m Message) Message {
	m.Header = m.Header.Clone()
	m.Body = append([]byte(nil), m.Body...)
	return m
}
func clean(h http.Header) {
	for k := range h {
		if len(k) >= len("X-Proxykit-") && equalPrefix(k, "X-Proxykit-") {
			delete(h, k)
		}
	}
}
func equalPrefix(s, p string) bool {
	for i := range p {
		a, b := s[i], p[i]
		if a >= 'A' && a <= 'Z' {
			a += 32
		}
		if b >= 'A' && b <= 'Z' {
			b += 32
		}
		if a != b {
			return false
		}
	}
	return true
}
