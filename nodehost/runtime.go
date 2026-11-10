package nodehost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/worker"
)

type RuntimeOptions struct {
	Options
	Drain, Cleanup time.Duration
	MaxDraining    int
}
type generation struct {
	id        uint64
	host      *Host
	ctx       context.Context
	cancel    context.CancelFunc
	leases    int
	finishing bool
	settled   bool // native work settled; worker cleanup may still own this generation
}
type instance struct {
	record    extensions.Installation
	worker    *worker.Worker
	stopping  bool
	preparing bool
}

// Runtime owns immutable pipelines, not a listener. Operations serialize before
// preparation; mu gates only ingress selection, state commit and publication.
type Runtime struct {
	txn          chan struct{}
	stop         context.Context
	cancel       context.CancelFunc
	shutdownDone chan struct{}
	mu           sync.Mutex
	options      RuntimeOptions
	installer    extensions.Installer
	current      *generation
	draining     map[uint64]*generation
	instances    map[string]*instance
	failures     map[string]string
	next         uint64
	closed       bool
	retired      sync.WaitGroup
	forced       uint64
	detail       string
	cleanupErr   error
}

func OpenRuntime(ctx context.Context, o RuntimeOptions) (*Runtime, error) {
	if o.Drain == 0 {
		o.Drain = 30 * time.Second
	}
	if o.Cleanup == 0 {
		o.Cleanup = 5 * time.Second
	}
	if o.MaxDraining == 0 {
		o.MaxDraining = 4
	}
	if o.Drain < 0 || o.Cleanup < 0 || o.MaxDraining < 1 {
		return nil, errors.New("invalid runtime drain bounds")
	}
	n := o.Engine.Admission
	if n == 0 {
		n = 64
	}
	if n < 1 {
		return nil, errors.New("live runtime requires finite shared admission")
	}
	o.Engine.SharedAdmission = make(chan struct{}, n)
	stop, cancel := context.WithCancel(context.Background())
	m := &Runtime{txn: make(chan struct{}, 1), stop: stop, cancel: cancel, shutdownDone: make(chan struct{}), options: o, installer: extensions.Installer{Store: o.Store, Owned: true, NodeBinary: o.Worker.NodeBinary}, draining: map[uint64]*generation{}, instances: map[string]*instance{}, failures: map[string]string{}}
	defer func() {
		if m.current == nil {
			cancel()
		}
	}()
	s, e := m.installer.State()
	if e != nil {
		return nil, e
	}
	records, e := m.installer.Enabled(s)
	if e != nil {
		return nil, e
	}
	h, e := buildHost(ctx, o.Options, records, nil, nil)
	if e != nil {
		return nil, e
	}
	m.current = m.newGeneration(h)
	for _, r := range records {
		m.instances[r.Manifest.ID] = &instance{record: r, worker: h.instances[r.Manifest.ID]}
	}
	return m, nil
}
func (m *Runtime) newGeneration(h *Host) *generation {
	m.next++
	ctx, cancel := context.WithCancel(context.Background())
	return &generation{id: m.next, host: h, ctx: ctx, cancel: cancel}
}

// Acquire selects once before Bind/authentication/rewrite. Capability requests
// can select only their original live engine; invalid tokens never become roots.
func (m *Runtime) Acquire(r *http.Request) (*extension.Engine, *http.Request, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, nil, extension.ErrClosed
	}
	g := m.current
	if extension.HasCapability(r) {
		g = nil
		if m.current.host.Engine.OwnsCapability(r) {
			g = m.current
		}
		if g == nil {
			for _, old := range m.draining {
				if !old.finishing && old.host.Engine.OwnsCapability(r) {
					g = old
					break
				}
			}
		}
		if g == nil {
			return nil, nil, nil, errors.New("invalid or expired managed capability")
		}
	}
	if g.finishing {
		return nil, nil, nil, extension.ErrClosed
	}
	g.leases++
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(g.ctx, cancel)
	var once sync.Once
	release := func() { once.Do(func() { stop(); cancel(); m.mu.Lock(); g.leases--; m.mu.Unlock() }) }
	return g.host.Engine, r.WithContext(ctx), release, nil
}

