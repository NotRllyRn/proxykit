// Package recall demonstrates a single-host, local-filesystem recall store.
// It is an application example, not a database or a distributed storage API.
package recall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/NotRllyRn/proxykit/ai"
)

var ErrConflict = errors.New("recall revision conflict")
var ErrUncertain = errors.New("recall execution uncertain; operator reconciliation required")

type Key struct{ Tenant, Conversation string }
type State struct {
	Revision          uint64
	Owner             string
	Identity          Key
	Archive           [][]ai.Message
	Original, Working []ai.Message
	Turn              *ai.Turn
	Affinity          ai.Affinity
	Budget            *ai.Budget
	InFlight          bool // durable reservation; never automatically retried after crash/error
}
type Store struct {
	Dir      string
	MaxBytes int64
}

func (s Store) path(k Key) (string, error) {
	if k.Tenant == "" || k.Conversation == "" {
		return "", errors.New("explicit tenant and conversation required")
	}
	b, _ := json.Marshal(k)
	h := sha256.Sum256(b)
	return filepath.Join(s.Dir, hex.EncodeToString(h[:])+".json"), nil
}
func (s Store) Read(ctx context.Context, k Key) (State, error) {
	p, err := s.path(k)
	if err != nil {
		return State{}, err
	}
	if err = ctx.Err(); err != nil {
		return State{}, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return State{Identity: k}, nil
	}
	if err != nil {
		return State{}, err
	}
	defer f.Close()
	max := s.MaxBytes
	if max <= 0 {
		max = 8 << 20
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return State{}, err
	}
	if int64(len(b)) > max {
		return State{}, ai.ErrLimit
	}
	var v State
	if err = json.Unmarshal(b, &v); err != nil {
		return State{}, err
	}
	if v.Identity != k {
		return State{}, errors.New("stored identity mismatch")
	}
	return v, nil
}

// update holds an advisory process lock, verifies CAS and durably replaces one
// file. The private directory and filesystem must support flock, atomic rename,
// file fsync and directory fsync. Do not use NFS or multiple hosts.
func (s Store) update(ctx context.Context, k Key, expected uint64, fn func(*State) error) (State, error) {
	return s.updateReserved(ctx, k, expected, false, fn)
}
func (s Store) updateReserved(ctx context.Context, k Key, expected uint64, reserved bool, fn func(*State) error) (State, error) {
	p, err := s.path(k)
	if err != nil {
		return State{}, err
	}
	if err = os.MkdirAll(s.Dir, 0700); err != nil {
		return State{}, err
	}
	unlock, err := lock(ctx, p+".lock")
	if err != nil {
		return State{}, err
	}
	defer unlock()
	st, err := s.Read(ctx, k)
	if err != nil {
		return State{}, err
	}
	if st.Revision != expected {
		return State{}, ErrConflict
	}
	if st.InFlight && !reserved {
		return State{}, ErrUncertain
	}
	if st.Revision == ^uint64(0) {
		return State{}, errors.New("revision exhausted")
	}
	if err = fn(&st); err != nil {
		return State{}, err
	}
	st.Revision++
	b, err := json.Marshal(st)
	if err != nil {
		return State{}, err
	}
	max := s.MaxBytes
	if max <= 0 {
		max = 8 << 20
	}
	if int64(len(b)) > max {
		return State{}, ai.ErrLimit
	}
	if err = ctx.Err(); err != nil {
		return State{}, err
	}
	f, err := os.CreateTemp(s.Dir, ".recall-*")
	if err != nil {
		return State{}, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return State{}, err
	}
	if err = ctx.Err(); err != nil {
		return State{}, err
	}
	if err = os.Rename(tmp, p); err != nil {
		return State{}, err
	}
	dir, err := os.Open(s.Dir)
	if err != nil {
		return State{}, fmt.Errorf("commit uncertain: %w", err)
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return State{}, fmt.Errorf("commit uncertain: %w", err)
	}
	return st, nil
}

// Archive commits originals before any reducer is invoked. After failure or
// restart Working still contains the complete original input.
func (s Store) Archive(ctx context.Context, k Key, rev uint64, owner string, original []ai.Message, affinity ai.Affinity) (State, error) {
	if owner == "" {
		return State{}, errors.New("missing compactor owner")
	}
	if err := ai.ValidateMessages(original); err != nil {
		return State{}, err
	}
	original = copyJSON(original)
	return s.update(ctx, k, rev, func(st *State) error {
		if st.Owner != "" && st.Owner != owner {
			return errors.New("another compactor owns revision")
		}
		st.Owner = owner
		st.Archive = append(st.Archive, original)
		st.Original = original
		st.Working = original
		st.Affinity = affinity
		st.Turn = nil
		return nil
	})
}

