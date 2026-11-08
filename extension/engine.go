package extension

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

type Engine struct {
	cfg          Config
	hooks        []Registration
	slots        chan struct{}
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	closed       bool
	wg           sync.WaitGroup
	capabilities map[string]capability
}

func New(cfg Config, registrations ...Registration) (*Engine, error) {
	if cfg.InvocationTimeout == 0 {
		cfg.InvocationTimeout = 30 * time.Second
	}
	if cfg.OperationTimeout == 0 {
		cfg.OperationTimeout = 5 * time.Minute
	}
	defaults := []struct {
		p *int
		n int
	}{{&cfg.Admission, 64}, {&cfg.BodyBytes, 4 << 20}, {&cfg.UnitBytes, 32 << 10}, {&cfg.MetadataBytes, 64 << 10}, {&cfg.LedgerEntries, 256}, {&cfg.Attempts, 16}, {&cfg.Depth, 8}}
	for _, d := range defaults {
		if *d.p == 0 {
			*d.p = d.n
		}
	}
	// Units must remain finite even when the total body bound is disabled.
	if cfg.UnitBytes < 0 {
		return nil, fmt.Errorf("unit size must be positive")
	}
	hooks, err := order(registrations)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{cfg: cfg, hooks: hooks, ctx: ctx, cancel: cancel, capabilities: make(map[string]capability)}
	if cfg.Admission > 0 {
		e.slots = make(chan struct{}, cfg.Admission)
	}
	for i, h := range hooks {
		if h.Setup != nil {
			if err := h.Setup(ctx); err != nil {
				cancel()
				for j := i - 1; j >= 0; j-- {
					if hooks[j].Cleanup != nil {
						_ = hooks[j].Cleanup(context.Background())
					}
				}
				return nil, fmt.Errorf("setup %s: %w", h.ID, err)
			}
		}
	}
	return e, nil
}
func order(regs []Registration) ([]Registration, error) {
	byID := make(map[string]Registration)
	for _, r := range regs {
		if r.ID == "" || len(r.ID) > 128 || r.ID == "@host" || r.Owner == "" || len(r.Owner) > 128 || r.Handler == nil || r.Stage > Delivery || r.Body > Unit || r.Failure > FailOpen {
			return nil, fmt.Errorf("invalid registration %q", r.ID)
		}
		if _, ok := byID[r.ID]; ok {
			return nil, fmt.Errorf("duplicate hook %s", r.ID)
		}
		if r.Body == Unit && r.Stage != ResponseBody && r.Stage != OriginalBody || r.Body != NoBody && (r.Stage == Observe || r.Stage == Finish || r.Stage == Delivery || r.Stage == ResponseStart) || r.Stage == OriginalBody && r.Body != Unit {
			return nil, fmt.Errorf("%s: %w: body mode/stage", r.ID, ErrUnsupported)
		}
		r.Formats = append([]string(nil), r.Formats...)
		r.Requires = append([]string(nil), r.Requires...)
		r.Before = append([]string(nil), r.Before...)
		r.After = append([]string(nil), r.After...)
		byID[r.ID] = r
	}
	whole, unit := false, false
	for _, r := range regs {
		if r.Stage == ResponseBody {
			whole = whole || r.Body == Whole
			unit = unit || r.Body == Unit
		}
	}
	if whole && unit {
		return nil, fmt.Errorf("%w: mixing whole and unit response transforms", ErrUnsupported)
	}
	edges := make(map[string]map[string]bool)
	indeg := make(map[string]int)
	add := func(a, b string) error {
		if byID[a].Stage > byID[b].Stage {
			return fmt.Errorf("order %s before %s contradicts stages", a, b)
		}
		if edges[a] == nil {
			edges[a] = map[string]bool{}
		}
		if !edges[a][b] {
			edges[a][b] = true
			indeg[b]++
		}
		return nil
	}
	for _, r := range byID {
		for _, id := range r.Requires {
			if _, ok := byID[id]; !ok {
				return nil, fmt.Errorf("%s requires %s", r.ID, id)
			}
			if err := add(id, r.ID); err != nil {
				return nil, err
			}
		}
		for _, id := range r.After {
			if _, ok := byID[id]; ok {
				if err := add(id, r.ID); err != nil {
					return nil, err
				}
			}
		}
		for _, id := range r.Before {
			if _, ok := byID[id]; ok {
				if err := add(r.ID, id); err != nil {
					return nil, err
				}
			}
		}
	}
	var result []Registration
	for len(byID) > 0 {
		var ready []Registration
		for id, r := range byID {
			if indeg[id] == 0 {
				ready = append(ready, r)
			}
		}
		if len(ready) == 0 {
			return nil, fmt.Errorf("hook dependency cycle")
		}
		sort.Slice(ready, func(i, j int) bool {
			if ready[i].Stage != ready[j].Stage {
				return ready[i].Stage < ready[j].Stage
			}
			return ready[i].ID < ready[j].ID
		})
		r := ready[0]
		result = append(result, r)
		delete(byID, r.ID)
		for id := range edges[r.ID] {
			indeg[id]--
		}
	}
	return result, nil
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (e *Engine) begin() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	e.wg.Add(1)
	return nil
}
func (e *Engine) invoke(ctx context.Context, h Registration, in Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := e.begin(); err != nil {
		return Result{}, err
	}
	if e.slots != nil {
		select {
		case e.slots <- struct{}{}:
		default:
			e.wg.Done()
			return Result{}, ErrOverload
		}
	}
	type answer struct {
		r   Result
		err error
	}
	done := make(chan answer, 1)
	callctx, cancel := context.WithCancel(ctx)
	if e.cfg.InvocationTimeout > 0 {
		cancel()
		callctx, cancel = context.WithTimeout(ctx, e.cfg.InvocationTimeout)
	}
	defer cancel()
	go func() {
		defer e.wg.Done()
		if e.slots != nil {
			defer func() { <-e.slots }()
		}
		a := answer{}
		defer func() {
			if p := recover(); p != nil {
				a.err = fmt.Errorf("hook %s panic: %v", h.ID, p)
			}
			done <- a
		}()
		a.r, a.err = h.Handler(callctx, in)
		// Copy before publishing; retaining and later modifying returned values is
		// forbidden by the handler contract (as with http.RoundTripper).
		if a.r.Message != nil {
			limit := e.cfg.BodyBytes
			if h.Body == Unit && a.r.Action != Respond {
				limit = e.cfg.UnitBytes
			}
			if limit >= 0 && len(a.r.Message.Body) > limit {
				a = answer{err: ErrLimit}
				return
			}
			m := cloneMessage(*a.r.Message)
			a.r.Message = &m
		}
		if e.cfg.MetadataBytes >= 0 {
			size := 0
			for k, v := range a.r.Metadata {
				size += len(k) + len(v)
				if size > e.cfg.MetadataBytes {
					a = answer{err: ErrLimit}
					return
				}
			}
		}
		a.r.Metadata = cloneOptional(a.r.Metadata)
		a.r.Credentials = a.r.Credentials.Clone()
	}()
	select {
	case a := <-done:
		if err := callctx.Err(); err != nil {
			return Result{}, err
		}
		return a.r, a.err
	case <-callctx.Done():
		return Result{}, callctx.Err()
	}
}
func cloneOptional(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	return copyMap(m)
}

// Shutdown freezes admission immediately and cancels active work. It waits for
// actual native exits before cleanup. A timed-out Shutdown can be retried.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	e.closed = true
	e.cancel()
	e.mu.Unlock()
	done := make(chan struct{})
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var first error
	for i := len(e.hooks) - 1; i >= 0; i-- {
		if f := e.hooks[i].Cleanup; f != nil {
			if err := f(ctx); err != nil && first == nil {
				first = err
			}
			e.hooks[i].Cleanup = nil
		}
	}
	return first
}
func (e *Engine) Close() error { return e.Shutdown(context.Background()) }

// Transport decorates the existing transport; it does not implement forwarding.
// The proxy must call Bind at ingress to retain original facts and authenticate
// host-issued ordinary-HTTP reentry. Direct callers may omit Bind.
func (e *Engine) Transport(base http.RoundTripper) http.RoundTripper {
	return &managedTransport{e: e, base: base}
}

type managedTransport struct {
	e    *Engine
	base http.RoundTripper
}

func (t *managedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return t.e.execute(r.Context(), r, t.base, nil)
}
func (t *managedTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
