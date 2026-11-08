package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

type Target struct {
	Route         Route
	Tools, Stream bool
	MaxTokens     int
	Temperature   bool
}
type Affinity struct {
	Bound        Route
	ProviderHeld bool
	Revision     string
}

// Prepare requires explicit migration even for portable history. Provider-held
// state may only stay on the exact route; this slice cannot reconstruct it.
func Prepare(req Request, t Target, state Affinity, migrate bool) (Request, error) {
	if t.Route.Provider == "" || t.Route.Model == "" || t.Route.Account == "" || t.Route.Endpoint == "" || t.Route.Version == "" {
		return Request{}, errors.New("incomplete target scope")
	}
	if t.Route.Protocol != Chat && t.Route.Protocol != Messages {
		return Request{}, ErrUnsupported
	}
	if state.Bound != (Route{}) && state.Bound != t.Route {
		if state.ProviderHeld || !migrate {
			return Request{}, fmt.Errorf("%w: affinity migration", ErrUnsupported)
		}
	}
	if len(req.Tools) > 0 && !t.Tools || req.Stream && !t.Stream || req.Settings.Temperature != nil && !t.Temperature {
		return Request{}, fmt.Errorf("%w: target capabilities", ErrUnsupported)
	}
	if req.Settings.MaxTokens <= 0 || t.MaxTokens > 0 && req.Settings.MaxTokens > t.MaxTokens {
		return Request{}, errors.New("unsupported max token setting")
	}
	if _, err := json.Marshal(req); err != nil {
		return Request{}, err
	}
	req = clone(req)
	req.Target = t.Route
	if _, err := (Adapter{t.Route.Protocol}).EncodeRequest(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

// CacheKey is available only after resolution. No unknown tenant/account/state
// identity is treated as a shareable cache scope. No cache storage is provided.
func CacheKey(req Request, state Affinity) (string, error) {
	if req.Identity.Tenant == "" || req.Identity.Conversation == "" || req.Target.Account == "" || req.Target.Endpoint == "" || req.Target.Version == "" || state.Revision == "" {
		return "", errors.New("unknown cache scope")
	}
	req.Lineage = Lineage{}
	req.Purpose = ""
	b, err := json.Marshal(struct {
		Request Request
		State   Affinity
	}{req, state})
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// Candidates selects the first accepted complete result and cancels all losers.
// It never executes tools. All calls remain charged until their functions end.
// The caller must authorize the selected result before passing it to tools.
func Candidates(ctx context.Context, s *Session, reqs []Request, accept func(Result) bool) (Result, error) {
	if len(reqs) == 0 || accept == nil {
		return Result{}, errors.New("missing candidates/selector")
	}
	l := s.Limits.defaults()
	if l.Parallel > 0 && len(reqs) > l.Parallel {
		return Result{}, ErrLimit
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct {
		r Result
		e error
	}
	ch := make(chan outcome, len(reqs))
	for _, r := range reqs {
		if _, err := json.Marshal(r); err != nil {
			return Result{}, err
		}
	}
	for _, r := range reqs {
		r = clone(r)
		r.Purpose = "candidate"
		go func(r Request) { v, e := s.Call(ctx, r); ch <- outcome{v, e} }(r)
	}
	var chosen Result
	found := false
	var failures []error
	for range reqs {
		o := <-ch
		if o.e != nil {
			failures = append(failures, o.e)
			continue
		}
		if !found && accept(clone(o.r)) {
			chosen = o.r
			found = true
			cancel()
		}
	}
	if found {
		return chosen, nil
	}
	return Result{}, fmt.Errorf("no accepted candidate: %w", errors.Join(failures...))
}

// Classify asks a managed model for an exact allowlisted label. It cannot inject
// an arbitrary model, endpoint, account or credential into the selected route.
func Classify(ctx context.Context, s *Session, classifier Request, targets map[string]Target) (Target, error) {
	if _, err := json.Marshal(classifier); err != nil {
		return Target{}, err
	}
	classifier = clone(classifier)
	classifier.Purpose = "classifier"
	classifier.Tools = nil
	classifier.Stream = false
	r, err := s.Call(ctx, classifier)
	if err != nil {
		return Target{}, err
	}
	if len(r.Response.Calls) > 0 || len(r.Response.Opaque) > 0 {
		return Target{}, ErrUnsupported
	}
	t, ok := targets[r.Response.Text]
	if !ok {
		return Target{}, errors.New("classifier returned unknown label")
	}
	return t, nil
}

// Reroute is precommit only. The host must provide the authoritative committed
// state and withhold final output/early hints throughout buffered selection.
func Reroute(ctx context.Context, s *Session, req Request, t Target, state Affinity, migrate, committed bool) (Result, error) {
	if committed {
		return Result{}, errors.New("cannot reroute committed delivery")
	}
	r, err := Prepare(req, t, state, migrate)
	if err != nil {
		return Result{}, err
	}
	r.Purpose = "reroute"
	return s.Call(ctx, r)
}
