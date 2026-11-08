package worker

import "errors"

func (w *Worker) Reaped() bool {
	w.mu.Lock()
	p := w.current
	if p == nil {
		p = w.last
	}
	w.mu.Unlock()
	if p == nil {
		return true
	}
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

// Health never restarts a failed process. Required hooks continue to fail closed.
func (w *Worker) Health() error {
	p := w.process()
	if p == nil {
		return errors.New("worker not running")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failure != nil {
		return p.failure
	}
	if p.stopping || !p.ready {
		return errors.New("worker stopping or not ready")
	}
	return nil
}
