package extension

import (
	"io"
	"net/http"
	"sync"
)

func (x *exchange) hasUnits() bool {
	for _, h := range x.engine.hooks {
		if h.Stage == ResponseBody && h.Body == Unit {
			return true
		}
	}
	return false
}

// originalReader observes bounded bytes from the actual source, even when a
// later Whole modifier buffers and replaces the complete response.
type originalReader struct {
	io.ReadCloser
	x       *exchange
	request *http.Request
}

func (b *originalReader) Read(p []byte) (int, error) {
	if len(p) > b.x.engine.cfg.UnitBytes {
		p = p[:b.x.engine.cfg.UnitBytes]
	}
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		if _, e := b.x.run(OriginalBody, b.request, nil, p[:n]); e != nil {
			return 0, e
		}
	}
	return n, err
}

type bodyReader struct {
	source    io.ReadCloser
	x         *exchange
	request   *http.Request
	release   func()
	once      sync.Once
	mu        sync.Mutex
	closeOnce sync.Once
	closeErr  error
	pending   []byte
	sourceErr error
	hookErr   error
	trailer   http.Header
}

func (b *bodyReader) finish(err error) error {
	b.once.Do(func() {
		clean(b.trailer)
		b.x.terminal = err
		outcome := "read-complete"
		if err != io.EOF {
			outcome = "read-aborted"
		}
		b.x.recordHost(outcome)
		_, b.hookErr = b.x.run(Finish, b.request, nil, nil)
		b.x.captureDelivery()
		b.release()
	})
	if b.hookErr != nil {
		return b.hookErr
	}
	return err
}
func (b *bodyReader) closeSource() error {
	b.closeOnce.Do(func() { b.closeErr = b.source.Close() })
	return b.closeErr
}
func (b *bodyReader) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	if err := b.x.ctx.Err(); err != nil {
		b.closeSource()
		return 0, b.finish(err)
	}
	for len(b.pending) == 0 {
		if b.sourceErr != nil {
			return 0, b.finish(b.sourceErr)
		}
		buf := make([]byte, b.x.engine.cfg.UnitBytes)
		n, err := b.source.Read(buf)
		b.sourceErr = err
		if n > 0 {
			if !b.x.hasWhole() {
				if _, e := b.x.run(ResponseBody, b.request, nil, buf[:n]); e != nil {
					b.closeSource()
					b.sourceErr = e
					return 0, b.finish(e)
				}
				if b.x.hasUnits() {
					buf = b.x.response.Body
					n = len(buf)
				}
			}
			b.pending = buf[:n]
		}
		if n == 0 && err == nil {
			return 0, nil
		}
	}
	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}
func (b *bodyReader) Close() error {
	err := b.closeSource()
	b.mu.Lock()
	defer b.mu.Unlock()
	terminal := err
	if terminal == nil {
		terminal = b.sourceErr
	}
	if terminal == nil {
		terminal = io.ErrClosedPipe
	}
	b.finish(terminal)
	if err != nil {
		return err
	}
	return b.hookErr
}
