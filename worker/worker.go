// Package worker supervises trusted Node ESM processes. It does not attach HTTP hooks or sandbox code.
package worker

import (
	"context"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

//go:embed runtime.mjs
var runtimeSource string

var ErrBusy = errors.New("worker concurrency exhausted")
var hookID = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]{0,127}$`)

type HookDescriptor struct {
	ID       string   `json:"id"`
	Stage    string   `json:"stage"`
	BodyMode string   `json:"bodyMode"`
	Order    float64  `json:"order,omitempty"`
	Failure  string   `json:"failure,omitempty"`
	Formats  []string `json:"formats,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Before   []string `json:"before,omitempty"`
	After    []string `json:"after,omitempty"`
}

func cloneHooks(hooks []HookDescriptor) []HookDescriptor {
	out := append([]HookDescriptor(nil), hooks...)
	for i := range out {
		out[i].Formats = append([]string(nil), out[i].Formats...)
		out[i].Requires = append([]string(nil), out[i].Requires...)
		out[i].Before = append([]string(nil), out[i].Before...)
		out[i].After = append([]string(nil), out[i].After...)
	}
	return out
}

type Options struct {
	NodeBinary                                                string
	Entry                                                     string
	DataDir                                                   string // host-created absolute persistent path; optional for low-level workers
	Config                                                    json.RawMessage
	Stderr                                                    io.Writer
	MaxFrame                                                  int
	Concurrency                                               int
	CallTimeout, StartupTimeout, CancelGrace, ShutdownTimeout time.Duration
}
type Worker struct {
	options       Options
	mu            sync.Mutex
	lifecycle     sync.Mutex // serializes Start and Restart
	current       *process
	last          *process // retains the last Stop outcome until a new Start
	next          atomic.Uint64
	callbackSlots chan struct{} // retained until Go callback settlement, including across restart
}
type reply struct {
	output json.RawMessage
	err    error
}
type pending struct {
	ch             chan reply
	settled        chan struct{}
	callbacks      sync.WaitGroup
	cancelled      bool
	ctx            context.Context
	cancel         context.CancelFunc
	callback       Callback
	callbackActive bool
	nextCall       uint64
}
type process struct {
	owner      *Worker
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	mu         sync.Mutex
	pending    map[string]*pending
	hooks      []HookDescriptor
	ready      bool
	stopping   bool
	stoppedAck bool
	failure    error
	readyCh    chan error
	exited     chan struct{}
	writes     chan []byte
	once       sync.Once
	stopOnce   sync.Once
	killOnce   sync.Once
	readerDone chan struct{}
}