// Reduce uses only an already archived revision. Provider-held or signed state
// cannot be compacted by this text-only example. CAS rejects competing reducers.
func (s Store) Reduce(ctx context.Context, k Key, rev uint64, owner string, reducer func([]ai.Message) ([]ai.Message, error)) (State, error) {
	if reducer == nil {
		return State{}, errors.New("missing reducer")
	}
	return s.update(ctx, k, rev, func(st *State) error {
		if st.Owner != owner || len(st.Archive) == 0 {
			return errors.New("no owned archive")
		}
		if st.Affinity.ProviderHeld {
			return ai.ErrUnsupported
		}
		ms, err := reducer(copyJSON(st.Working))
		if err != nil {
			return err
		}
		if err = ai.ValidateMessages(ms); err != nil {
			return err
		}
		st.Working = copyJSON(ms)
		return nil
	})
}

// SaveTurn checkpoints quiescent state only; use Run to reserve before effects.
func (s Store) SaveTurn(ctx context.Context, k Key, rev uint64, t ai.Turn, budget ai.Budget) (State, error) {
	if t.Identity.Tenant != k.Tenant || t.Identity.Conversation != k.Conversation {
		return State{}, errors.New("turn identity mismatch")
	}
	if _, err := json.Marshal(t); err != nil {
		return State{}, err
	}
	t = copyJSON(t)
	return s.update(ctx, k, rev, func(st *State) error { st.Turn = &t; st.Working = t.Working; st.Budget = &budget; return nil })
}

// Continue serializes CAS/replay admission across processes before invoking the
// coordinator, so losing revisions do not run tools. A crash during an external
// effect is still ambiguous: tools must implement their own idempotency using
// tenant/conversation plus ai.ToolCallID. No exactly-once effect claim is made.
func (s Store) Continue(ctx context.Context, k Key, rev uint64, c ai.Coordinator, req ai.Request, replay []ai.Message) (State, error) {
	return s.Run(ctx, k, rev, func(st State) (ai.Turn, ai.Budget, error) {
		if st.Turn == nil || c.Session == nil {
			return ai.Turn{}, ai.Budget{}, errors.New("no pending continuation/session")
		}
		if st.Budget == nil {
			return ai.Turn{}, ai.Budget{}, errors.New("missing authoritative continuation budget")
		}
		if err := c.Session.RestoreBudget(*st.Budget); err != nil {
			return ai.Turn{}, ai.Budget{}, err
		}
		if !st.Budget.Deadline.IsZero() {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, st.Budget.Deadline)
			defer cancel()
		}
		turn, err := c.Resume(ctx, req, *st.Turn, replay)
		if err != nil {
			return turn, ai.Budget{}, err
		}
		budget, err := c.Session.Budget()
		budget.Deadline = st.Budget.Deadline
		return turn, budget, err
	})
}

// Run reserves the revision durably BEFORE running any inference or effects.
// Concurrent/stale requests cannot pass admission. A callback error, cancellation,
// persistence failure or process crash leaves a poisoned reservation: explicit
// operator reconciliation is safer than resetting limits or repeating effects.
// The callback may restore st.Budget into a fresh Session. Archive/Reduce must
// happen before Run; mutation is blocked while execution is reserved.
func (s Store) Run(ctx context.Context, k Key, rev uint64, fn func(State) (ai.Turn, ai.Budget, error)) (State, error) {
	if fn == nil {
		return State{}, errors.New("missing execution")
	}
	reserved, err := s.update(ctx, k, rev, func(st *State) error { st.InFlight = true; return nil })
	if err != nil {
		return State{}, err
	}
	turn, budget, err := fn(copyJSON(reserved))
	if err != nil {
		return reserved, err
	}
	if turn.Identity.Tenant != k.Tenant || turn.Identity.Conversation != k.Conversation {
		return reserved, errors.New("turn identity mismatch")
	}
	return s.updateReserved(ctx, k, reserved.Revision, true, func(st *State) error {
		if !st.InFlight {
			return ErrConflict
		}
		st.InFlight = false
		st.Turn = &turn
		st.Working = turn.Working
		st.Budget = &budget
		st.Affinity.Bound = turn.Served
		return nil
	})
}
func copyJSON[T any](v T) T {
	b, _ := json.Marshal(v)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