// admit serializes management work without making queued callers wait forever.
func (m *Runtime) admit(ctx context.Context) error {
	if m.stop.Err() != nil {
		return extension.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-m.stop.Done():
		return extension.ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	case m.txn <- struct{}{}:
		if m.stop.Err() != nil {
			<-m.txn
			return extension.ErrClosed
		}
		if err := ctx.Err(); err != nil {
			<-m.txn
			return err
		}
		return nil
	}
}
func (m *Runtime) operationContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.stop, cancel)
	return ctx, func() { stop(); cancel() }
}

func (m *Runtime) Change(ctx context.Context, id string, enabled bool) (err error) {
	if err = m.admit(ctx); err != nil {
		return err
	}
	defer func() { <-m.txn }()
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			m.failures[id] = "activation: " + err.Error()
		} else {
			delete(m.failures, id)
		}
	}()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return extension.ErrClosed
	}
	s, e := m.installer.State()
	if e != nil {
		return e
	}
	candidate, e := m.installer.Decide(s, id, enabled)
	if e != nil {
		return e
	}
	// Idempotent replies also reconcile lost successful responses.
	if old, ok := s.Decisions[id]; ok && old == candidate.Decisions[id] {
		return nil
	}
	records, e := m.installer.Enabled(candidate)
	if e != nil {
		return e
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return extension.ErrClosed
	}
	if len(m.draining) >= m.options.MaxDraining {
		m.mu.Unlock()
		return errors.New("activation busy: draining generation limit")
	}
	m.pruneReapedLocked()
	reuse := map[string]*worker.Worker{}
	for _, r := range records {
		if live := m.instances[r.Manifest.ID]; live != nil {
			if !live.record.Equal(r) || live.stopping {
				m.mu.Unlock()
				return fmt.Errorf("%s: overlapping replacement or cleanup in progress", r.Manifest.ID)
			}
			if e = live.worker.Health(); e != nil {
				m.mu.Unlock()
				return fmt.Errorf("%s: %w", r.Manifest.ID, e)
			}
			reuse[r.Manifest.ID] = live.worker
		}
	}
	count := len(m.instances) + len(records) - len(reuse)
	if count > 64 {
		m.mu.Unlock()
		return errors.New("activation busy: live worker limit")
	}
	// Reserve reused workers before retirement can observe them as orphaned.
	reserved := map[string]*instance{}
	for id := range reuse {
		w := m.instances[id]
		w.preparing = true
		reserved[id] = w
	}
	m.mu.Unlock()
	prepared := map[string]*instance{}
	defer func() {
		if err == nil {
			return
		}
		m.mu.Lock()
		for _, w := range reserved {
			w.preparing = false
		}
		for id, w := range prepared {
			w.preparing = false
			w.stopping = true
			if w.worker.Reaped() && m.instances[id] == w {
				delete(m.instances, id)
			}
		}
		m.mu.Unlock()
		m.cleanupUnused()
	}()
	h, e := buildHost(ctx, m.options.Options, records, reuse, func(r extensions.Installation, w *worker.Worker) {
		m.mu.Lock()
		defer m.mu.Unlock()
		live := &instance{record: r, worker: w, preparing: true}
		prepared[r.Manifest.ID] = live
		m.instances[r.Manifest.ID] = live
	})
	if e != nil {
		return e
	}
	abort := func() {
		c, cancel := context.WithTimeout(context.Background(), m.options.Cleanup)
		defer cancel()
		_ = h.Close(c)
	}
	m.mu.Lock()
	e = ctx.Err()
	if m.closed {
		e = extension.ErrClosed
	}
	if e == nil {
		for id, w := range h.instances {
			live := m.instances[id]
			expected := reserved[id]
			if expected == nil {
				expected = prepared[id]
			}
			if live == nil || live != expected || live.worker != w || live.stopping {
				e = fmt.Errorf("%s: worker ownership changed during preparation", id)
				break
			}
			if e = w.Health(); e != nil {
				break
			}
		}
	}
	if e == nil {
		e = m.installer.SaveState(candidate)
	}
	if e != nil {
		m.mu.Unlock()
		abort()
		return e
	}
	for _, w := range prepared {
		w.preparing = false
	}
	for _, w := range reserved {
		w.preparing = false
	}
	old := m.current
	m.current = m.newGeneration(h)
	m.draining[old.id] = old
	for _, r := range records {
		if m.instances[r.Manifest.ID] == nil {
			m.instances[r.Manifest.ID] = &instance{record: r, worker: h.instances[r.Manifest.ID]}
		}
	}
	m.retired.Add(1)
	m.mu.Unlock()
	go m.retire(old)
	return nil
}
func (m *Runtime) retire(g *generation) {
	defer m.retired.Done()
	drain, cancel := context.WithTimeout(context.Background(), m.options.Drain)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	forced := false
wait:
	for {
		m.mu.Lock()
		zero := g.leases == 0
		if zero {
			g.finishing = true
		}
		m.mu.Unlock()
		if zero {
			if g.host.Engine.WaitIdle(drain) != nil {
				forced = true
			}
			break
		}
		select {
		case <-drain.Done():
			forced = true
			break wait
		case <-ticker.C:
		}
	}
	m.mu.Lock()
	g.finishing = true
	if forced {
		m.forced++
		m.detail = fmt.Sprintf("forced generation %d retirement; %d ingress requests interrupted; shared worker cancellation may affect current requests", g.id, g.leases)
	}
	m.mu.Unlock()
	g.cancel()
	cleanup, stop := context.WithTimeout(context.Background(), m.options.Cleanup)
	err := g.host.Engine.Shutdown(cleanup)
	stop()
	if err != nil {
		m.mu.Lock()
		m.detail = fmt.Sprintf("%s; generation %d settlement pending: %v", m.detail, g.id, err)
		m.mu.Unlock()
		// Expiring cleanup is not actual settlement. Quarantine this generation
		// against the draining bound until retained invocations/callbacks exit.
		// Close's caller deadline still bounds server shutdown.
		_ = g.host.Engine.WaitIdle(context.Background())
	}
	// No worker can be reused after it is marked stopping. Keep registry entries
	// until actual process reaping is confirmed, even if bounded cleanup expires.
	m.mu.Lock()
	g.settled = true
	m.mu.Unlock()
	m.cleanupUnused()
	m.mu.Lock()
	delete(m.draining, g.id)
	m.mu.Unlock()
}