func New(o Options) (*Worker, error) {
	if o.NodeBinary == "" {
		o.NodeBinary = "node"
	}
	if o.MaxFrame == 0 {
		o.MaxFrame = 1 << 20
	}
	if o.Concurrency == 0 {
		o.Concurrency = 16
	}
	if o.CallTimeout == 0 {
		o.CallTimeout = 30 * time.Second
	}
	if o.StartupTimeout == 0 {
		o.StartupTimeout = 10 * time.Second
	}
	if o.CancelGrace == 0 {
		o.CancelGrace = time.Second
	}
	if o.ShutdownTimeout == 0 {
		o.ShutdownTimeout = 5 * time.Second
	}
	if o.MaxFrame < 256 || o.MaxFrame > 64<<20 || o.Concurrency < 1 || o.Concurrency > 4096 || o.CallTimeout < 0 || o.StartupTimeout < 0 || o.CancelGrace < 0 || o.ShutdownTimeout < 0 {
		return nil, errors.New("invalid worker bounds")
	}
	entry, err := filepath.Abs(o.Entry)
	if err != nil || o.Entry == "" {
		return nil, errors.New("entry required")
	}
	o.Entry = entry
	if len(o.Config) == 0 {
		o.Config = json.RawMessage(`{}`)
	}
	if !json.Valid(o.Config) || !utf8.Valid(o.Config) {
		return nil, errors.New("configuration is not UTF-8 JSON")
	}
	if len(o.Config) > min(o.MaxFrame, 1<<20) {
		return nil, errors.New("configuration exceeds startup frame allowance")
	}
	return &Worker{options: o, callbackSlots: make(chan struct{}, o.Concurrency)}, nil
}
func (w *Worker) Start(ctx context.Context) error {
	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	return w.start(ctx)
}
func (w *Worker) start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	if w.current != nil {
		w.mu.Unlock()
		return errors.New("worker already started; use Restart")
	}
	p := &process{owner: w, pending: map[string]*pending{}, readyCh: make(chan error, 1), exited: make(chan struct{}), writes: make(chan []byte, w.options.Concurrency*2+8), readerDone: make(chan struct{})}
	p.cmd = exec.Command(w.options.NodeBinary, "--input-type=module", "-e", runtimeSource)
	configureProcess(p.cmd)
	p.cmd.Stderr = w.options.Stderr
	// Bound os/exec's copy wait when a descendant inherits stderr. Writers must
	// still return from Write: WaitDelay cannot interrupt arbitrary user code.
	p.cmd.WaitDelay = 20 * time.Millisecond
	var err error
	p.stdin, err = p.cmd.StdinPipe()
	if err != nil {
		w.mu.Unlock()
		return err
	}
	// Own the read pipe so Cmd.Wait cannot close it before buffered final
	// frames (especially stopped) have been consumed.
	stdout, stdoutChild, err := os.Pipe()
	if err != nil {
		p.stdin.Close()
		w.mu.Unlock()
		return err
	}
	p.cmd.Stdout = stdoutChild
	if err = p.cmd.Start(); err != nil {
		p.stdin.Close()
		stdout.Close()
		stdoutChild.Close()
		w.mu.Unlock()
		return err
	}
	stdoutChild.Close()
	w.current = p
	w.last = nil
	w.mu.Unlock()
	go func() {
		err := p.wait()
		// Descendants may also inherit stdout; allow final frames to drain,
		// but do not let inherited descriptors prevent reaping completion.
		drain := time.NewTimer(20 * time.Millisecond)
		select {
		case <-p.readerDone:
		case <-drain.C:
			_ = stdout.Close()
		}
		drain.Stop()
		<-p.readerDone
		p.mu.Lock()
		ack := p.stoppedAck
		p.mu.Unlock()
		if err != nil {
			exitErr := fmt.Errorf("worker exit: %w", err)
			p.mu.Lock()
			prior := p.failure
			if prior != nil {
				p.failure = errors.Join(prior, exitErr)
			}
			p.mu.Unlock()
			if prior == nil {
				p.fail(exitErr)
			}
		} else if !ack {
			p.fail(errors.New("worker exited without stopped acknowledgement"))
		}
		close(p.exited)
	}()
	go p.writer()
	go func() {
		defer close(p.readerDone)
		defer stdout.Close()
		p.reader(stdout)
	}()
	err = p.send(map[string]any{"v": 1, "type": "init", "entry": w.options.Entry, "dataDir": w.options.DataDir, "config": w.options.Config, "maxFrame": w.options.MaxFrame, "concurrency": w.options.Concurrency, "shutdownTimeout": w.options.ShutdownTimeout.Milliseconds()})
	if err != nil {
		p.fail(err)
		return err
	}
	timer := time.NewTimer(w.options.StartupTimeout)
	defer timer.Stop()
	select {
	case err = <-p.readyCh:
		return err
	case <-ctx.Done():
		// Setup may already own durable resources. Request protocol cleanup,
		// but retain the cancellation grace for an uncooperative setup.
		cleanup, cancel := context.WithTimeout(context.Background(), w.options.CancelGrace)
		_ = w.Stop(cleanup)
		cancel()
		return ctx.Err()
	case <-timer.C:
		err = errors.New("worker startup timeout")
	}
	p.fail(err)
	return err
}
func (p *process) fail(err error) {
	p.once.Do(func() {
		p.mu.Lock()
		p.failure = err
		p.stopping = true
		for _, c := range p.pending {
			if !c.cancelled {
				c.ch <- reply{err: err}
			}
		}
		for _, c := range p.pending {
			if c.cancel != nil {
				c.cancel()
			}
			if c.settled != nil {
				close(c.settled)
			}
		}
		p.pending = map[string]*pending{}
		p.mu.Unlock()
		select {
		case p.readyCh <- err:
		default:
		}
		_ = p.stdin.Close()
		p.kill()
	})
}
func (p *process) kill() {
	p.killOnce.Do(func() { killProcess(p.cmd) })
}
func (p *process) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) == 0 || len(b) > p.owner.options.MaxFrame {
		return errors.New("worker frame exceeds bound")
	}
	if m, ok := v.(map[string]any); ok && m["type"] == "init" && len(b) > 1<<20 {
		return errors.New("init frame exceeds 1 MiB bootstrap bound")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.enqueueLocked(b)
}

