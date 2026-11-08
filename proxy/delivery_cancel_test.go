package proxy

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extension"
)

// Cancellation can race a downstream write after source consumption. The final
// boundary observation must not reuse the canceled reader context.
func TestDeliveryAfterRequestCancellation(t *testing.T) {
	var evidence *extension.DeliveryEvidence
	e := executionEngine(t, extension.Config{}, hook("delivery", "recorder", extension.Delivery, func(ctx context.Context, in extension.Input) (extension.Result, error) {
		if ctx.Err() != nil {
			t.Error("delivery invoked with canceled reader context")
		}
		evidence = in.Delivery
		return extension.Result{}, nil
	}))
	p, _ := NewWithEngine("http://example.test", e)
	p.ErrorLog = log.New(io.Discard, "", 0)
	p.Transport = e.Transport(deliveryTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: -1, Body: io.NopCloser(strings.NewReader("body"))}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "http://example.test/", nil).WithContext(context.WithValue(ctx, http.ServerContextKey, &http.Server{}))
	w := &cancelWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	func() {
		defer func() {
			if p := recover(); p != nil && p != http.ErrAbortHandler {
				t.Fatalf("unexpected panic %v", p)
			}
		}()
		p.ServeHTTP(w, r)
	}()
	if evidence == nil || evidence.Bytes != 4 {
		t.Fatalf("lost boundary evidence after cancellation: %+v", evidence)
	}
}

type cancelWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *cancelWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.cancel()
	return n, err
}