// Collect workers after either generation settlement or candidate abort.
func (m *Runtime) cleanupUnused() {
	m.mu.Lock()
	var orphan map[string]*instance = map[string]*instance{}
	for id, w := range m.instances {
		used := m.current != nil && m.current.host.instances[id] == w.worker
		for _, other := range m.draining {
			used = used || (!other.settled && other.host.instances[id] == w.worker)
		}
		if !used && !w.stopping && !w.preparing {
			w.stopping = true
			orphan[id] = w
		}
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for id, w := range orphan {
		wg.Add(1)
		go func(id string, w *instance) {
			defer wg.Done()
			c, cancel := context.WithTimeout(context.Background(), m.options.Cleanup)
			e := w.worker.Stop(c)
			cancel()
			m.mu.Lock()
			defer m.mu.Unlock()
			if e != nil {
				m.failures[id] = e.Error()
				m.cleanupErr = errors.Join(m.cleanupErr, fmt.Errorf("cleanup %s: %w", id, e))
			}
			if w.worker.Reaped() {
				if m.instances[id] == w {
					delete(m.instances, id)
				}
			} else {
				m.failures[id] = "worker still reaping; removal/replacement blocked"
			}
		}(id, w)
	}
	wg.Wait()
}

// Caller holds mu. Failed candidates and orphan cleanup retain ownership until
// a later operation confirms real reaping; no blind retry can overlap processes.
func (m *Runtime) pruneReapedLocked() {
	for id, w := range m.instances {
		if w.stopping && w.worker.Reaped() {
			delete(m.instances, id)
		}
	}
}

func (m *Runtime) Remove(id string) error {
	if err := m.admit(context.Background()); err != nil {
		return err
	}
	defer func() { <-m.txn }()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return extension.ErrClosed
	}
	m.pruneReapedLocked()
	if m.instances[id] != nil {
		return errors.New("extension active, draining or still reaping; disable and wait before removal")
	}
	return m.installer.Remove(id)
}
func (m *Runtime) Install(ctx context.Context, source string, trust bool) (extensions.Installation, error) {
	if err := m.admit(ctx); err != nil {
		return extensions.Installation{}, err
	}
	defer func() { <-m.txn }()
	ctx, cancel := m.operationContext(ctx)
	defer cancel()
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return extensions.Installation{}, extension.ErrClosed
	}
	return m.installer.Install(ctx, source, trust)
}
func (m *Runtime) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closed {
		m.closed = true
		m.cancel()
		// Teardown retains the transaction even if this caller times out.
		// All retirement additions finish before Wait; no candidate can publish.
		go func() {
			m.txn <- struct{}{}
			defer func() { <-m.txn }()
			m.mu.Lock()
			g := m.current
			m.current = nil
			m.draining[g.id] = g
			m.retired.Add(1)
			m.mu.Unlock()
			go m.retire(g)
			m.retired.Wait()
			// Bounded Stop may return before actual process reaping. Keep
			// shutdown ownership until every registry owner has really exited,
			// including workers from an aborted preparing transaction.
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				m.mu.Lock()
				m.pruneReapedLocked()
				done := len(m.instances) == 0
				m.mu.Unlock()
				if done {
					break
				}
				<-ticker.C
			}
			close(m.shutdownDone)
		}()
	}
	m.mu.Unlock()
	select {
	case <-m.shutdownDone:
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.cleanupErr
	case <-ctx.Done():
		m.mu.Lock()
		for _, g := range m.draining {
			g.cancel()
		}
		m.mu.Unlock()
		return ctx.Err()
	}
}