// Caller holds mu, ordering admission/cancellation/shutdown in the wire queue.
func (p *process) enqueueLocked(b []byte) error {
	if p.failure != nil {
		return p.failure
	}
	select {
	case p.writes <- b:
		return nil
	default:
		return errors.New("worker write queue exhausted")
	}
}
func (p *process) writer() {
	for {
		select {
		case <-p.exited:
			return
		case b := <-p.writes:
			frame := make([]byte, 4+len(b))
			binary.BigEndian.PutUint32(frame, uint32(len(b)))
			copy(frame[4:], b)
			if _, err := p.stdin.Write(frame); err != nil {
				p.fail(fmt.Errorf("worker write: %w", err))
				return
			}
		}
	}
}
func (p *process) reader(r io.Reader) {
	for {
		var header [4]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			p.mu.Lock()
			ack := p.stoppedAck
			p.mu.Unlock()
			if !ack || !errors.Is(err, io.EOF) {
				p.fail(fmt.Errorf("worker protocol read (stopped acknowledgement required): %w", err))
			}
			return
		}
		n := binary.BigEndian.Uint32(header[:])
		if n == 0 || n > uint32(p.owner.options.MaxFrame) {
			p.fail(errors.New("invalid worker frame length"))
			return
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			p.fail(err)
			return
		}
		if err := p.receive(b); err != nil {
			p.fail(err)
			return
		}
	}
}
func (p *process) receive(b []byte) error {
	var m struct {
		V       int              `json:"v"`
		Type    string           `json:"type"`
		ID      string           `json:"id"`
		Hooks   []HookDescriptor `json:"hooks"`
		Output  json.RawMessage  `json:"output"`
		CallID  string           `json:"callId"`
		Request json.RawMessage  `json:"request"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := decodeFrame(b, &m); err != nil {
		return fmt.Errorf("malformed worker JSON: %w", err)
	}
	if m.V != 1 {
		return errors.New("unsupported worker protocol version")
	}
	if err := messageShape(b, m.Type); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failure != nil {
		return p.failure
	}
	switch m.Type {
	case "ready":
		if err := hookShapes(b); err != nil {
			return err
		}
		if p.ready || m.ID != "" || m.Hooks == nil || len(m.Hooks) > 4096 {
			return errors.New("invalid worker ready")
		}
		seen := map[string]bool{}
		for _, h := range m.Hooks {
			if !hookID.MatchString(h.ID) || !hookID.MatchString(h.Stage) || seen[h.ID] || (h.BodyMode != "none" && h.BodyMode != "stream" && h.BodyMode != "buffered") {
				return errors.New("invalid hook descriptor")
			}
			seen[h.ID] = true
		}
		p.hooks = cloneHooks(m.Hooks)
		p.ready = true
		p.readyCh <- nil
	case "call":
		return p.receiveCallLocked(m.ID, m.CallID, m.Request)
	case "result", "error":
		if !p.ready {
			return errors.New("reply before readiness")
		}
		c, ok := p.pending[m.ID]
		if !ok {
			return errors.New("unmatched worker reply")
		}
		if m.Type == "result" && len(m.Output) == 0 {
			return errors.New("missing worker output")
		}
		if m.Type == "error" && (m.Error == nil || m.Error.Code == "" || m.Error.Message == "") {
			return errors.New("invalid worker error")
		}
		delete(p.pending, m.ID)
		if c.settled != nil {
			close(c.settled)
		}
		if c.cancel != nil {
			c.cancel()
		}
		if !c.cancelled {
			if m.Type == "error" {
				c.ch <- reply{err: fmt.Errorf("worker %s: %s", m.Error.Code, m.Error.Message)}
			} else {
				c.ch <- reply{output: append(json.RawMessage(nil), m.Output...)}
			}
		}
	case "stopped":
		if !p.stopping || p.stoppedAck || len(p.pending) != 0 {
			return errors.New("unexpected stopped reply")
		}
		p.stoppedAck = true
	default:
		return errors.New("unknown worker message")
	}
	return nil
}
func (w *Worker) process() *process { w.mu.Lock(); defer w.mu.Unlock(); return w.current }
func (w *Worker) Hooks() []HookDescriptor {
	p := w.process()
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return cloneHooks(p.hooks)
}

// Invoke admits immediately or returns ErrBusy. Cancellation retains capacity until settlement or kill.
// Input/output are explicit bounded JSON units; no HTTP bodies are copied by this package.
func (w *Worker) Invoke(ctx context.Context, hook string, input json.RawMessage) (json.RawMessage, error) {
	return w.InvokeWithCallback(ctx, hook, input, nil)
}

// Callback is a bounded invocation-scoped host operation. It must honor ctx.
// Only one callback may be outstanding per invocation; no callback queue exists.
type Callback func(context.Context, json.RawMessage) (json.RawMessage, error)

func (w *Worker) InvokeWithCallback(ctx context.Context, hook string, input json.RawMessage, callback Callback) (json.RawMessage, error) {
	return w.invoke(ctx, hook, input, callback, false)
}

// InvokeSettled retains native generation accounting through real Node settlement
// (or process reaping), even when the caller has already been canceled.
func (w *Worker) InvokeSettled(ctx context.Context, hook string, input json.RawMessage, callback Callback) (json.RawMessage, error) {
	return w.invoke(ctx, hook, input, callback, true)
}
func (w *Worker) invoke(ctx context.Context, hook string, input json.RawMessage, callback Callback, settle bool) (json.RawMessage, error) {
	if len(input) > w.options.MaxFrame {
		return nil, errors.New("input exceeds worker frame bound")
	}
	if !json.Valid(input) || !utf8.Valid(input) {
		return nil, errors.New("input is not UTF-8 JSON")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, w.options.CallTimeout)
	defer cancel()
	p := w.process()
	if p == nil {
		return nil, errors.New("worker not started")
	}
	id := fmt.Sprint(w.next.Add(1))
	scopectx, scopeCancel := context.WithCancel(ctx)
	defer scopeCancel()
	c := &pending{ch: make(chan reply, 1), settled: make(chan struct{}), ctx: scopectx, cancel: scopeCancel, callback: callback}
	// Prepare outside the admission lock; registration and enqueue below are
	// one transaction, so Stop cannot overtake an admitted call.
	deadline, _ := ctx.Deadline()
	b, err := json.Marshal(map[string]any{"v": 1, "type": "invoke", "id": id, "hook": hook, "input": input, "deadline": deadline.UnixMilli(), "managed": callback != nil})
	if err != nil {
		return nil, err
	}
	if len(b) > w.options.MaxFrame {
		return nil, errors.New("worker frame exceeds bound")
	}
	p.mu.Lock()
	if !p.ready || p.stopping || p.failure != nil {
		p.mu.Unlock()
		return nil, errors.New("worker unavailable")
	}
	found := false
	for _, h := range p.hooks {
		if h.ID == hook {
			found = true
			break
		}
	}
	if !found {
		p.mu.Unlock()
		return nil, errors.New("unknown hook")
	}
	if len(p.pending) >= w.options.Concurrency {
		p.mu.Unlock()
		return nil, ErrBusy
	}
	if err := p.enqueueLocked(b); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.pending[id] = c
	p.mu.Unlock()
	if settle {
		defer func() {
			<-c.settled
			c.callbacks.Wait()
			p.mu.Lock()
			failed := p.failure != nil
			p.mu.Unlock()
			if failed {
				<-p.exited
			}
		}()
	}
	select {
	case r := <-c.ch:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return r.output, r.err
	case <-ctx.Done():
		p.cancel(id, c)
		return nil, ctx.Err()
	}
}
func (p *process) cancel(id string, c *pending) {
	p.mu.Lock()
	if p.pending[id] != c || c.cancelled {
		p.mu.Unlock()
		return
	}
	c.cancelled = true
	c.cancel()
	c.ch <- reply{err: context.Canceled}
	b, _ := json.Marshal(map[string]any{"v": 1, "type": "cancel", "id": id})
	err := p.enqueueLocked(b)
	p.mu.Unlock()
	if err != nil {
		p.fail(err)
		return
	}
	time.AfterFunc(p.owner.options.CancelGrace, func() {
		p.mu.Lock()
		still := p.pending[id] == c
		p.mu.Unlock()
		if still {
			p.fail(errors.New("worker did not settle cancellation within grace"))
		}
	})
}
func (w *Worker) Stop(ctx context.Context) error {
	w.mu.Lock()
	p := w.current
	if p == nil {
		p = w.last
	}
	w.mu.Unlock()
	if p == nil {
		return nil
	}
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.stopping = true
		// shutdown itself cancels all active runtime calls. Notify local waiters
		// without sending duplicate cancels after shutdown has been queued.
		for _, c := range p.pending {
			if !c.cancelled {
				c.cancelled = true
				if c.cancel != nil {
					c.cancel()
				}
				c.ch <- reply{err: context.Canceled}
			}
		}
		err := p.enqueueLocked([]byte(`{"v":1,"type":"shutdown"}`))
		p.mu.Unlock()
		if err != nil {
			p.fail(err)
		}
		go func() {
			timer := time.NewTimer(w.options.ShutdownTimeout)
			defer timer.Stop()
			select {
			case <-p.exited:
			case <-timer.C:
				p.fail(errors.New("worker shutdown timeout"))
			}
		}()
	})
	// Never unconditionally wait for os/exec's stderr copier after a deadline.
	timer := time.NewTimer(w.options.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-p.exited:
	case <-ctx.Done():
		p.fail(ctx.Err())
		return ctx.Err()
	case <-timer.C:
		err := errors.New("worker shutdown timeout")
		p.fail(err)
		return err
	}
	p.mu.Lock()
	err := p.failure
	p.mu.Unlock()
	w.mu.Lock()
	if w.current == p {
		w.current = nil
		w.last = p
	}
	w.mu.Unlock()
	return err
}
func (w *Worker) Restart(ctx context.Context) error {
	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	p := w.process()
	alreadyFailed := false
	if p != nil {
		p.mu.Lock()
		alreadyFailed = p.failure != nil
		p.mu.Unlock()
	} else {
		alreadyFailed = true
	}
	if err := w.Stop(ctx); err != nil && !alreadyFailed {
		return err
	}
	// A timed-out Stop can leave reaping in progress. Do not overlap processes.
	if p != nil {
		select {
		case <-p.exited:
			w.mu.Lock()
			if w.current == p {
				w.current = nil
			}
			w.mu.Unlock()
		default:
			return errors.New("worker still reaping; retry Restart after exit")
		}
	}
	return w.start(ctx)
}
