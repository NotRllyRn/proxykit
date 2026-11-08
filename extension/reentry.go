package extension

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type exchangeKey struct{}
type capability struct {
	x   *exchange
	ctx context.Context
}

const capabilityHeader = "X-Proxykit-Capability"

// Bind is called by the gateway before rewriting. Only an unguessable capability
// minted by this engine can link an ordinary HTTP request to a live exchange.
// All public lineage-like headers are stripped, including invalid tokens.
func (e *Engine) Bind(r *http.Request) (*http.Request, error) {
	token := r.Header.Get(capabilityHeader)
	r = cloneRequest(r, r.Context())
	clean(r.Header)
	if existing, ok := r.Context().Value(ingressKey{}).(ingress); ok && existing.engine == e {
		return r, nil
	}
	in := ingress{engine: e, requested: requestMessage(r)}
	if token != "" {
		e.mu.Lock()
		cap, ok := e.capabilities[token]
		e.mu.Unlock()
		if !ok || cap.x.ctx.Err() != nil || cap.ctx.Err() != nil {
			return nil, fmt.Errorf("extension: invalid or expired managed capability")
		}
		in.parent = cap.x
		in.scope = cap.ctx
		r = r.WithContext(WithAuthentication(r.Context(), cap.x.auth))
	}
	return r.WithContext(context.WithValue(r.Context(), ingressKey{}, in)), nil
}

// Client provides authenticated ordinary HTTP reentry to exactly one origin.
// It reuses trusted request authentication, NOT parent route credentials. Tokens
// are scoped to the active handler/exchange and revoked when that context ends.
// Redirects are refused; arbitrary sockets do not inherit this managed contract.
func (e *Engine) Client(ctx context.Context, gateway string) (*http.Client, error) {
	x, ok := ctx.Value(exchangeKey{}).(*exchange)
	if !ok || x.engine != e || ctx.Err() != nil {
		return nil, fmt.Errorf("extension: no active managed scope")
	}
	u, err := url.Parse(gateway)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("extension: gateway must be an HTTP(S) origin")
	}
	token := newID()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, ErrClosed
	}
	// A finite per-scope cap avoids unbounded capability registry growth.
	count := 0
	for _, c := range e.capabilities {
		if c.x == x {
			count++
		}
	}
	if count >= 16 {
		e.mu.Unlock()
		return nil, ErrLimit
	}
	e.capabilities[token] = capability{x: x, ctx: ctx}
	e.mu.Unlock()
	context.AfterFunc(ctx, func() { e.mu.Lock(); delete(e.capabilities, token); e.mu.Unlock() })
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	context.AfterFunc(ctx, transport.CloseIdleConnections)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if r.URL.Scheme != u.Scheme || r.URL.Host != u.Host {
			return nil, fmt.Errorf("extension: managed client origin mismatch")
		}
		callctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(ctx, cancel)
		r = cloneRequest(r, callctx)
		clean(r.Header)
		r.Header.Set(capabilityHeader, token)
		resp, err := transport.RoundTrip(r)
		if err != nil {
			stop()
			cancel()
			return nil, err
		}
		resp.Body = &releaseBody{ReadCloser: resp.Body, release: func() { stop(); cancel() }}
		return resp, nil
	})
	return client, nil
}
