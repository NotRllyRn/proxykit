package proxy

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/NotRllyRn/proxykit/extension"
)

// Unwrap keeps ResponseController full duplex/deadline/flush support intact.
// No ReaderFrom: io.Copy must pass through Write for honest accepted-byte counts.
type deliveryWriter struct {
	http.ResponseWriter
	mu                    sync.Mutex
	evidence              extension.DeliveryEvidence
	suppressInformational bool
}

func (w *deliveryWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *deliveryWriter) WriteHeader(status int) {
	if w.suppressInformational && status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		return
	}
	w.ResponseWriter.WriteHeader(status)
	if status >= 200 {
		w.mu.Lock()
		if w.evidence.Status == 0 {
			w.evidence.Status = status
		}
		w.mu.Unlock()
	}
}
func (w *deliveryWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.mu.Lock()
	if w.evidence.Status == 0 {
		w.evidence.Status = http.StatusOK
	}
	w.evidence.Bytes += int64(n)
	if w.evidence.WriteError == nil {
		w.evidence.WriteError = err
		if err == nil && n < len(b) {
			w.evidence.WriteError = io.ErrShortWrite
		}
	}
	w.mu.Unlock()
	return n, err
}

// Flush preserves the conventional Flusher surface as well as the controller's
// error-returning form. Unsupported underlying flushes remain explicit evidence.
func (w *deliveryWriter) Flush() { _ = w.FlushError() }
func (w *deliveryWriter) FlushError() error {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	w.mu.Lock()
	if err == nil && w.evidence.Status == 0 {
		w.evidence.Status = http.StatusOK
	}
	if err != nil && w.evidence.FlushError == nil {
		w.evidence.FlushError = err
	}
	w.mu.Unlock()
	return err
}
func (w *deliveryWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, b, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.mu.Lock()
		w.evidence.Hijacked = true
		w.mu.Unlock()
	}
	return c, b, err
}
func (w *deliveryWriter) result(returned bool) extension.DeliveryEvidence {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := w.evidence
	d.Returned, d.Aborted = returned, !returned
	return d
}

// Preserve the legacy ReverseProxy CloseNotifier fallback only if supported.
type notifyingDeliveryWriter struct {
	*deliveryWriter
	http.CloseNotifier
}
