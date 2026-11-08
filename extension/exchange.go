package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
)

type rootState struct {
	mu        sync.Mutex
	attempts  int
	ledger    []Entry
	truncated bool
}
type ingressKey struct{}
type ingress struct {
	engine    *Engine
	scope     context.Context
	requested Message
	parent    *exchange
}
type exchange struct {
	engine                       *Engine
	root                         *rootState
	id                           Identity
	auth                         Authentication
	requested, request, response Message
	source                       Message // detached upstream metadata; never effective/local output
	metadata                     map[string]map[string]string
	credentials                  http.Header
	provenance                   []Identity
	ctx                          context.Context
	base                         http.RoundTripper
	terminal                     error
}

func requestMessage(r *http.Request) Message {
	return Message{Method: r.Method, URL: r.URL.String(), Header: r.Header.Clone()}
}
func responseMessage(r *http.Response) Message {
	return Message{Status: r.StatusCode, Header: r.Header.Clone()}
}
func (e *Engine) execute(ctx context.Context, r *http.Request, base http.RoundTripper, parent *exchange) (_ *http.Response, executeErr error) {
	if r == nil || r.URL == nil {
		return nil, fmt.Errorf("extension: missing request URL")
	}
	// RoundTripper owns the supplied body even when a local response or an
	// admission/policy error prevents delegation to the underlying transport.
	originalBody := r.Body
	delegated := false
	defer func() {
		if !delegated && originalBody != nil {
			originalBody.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.begin(); err != nil {
		return nil, err
	}
	parentctx := ctx
	ctx, cancel := context.WithCancel(parentctx)
	if e.cfg.OperationTimeout > 0 {
		cancel()
		ctx, cancel = context.WithTimeout(parentctx, e.cfg.OperationTimeout)
	}
	stop := context.AfterFunc(e.ctx, cancel)
	stopParent := func() bool { return false }
	stopScope := func() bool { return false }
	var once sync.Once
	release := func() { once.Do(func() { stop(); stopParent(); stopScope(); cancel(); e.wg.Done() }) }
	success := false
	var x *exchange
	defer func() {
		if !success {
			if x != nil && x.id.Attempt > 0 {
				x.terminal = executeErr
				x.recordHost("failed")
				_, finishErr := x.run(Finish, r, nil, nil)
				executeErr = errors.Join(executeErr, finishErr)
				x.captureDelivery()
			}
			release()
		}
	}()
	in, _ := ctx.Value(ingressKey{}).(ingress)
	if in.scope != nil {
		stopScope = context.AfterFunc(in.scope, cancel)
	}
	if parent == nil {
		parent = in.parent
	}
	if parent != nil {
		stopParent = context.AfterFunc(parent.ctx, cancel)
	}
	x = &exchange{engine: e, ctx: ctx, base: base, request: requestMessage(r), requested: requestMessage(r), metadata: map[string]map[string]string{}}
	if parent != nil {
		x.root = parent.root
		x.id.Parent = parent.id.Exchange
		x.id.Root = parent.id.Root
		x.id.Depth = parent.id.Depth + 1
		x.auth = parent.auth
	} else {
		x.root = &rootState{}
		x.id.Root = newID()
		x.auth, _ = ctx.Value(authKey{}).(Authentication)
	}
	if in.requested.URL != "" && parent == nil {
		x.requested = cloneMessage(in.requested)
	}
	x.id.Exchange = newID()
	x.root.mu.Lock()
	if e.cfg.Depth >= 0 && x.id.Depth > e.cfg.Depth || e.cfg.Attempts >= 0 && x.root.attempts >= e.cfg.Attempts {
		x.root.mu.Unlock()
		return nil, ErrLimit
	}
	x.root.attempts++
	x.id.Attempt = x.root.attempts
	x.root.mu.Unlock()
	x.provenance = []Identity{x.id}
	x.ctx = context.WithValue(ctx, exchangeKey{}, x)
	r = cloneRequest(r, x.ctx)
	clean(r.Header)
	x.request.Header = r.Header.Clone()
	var local *http.Response
	for _, stage := range []Stage{Prepare, Resolve, Upstream} {
		var err error
		local, err = x.run(stage, r, nil, nil)
		if err != nil {
			return nil, err
		}
		if local != nil {
			break
		}
	}
	var resp *http.Response
	var err error
	if local != nil {
		resp = local
	} else {
		if err = applyRequest(r, x.request); err != nil {
			return nil, err
		}
		for k, v := range x.credentials {
			r.Header[k] = append([]string(nil), v...)
		}
		clean(r.Header)
		clean(r.Trailer)
		if r.Body != nil {
			r.Body = &trailerBody{ReadCloser: r.Body, trailer: r.Trailer}
		}
		delegated = true
		resp, err = base.RoundTrip(r)
	}
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		x.terminal = err
		_, obsErr := x.run(Observe, r, nil, nil)
		if obsErr != nil {
			return nil, obsErr
		}
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("transport returned nil response")
	}
	if resp.Body == nil {
		resp.Body = http.NoBody
	}
	x.response = responseMessage(resp)
	if local == nil {
		x.source = cloneMessage(x.response)
		x.recordHost("upstream")
	} else {
		x.recordHost("local")
	}
	if local == nil {
		if _, err = x.run(Observe, r, resp, nil); err != nil {
			resp.Body.Close()
			return nil, err
		}
	}
	// Upgrade bodies must retain their io.ReadWriteCloser. No transforms are
	// promised for this bidirectional protocol; reject rather than silently skip.
	if resp.StatusCode == http.StatusSwitchingProtocols {
		for _, h := range e.hooks {
			if h.Stage == ResponseStart || h.Stage == OriginalBody || h.Stage == ResponseBody || h.Stage == Finish || h.Stage == Delivery {
				resp.Body.Close()
				return nil, fmt.Errorf("%w: upgrade response hooks", ErrUnsupported)
			}
		}
		clean(resp.Header)
		duplex, ok := resp.Body.(io.ReadWriteCloser)
		if !ok {
			resp.Body.Close()
			return nil, fmt.Errorf("%w: non-duplex upgrade body", ErrUnsupported)
		}
		body := &upgradeBody{ReadWriteCloser: duplex, release: release}
		resp.Body = body
		context.AfterFunc(ctx, func() { body.Close() })
		success = true
		return resp, nil
	}
	if local == nil {
		resp.Body = &originalReader{ReadCloser: resp.Body, x: x, request: r}
	}
	if _, err = x.run(ResponseStart, r, resp, nil); err != nil {
		resp.Body.Close()
		return nil, err
	}
	// Whole response transforms finish before any delivery, unit transforms run
	// as the transport reader is consumed. Mixing modes is rejected at startup.
	if x.hasWhole() {
		if _, err = x.run(ResponseBody, r, resp, nil); err != nil {
			resp.Body.Close()
			return nil, err
		}
	}
	applyResponse(resp, x.response)
	clean(resp.Header)
	if x.hasUnits() {
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
	}
	resp.Request = r.WithContext(context.WithValue(r.Context(), provenanceKey{}, append([]Identity(nil), x.provenance...)))
	clean(resp.Trailer)
	body := &bodyReader{source: resp.Body, x: x, request: r, release: release, trailer: resp.Trailer}
	resp.Body = body
	context.AfterFunc(ctx, func() { body.Close() })
	success = true
	return resp, nil
}

// cloneRequest keeps Trailer live; http.Request.Clone intentionally snapshots it.
func cloneRequest(r *http.Request, ctx context.Context) *http.Request {
	t := r.Trailer
	c := r.Clone(ctx)
	c.Trailer = t
	return c
}
func applyRequest(r *http.Request, m Message) error {
	u, err := url.Parse(m.URL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%w: request URL", ErrUnsupported)
	}
	if m.Method == "" {
		return fmt.Errorf("%w: empty method", ErrUnsupported)
	}
	r.Method = m.Method
	r.URL = u
	r.Host = u.Host
	r.Header = m.Header.Clone()
	return nil
}
func applyResponse(r *http.Response, m Message) {
	r.StatusCode = m.Status
	r.Status = fmt.Sprintf("%d %s", m.Status, http.StatusText(m.Status))
	r.Header = m.Header.Clone()
}
func (x *exchange) hasWhole() bool {
	for _, h := range x.engine.hooks {
		if h.Stage == ResponseBody && h.Body == Whole {
			return true
		}
	}
	return false
}
func (x *exchange) input(h Registration) Input {
	x.root.mu.Lock()
	ledger := append([]Entry(nil), x.root.ledger...)
	truncated := x.root.truncated
	x.root.mu.Unlock()
	a := x.auth
	a.Claims = copyMap(a.Claims)
	return Input{Identity: x.id, Requested: cloneMessage(x.requested), Request: cloneMessage(x.request), Response: cloneMessage(x.response), Metadata: copyMap(x.metadata[h.Owner]), Ledger: ledger, Truncated: truncated, Provenance: append([]Identity(nil), x.provenance...), Auth: a, Err: x.terminal, Call: func(ctx context.Context, r *http.Request) (*http.Response, error) {
		return x.engine.execute(ctx, r, x.base, x)
	}}
}
func (x *exchange) record(h Registration, outcome string) {
	truncated := len(outcome) > 512
	if truncated {
		outcome = outcome[:512]
	}
	x.root.mu.Lock()
	defer x.root.mu.Unlock()
	x.root.truncated = x.root.truncated || truncated
	if n := x.engine.cfg.LedgerEntries; n >= 0 && len(x.root.ledger) >= n {
		x.root.truncated = true
		return
	}
	requested, served := x.requested.URL, x.request.URL
	if len(requested) > 4096 {
		requested = requested[:4096]
		x.root.truncated = true
	}
	if len(served) > 4096 {
		served = served[:4096]
		x.root.truncated = true
	}
	status := x.response.Status
	if h.Stage == OriginalBody {
		status = x.source.Status
	}
	x.root.ledger = append(x.root.ledger, Entry{Identity: x.id, Hook: h.ID, Stage: h.Stage, Outcome: outcome, RequestedURL: requested, ServedURL: served, Status: status})
}
func (x *exchange) recordHost(outcome string) {
	x.record(Registration{ID: "@host", Stage: Observe}, outcome)
}

func applicable(h Registration, header http.Header) bool {
	if len(h.Formats) == 0 {
		return true
	}
	f, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil {
		return false
	}
	for _, v := range h.Formats {
		if strings.EqualFold(f, v) {
			return true
		}
	}
	return false
}
func boundedRead(r io.Reader, n int) ([]byte, error) {
	if n < 0 {
		return io.ReadAll(r)
	}
	b, err := io.ReadAll(io.LimitReader(r, int64(n)))
	if err != nil || len(b) < n {
		return b, err
	}
	var probe [1]byte
	count, err := io.ReadFull(r, probe[:])
	if count > 0 {
		return nil, ErrLimit
	}
	if err == io.EOF {
		return b, nil
	}
	return nil, err
}
func (x *exchange) run(stage Stage, req *http.Request, resp *http.Response, unit []byte) (*http.Response, error) {
	retained := false
	for _, h := range x.engine.hooks {
		if h.Stage != stage {
			continue
		}
		header := x.request.Header
		if stage >= Observe {
			header = x.response.Header
		}
		if stage == OriginalBody {
			header = x.source.Header
		}
		if !applicable(h, header) {
			continue
		}
		if h.Body == Whole && !retained {
			var b []byte
			var err error
			if stage < ResponseStart {
				if req.Body != nil {
					b, err = boundedRead(req.Body, x.engine.cfg.BodyBytes)
					req.Body.Close()
				}
				req.Body = io.NopCloser(bytes.NewReader(b))
				req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
				x.request.Body = b
			} else {
				b, err = boundedRead(resp.Body, x.engine.cfg.BodyBytes)
				resp.Body.Close()
				resp.Body = io.NopCloser(bytes.NewReader(b))
				x.response.Body = b
			}
			if err != nil {
				return nil, err
			}
			retained = true
		}
		in := x.input(h)
		if stage == OriginalBody {
			in.Response = cloneMessage(x.source)
		}
		if h.Body == NoBody {
			in.Request.Body = nil
			in.Response.Body = nil
		}
		if h.Body == Unit {
			in.Response.Body = append([]byte(nil), unit...)
		}
		result, err := x.engine.invoke(x.ctx, h, in)
		if err == nil {
			err = x.validate(h, result)
		}
		var child *http.Response
		var childBody []byte
		if err == nil && result.Action == Child {
			childReq := cloneRequest(result.Request, x.ctx)
			child, err = x.engine.execute(x.ctx, childReq, x.base, x)
			if err == nil {
				childBody, err = boundedRead(child.Body, x.engine.cfg.BodyBytes)
				child.Body.Close()
			}
		}
		if err != nil {
			x.record(h, err.Error())
			if h.Failure == FailOpen {
				continue
			}
			return nil, fmt.Errorf("hook %s: %w", h.ID, err)
		}
		// Snapshot and validate everything before committing metadata or content.
		if result.Metadata != nil {
			x.metadata[h.Owner] = copyMap(result.Metadata)
		}
		if result.Credentials != nil {
			x.credentials = result.Credentials.Clone()
		}
		x.record(h, fmt.Sprint(result.Action))
		switch result.Action {
		case Replace:
			m := cloneMessage(*result.Message)
			if stage < ResponseStart {
				if h.Body == Whole {
					req.Body = io.NopCloser(bytes.NewReader(m.Body))
					req.ContentLength = int64(len(m.Body))
					req.GetBody = nil
					m.Header.Del("Content-Length")
				} else {
					m.Body = x.request.Body
				}
				x.request = m
			} else {
				if h.Body == Whole {
					resp.Body = io.NopCloser(bytes.NewReader(m.Body))
					resp.ContentLength = int64(len(m.Body))
					m.Header.Del("Content-Length")
				} else if h.Body == Unit {
					unit = append(unit[:0], m.Body...)
				} else {
					m.Body = x.response.Body
				}
				x.response = m
			}
		case Respond:
			m := cloneMessage(*result.Message)
			x.response = m
			return &http.Response{StatusCode: m.Status, Header: m.Header.Clone(), Body: io.NopCloser(bytes.NewReader(m.Body)), ContentLength: int64(len(m.Body)), Request: req}, nil
		case Child:
			x.recordHost("source-abandoned")
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(childBody))
			resp.StatusCode = child.StatusCode
			resp.Header = child.Header.Clone()
			resp.Trailer = child.Trailer.Clone()
			resp.ContentLength = int64(len(childBody))
			x.response = responseMessage(resp)
			x.response.Body = childBody
			if child.Request != nil {
				if p, ok := child.Request.Context().Value(provenanceKey{}).([]Identity); ok {
					x.provenance = append(x.provenance, p...)
				}
			}
		}
	}
	if stage == ResponseBody && unit != nil {
		x.response.Body = append([]byte(nil), unit...)
	}
	return nil, nil
}
func (x *exchange) validate(h Registration, r Result) error {
	size := 0
	for owner, m := range x.metadata {
		if owner == h.Owner && r.Metadata != nil {
			continue
		}
		for k, v := range m {
			size += len(k) + len(v)
		}
	}
	for k, v := range r.Metadata {
		size += len(k) + len(v)
	}
	if x.engine.cfg.MetadataBytes >= 0 && size > x.engine.cfg.MetadataBytes {
		return ErrLimit
	}
	if r.Action > Child {
		return fmt.Errorf("%w: action", ErrUnsupported)
	}
	if r.Credentials != nil && h.Stage != Resolve && h.Stage != Upstream {
		return fmt.Errorf("%w: credential stage", ErrUnsupported)
	}
	if (h.Stage == Observe || h.Stage == OriginalBody || h.Stage == Finish || h.Stage == Delivery) && (r.Action != Pass || r.Metadata != nil || r.Credentials != nil) {
		return fmt.Errorf("%w: observation mutation", ErrUnsupported)
	}
	if r.Action == Replace || r.Action == Respond {
		if r.Message == nil {
			return fmt.Errorf("%w: missing snapshot", ErrUnsupported)
		}
		if h.Stage == ResponseBody && !x.hasWhole() && (r.Message.Status != x.response.Status || !reflect.DeepEqual(r.Message.Header, x.response.Header)) {
			return fmt.Errorf("%w: streaming headers/status already committed; use ResponseStart", ErrUnsupported)
		}
		if (r.Action == Respond || h.Stage >= ResponseStart) && len(r.Message.Body) > 0 && (x.request.Method == http.MethodHead || r.Message.Status == 204 || r.Message.Status == 304) {
			return fmt.Errorf("%w: body forbidden for method/status", ErrUnsupported)
		}
		if (h.Body != Unit || r.Action == Respond) && x.engine.cfg.BodyBytes >= 0 && len(r.Message.Body) > x.engine.cfg.BodyBytes {
			return ErrLimit
		}
		if h.Body == Unit && len(r.Message.Body) > x.engine.cfg.UnitBytes {
			return ErrLimit
		}
		if r.Action == Respond {
			if h.Stage > Upstream || r.Message.Status < 200 || r.Message.Status > 599 {
				return fmt.Errorf("%w: local response", ErrUnsupported)
			}
		} else if h.Stage < ResponseStart {
			dummy := new(http.Request)
			if err := applyRequest(dummy, *r.Message); err != nil {
				return err
			}
		} else if r.Message.Status < 200 || r.Message.Status > 599 {
			return fmt.Errorf("%w: response status", ErrUnsupported)
		}
		if h.Body == NoBody && len(r.Message.Body) > 0 && r.Action == Replace {
			return fmt.Errorf("%w: undeclared body access", ErrUnsupported)
		}
	}
	if r.Action == Child && (h.Stage != ResponseStart || r.Request == nil) {
		return fmt.Errorf("%w: child stage/request", ErrUnsupported)
	}
	return nil
}

type provenanceKey struct{}