type RuntimeStatus struct {
	Revision, Generation, Forced uint64
	Draining                     []uint64
	Detail                       string
	Rows                         []StatusRow
}
type StatusRow struct {
	ID, Version                  string
	API                          int
	Enabled                      string
	Active                       bool
	Draining                     []uint64
	Incompatible, Failed, Detail string
}

func (m *Runtime) Status() (RuntimeStatus, error) {
	if err := m.admit(context.Background()); err != nil {
		return RuntimeStatus{}, err
	}
	defer func() { <-m.txn }()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneReapedLocked()
	s, e := m.installer.State()
	if e != nil {
		return RuntimeStatus{}, e
	}
	rows, e := m.installer.Inventory()
	if e != nil {
		return RuntimeStatus{}, e
	}
	result := RuntimeStatus{Revision: s.Revision, Forced: m.forced, Detail: m.detail}
	if m.current != nil {
		result.Generation = m.current.id
	}
	for id := range m.draining {
		result.Draining = append(result.Draining, id)
	}
	for _, row := range rows {
		r := StatusRow{ID: row.ID, Version: row.Installation.Manifest.Version, API: row.Installation.Manifest.APIVersion, Enabled: "unknown", Incompatible: row.Diagnostic}
		if d, ok := s.Decisions[row.ID]; ok && d.Commit == row.Installation.Commit {
			r.Enabled = fmt.Sprint(d.Enabled)
		}
		if m.current != nil {
			r.Active = m.current.host.instances[row.ID] != nil
		}
		for id, g := range m.draining {
			if g.host.instances[row.ID] != nil {
				r.Draining = append(r.Draining, id)
			}
		}
		if w := m.instances[row.ID]; w != nil {
			if e := w.worker.Health(); e != nil {
				r.Failed = e.Error()
			}
			if w.stopping {
				r.Detail = "cleanup/reaping"
			}
		}
		if f := m.failures[row.ID]; f != "" {
			r.Failed = f
		}
		result.Rows = append(result.Rows, r)
	}
	return result, nil
}
