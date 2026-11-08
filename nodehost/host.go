package nodehost

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/worker"
)

var dependencyID = regexp.MustCompile(`^(?:[a-z][a-z0-9-]{0,63}/)?[a-zA-Z][a-zA-Z0-9_.-]{0,127}$`)

var stageNames = []string{"request", "resolve", "upstream", "observe", "responseStart", "originalBody", "responseBody", "finish", "delivery"}
var stages = func() map[string]extension.Stage {
	m := map[string]extension.Stage{}
	for i, s := range stageNames {
		m[s] = extension.Stage(i)
	}
	return m
}()

type Options struct {
	Store   string
	Gateway string         // exact ordinary HTTP(S) gateway origin, never an upstream
	Worker  worker.Options // Entry is installation-owned; other settings are explicit host policy
	Engine  extension.Config
}
type Host struct {
	Engine               *extension.Engine
	workers              []*worker.Worker
	gateway              string
	bodyLimit, unitLimit int
	mu                   sync.Mutex
	streams              map[string]uint64
}
type installedHook struct {
	owner      string
	descriptor worker.HookDescriptor
	worker     *worker.Worker
}

// Open validates every installation and descriptor before returning an immutable engine.
// Any startup failure rolls back all launched processes. No package is silently omitted.
func Open(ctx context.Context, o Options) (_ *Host, err error) {
	records, err := (extensions.Installer{Store: o.Store}).List()
	if err != nil {
		return nil, err
	}
	if len(records) > 64 {
		return nil, errors.New("nodehost: at most 64 installed workers")
	}
	u, e := url.Parse(o.Gateway)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("nodehost: gateway must be exact HTTP(S) origin")
	}
	if o.Engine.BodyBytes == 0 {
		o.Engine.BodyBytes = 256 << 10
	}
	if o.Engine.UnitBytes == 0 {
		o.Engine.UnitBytes = 32 << 10
	}
	if o.Engine.BodyBytes < 1 || o.Engine.BodyBytes > 16<<20 || o.Engine.UnitBytes < 1 || o.Engine.UnitBytes > o.Engine.BodyBytes {
		return nil, errors.New("nodehost: finite body/unit limits required (body at most 16 MiB)")
	}
	h := &Host{gateway: strings.TrimSuffix(o.Gateway, "/"), bodyLimit: o.Engine.BodyBytes, unitLimit: o.Engine.UnitBytes, streams: map[string]uint64{}}
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err = errors.Join(err, h.Close(cleanup))
		}
	}()
	var hooks []installedHook
	for _, record := range records {
		options := o.Worker
		options.Entry = filepath.Join(o.Store, record.Manifest.ID, "package", filepath.FromSlash(record.Manifest.Entry))
		w, e := worker.New(options)
		if e != nil {
			return nil, e
		}
		h.workers = append(h.workers, w)
		if e = w.Start(ctx); e != nil {
			return nil, fmt.Errorf("start %s: %w", record.Manifest.ID, e)
		}
		for _, d := range w.Hooks() {
			hooks = append(hooks, installedHook{record.Manifest.ID, d, w})
			if len(hooks) > 512 {
				return nil, errors.New("nodehost: at most 512 installed hooks")
			}
		}
	}
	regs, e := h.registrations(hooks)
	if e != nil {
		return nil, e
	}
	h.Engine, err = extension.New(o.Engine, regs...)
	if err != nil {
		return nil, err
	}
	return h, nil
}
func global(owner, id string) string {
	if strings.Contains(id, "/") {
		return id
	}
	return owner + "/" + id
}
func (h *Host) registrations(hooks []installedHook) ([]extension.Registration, error) {
	sort.Slice(hooks, func(i, j int) bool {
		a, b := hooks[i], hooks[j]
		if stages[a.descriptor.Stage] != stages[b.descriptor.Stage] {
			return stages[a.descriptor.Stage] < stages[b.descriptor.Stage]
		}
		if a.descriptor.Order != b.descriptor.Order {
			return a.descriptor.Order < b.descriptor.Order
		}
		return global(a.owner, a.descriptor.ID) < global(b.owner, b.descriptor.ID)
	})
	if len(hooks) > 512 {
		return nil, errors.New("nodehost: at most 512 installed hooks")
	}
	regs := make([]extension.Registration, 0, len(hooks)+1)
	for i, item := range hooks {
		d := item.descriptor
		stage, ok := stages[d.Stage]
		if !ok {
			return nil, fmt.Errorf("unsupported stage %q", d.Stage)
		}
		r := extension.Registration{Owner: item.owner, ID: global(item.owner, d.ID), Stage: stage, Formats: append([]string(nil), d.Formats...)}
		if len(r.ID) > 128 {
			return nil, errors.New("namespaced hook ID exceeds 128 bytes")
		}
		switch d.BodyMode {
		case "none":
			r.Body = extension.NoBody
		case "buffered":
			r.Body = extension.Whole
		case "stream":
			r.Body = extension.Unit
		default:
			return nil, extension.ErrUnsupported
		}
		if d.Failure == "failOpen" {
			r.Failure = extension.FailOpen
		} else if d.Failure != "" && d.Failure != "block" {
			return nil, errors.New("invalid failure policy")
		}
		for _, f := range r.Formats {
			parsed, params, err := mime.ParseMediaType(f)
			if err != nil || len(params) != 0 || parsed != f || !strings.Contains(f, "/") {
				return nil, fmt.Errorf("invalid MIME format %q", f)
			}
		}
		lists := []struct {
			dst *[]string
			src []string
		}{{&r.Requires, d.Requires}, {&r.Before, d.Before}, {&r.After, d.After}}
		for _, list := range lists {
			for _, dep := range list.src {
				if !dependencyID.MatchString(dep) || len(global(item.owner, dep)) > 128 {
					return nil, errors.New("invalid dependency ID")
				}
				*list.dst = append(*list.dst, global(item.owner, dep))
			}
		}
		// Numeric priority is a hard same-stage edge. Equal priorities use native
		// dependency ordering, then stable global ID as the ready-node tie-breaker.
		for _, previous := range hooks[:i] {
			if previous.descriptor.Stage == d.Stage && previous.descriptor.Order < d.Order {
				r.After = append(r.After, global(previous.owner, previous.descriptor.ID))
			}
		}
		r.Handler = h.handler(item)
		regs = append(regs, r)
	}
	needsRetirement := false
	for _, item := range hooks {
		needsRetirement = needsRetirement || item.descriptor.BodyMode == "stream"
	}
	if !needsRetirement {
		return regs, nil
	}
	// Run stream retirement before user Finish hooks (even a required Finish failure).
	retire := extension.Registration{Owner: "@nodehost", ID: "@nodehost.streams", Stage: extension.Finish, Failure: extension.FailOpen, Handler: func(_ context.Context, in extension.Input) (extension.Result, error) {
		h.mu.Lock()
		prefix := in.Identity.Exchange + "/"
		for key := range h.streams {
			if strings.HasPrefix(key, prefix) {
				delete(h.streams, key)
			}
		}
		h.mu.Unlock()
		return extension.Result{}, nil
	}}
	for _, r := range regs {
		if r.Stage == extension.Finish {
			retire.Before = append(retire.Before, r.ID)
		}
	}
	return append(regs, retire), nil
}
func (h *Host) handler(item installedHook) extension.Handler {
	return func(ctx context.Context, in extension.Input) (extension.Result, error) {
		d := item.descriptor
		wire := snapshot(in, d)
		if d.BodyMode == "stream" {
			key := fmt.Sprintf("%s/%d/%s/%s", in.Identity.Exchange, in.Identity.Attempt, global(item.owner, d.ID), d.Stage)
			h.mu.Lock()
			seq, exists := h.streams[key]
			if (!exists && len(h.streams) >= 1024) || seq >= 1<<53-1 {
				h.mu.Unlock()
				return extension.Result{}, extension.ErrLimit
			}
			h.streams[key] = seq + 1
			h.mu.Unlock()
			wire.Response.Body = nil
			wire.Stream = &Stream{fmt.Sprintf("%x", sha256.Sum256([]byte(key))), seq, base64.StdEncoding.EncodeToString(in.Response.Body), false}
		}
		b, err := json.Marshal(wire)
		if err != nil {
			return extension.Result{}, err
		}
		var callback worker.Callback
		if stages[d.Stage] != extension.Delivery {
			callback = h.managed
		}
		out, err := item.worker.InvokeWithCallback(ctx, d.ID, b, callback)
		if err != nil {
			return extension.Result{}, err
		}
		return result(out, d, h.bodyLimit, h.unitLimit)
	}
}

// Close stops native admission/drains first, then stops workers. Every worker gets
// a stop even if another reports failure. Caller supplies a finite drain deadline.
func (h *Host) Close(ctx context.Context) error {
	var err error
	if h.Engine != nil {
		err = h.Engine.Shutdown(ctx)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, w := range h.workers {
		wg.Add(1)
		go func() { defer wg.Done(); e := w.Stop(ctx); mu.Lock(); err = errors.Join(err, e); mu.Unlock() }()
	}
	wg.Wait()
	return err
}
