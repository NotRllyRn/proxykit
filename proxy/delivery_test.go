package proxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extension"
)

type deliveryTransport func(*http.Request) (*http.Response, error)

func (f deliveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type boundaryWriter struct {
	header  http.Header
	limit   int
	err     error
	status  int
	bytes   int
	flushed bool
	duplex  bool
}

func (w *boundaryWriter) Header() http.Header { return w.header }
func (w *boundaryWriter) WriteHeader(s int)   { w.status = s }
func (w *boundaryWriter) Write(b []byte) (int, error) {
	n := len(b)
	if w.limit >= 0 && n > w.limit {
		n = w.limit
	}
	w.bytes += n
	return n, w.err
}
func (w *boundaryWriter) Flush()                  { w.flushed = true }
func (w *boundaryWriter) EnableFullDuplex() error { w.duplex = true; return nil }

func TestDeliveryBoundaryCountsErrorsAndAbort(t *testing.T) {
	writeErr := errors.New("client write failed")
	readErr := errors.New("upstream read failed")
	for _, tc := range []struct {
		name                      string
		limit                     int
		writeErr, errorFromSource error
		abort                     bool
	}{
		{name: "success", limit: -1},
		{name: "partial-write", limit: 2, writeErr: writeErr, abort: true},
		{name: "short-write", limit: 2, abort: true},
		{name: "read-abort", limit: -1, errorFromSource: readErr, abort: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []extension.Input
			var finishes int
			e := executionEngine(t, extension.Config{},
				hook("finish", "recorder", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
					finishes++
					return extension.Result{}, nil
				}),
				hook("delivery", "recorder", extension.Delivery, func(_ context.Context, in extension.Input) (extension.Result, error) {
					events = append(events, in)
					return extension.Result{}, nil
				}),
			)
			p, err := NewWithEngine("http://example.test", e)
			if err != nil {
				t.Fatal(err)
			}
			p.ErrorLog = log.New(io.Discard, "", 0)
			// A Whole reader is unnecessary: a source returning bytes and EOF already
			// demonstrates that reader completion need not imply accepted client writes.
			p.Transport = e.Transport(deliveryTransport(func(r *http.Request) (*http.Response, error) {
				terminal := error(io.EOF)
				if tc.errorFromSource != nil {
					terminal = tc.errorFromSource
				}
				return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"text/plain"}, "Trailer": {"X-End"}}, Trailer: http.Header{"X-End": {"done"}}, Body: io.NopCloser(&endReader{data: []byte("abcdef"), err: terminal}), ContentLength: -1}, nil
			}))
			r := httptest.NewRequest("GET", "http://example.test/", nil)
			r = r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, &http.Server{}))
			w := &boundaryWriter{header: make(http.Header), limit: tc.limit, err: tc.writeErr}
			var recovered any
			func() { defer func() { recovered = recover() }(); p.ServeHTTP(w, r) }()
			if tc.abort && recovered != http.ErrAbortHandler {
				t.Fatalf("abort %v", recovered)
			}
			if !tc.abort && recovered != nil {
				t.Fatal(recovered)
			}
			if finishes != 1 || len(events) != 1 {
				t.Fatalf("finish %d delivery %d", finishes, len(events))
			}
			in := events[0]
			d := in.Delivery
			if d == nil || d.Status != 201 || d.Bytes != int64(w.bytes) || d.Aborted != tc.abort || d.Returned == tc.abort || d.Hijacked {
				t.Fatalf("evidence %+v writer %+v", d, w)
			}
			if tc.writeErr != nil && !errors.Is(d.WriteError, tc.writeErr) {
				t.Fatalf("write error %v", d.WriteError)
			}
			if tc.name == "short-write" && !errors.Is(d.WriteError, io.ErrShortWrite) {
				t.Fatalf("short write %v", d.WriteError)
			}
			if tc.errorFromSource != nil && !errors.Is(d.ReadError, tc.errorFromSource) {
				t.Fatalf("read error %v", d.ReadError)
			}
			if tc.errorFromSource == nil && !errors.Is(d.ReadError, io.EOF) {
				t.Fatalf("reader should be complete independently of delivery: %v", d.ReadError)
			}
			if !w.duplex {
				t.Fatal("ResponseController full duplex lost")
			}
			if !tc.abort && (!w.flushed || w.header.Get("X-End") != "done") {
				t.Fatalf("flush/trailer lost %+v", w)
			}
			if in.Call != nil || len(in.Response.Body) != 0 {
				t.Fatal("terminal delivery exposes work/body")
			}
		})
	}
}

type endReader struct {
	data []byte
	err  error
}

func (r *endReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.err
	}
	return n, nil
}

func TestDeliveryOnlyExternalRootNotConsumedChild(t *testing.T) {
	var delivered []extension.Input
	var finished []extension.Identity
	e := executionEngine(t, extension.Config{},
		hook("child", "router", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
			if in.Identity.Depth > 0 {
				return extension.Result{}, nil
			}
			r, _ := http.NewRequest("GET", "http://example.test/child", nil)
			return extension.Result{Action: extension.Child, Request: r}, nil
		}),
		hook("finish", "recorder", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
			finished = append(finished, in.Identity)
			return extension.Result{}, nil
		}),
		hook("delivery", "recorder", extension.Delivery, func(_ context.Context, in extension.Input) (extension.Result, error) {
			delivered = append(delivered, in)
			return extension.Result{}, nil
		}),
	)
	p, _ := NewWithEngine("http://example.test", e)
	p.Transport = e.Transport(deliveryTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(r.URL.Path)), ContentLength: -1}, nil
	}))
	// A plain transport invocation is consumption, not boundary delivery.
	resp, err := p.Transport.RoundTrip(httptest.NewRequest("GET", "http://example.test/", nil))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if len(delivered) != 0 {
		t.Fatal("transport consumption labeled delivery")
	}
	finished = nil
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", "http://example.test/", nil))
	if w.Body.String() != "/child" || len(finished) != 2 || len(delivered) != 1 {
		t.Fatalf("body %s finish %d delivery %d", w.Body.String(), len(finished), len(delivered))
	}
	if delivered[0].Identity.Depth != 0 || len(delivered[0].Provenance) != 2 || delivered[0].Delivery.Bytes != 6 {
		t.Fatalf("wrong delivery attribution %+v", delivered[0])
	}
}

func TestDeliveryLocalAndRejectedUpgrade(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade-rejected", true: "local"}[local], func(t *testing.T) {
			var event extension.Input
			var originals int
			original := hook("source", "recorder", extension.OriginalBody, func(context.Context, extension.Input) (extension.Result, error) {
				originals++
				return extension.Result{}, nil
			})
			original.Body = extension.Unit
			rs := []extension.Registration{hook("delivery", "recorder", extension.Delivery, func(_ context.Context, in extension.Input) (extension.Result, error) {
				event = in
				return extension.Result{}, nil
			})}
			if local {
				rs = append(rs, original, hook("local", "cache", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
					return extension.Result{Action: extension.Respond, Message: &extension.Message{Status: 202, Header: make(http.Header), Body: []byte("cached")}}, nil
				}))
			}
			e := executionEngine(t, extension.Config{}, rs...)
			p, _ := NewWithEngine("http://example.test", e)
			p.ErrorLog = log.New(io.Discard, "", 0)
			p.Transport = e.Transport(deliveryTransport(func(*http.Request) (*http.Response, error) {
				if local {
					t.Error("local response dispatched")
				}
				return &http.Response{StatusCode: 101, Header: http.Header{"Connection": {"Upgrade"}, "Upgrade": {"example"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			}))
			w := httptest.NewRecorder()
			p.ServeHTTP(w, httptest.NewRequest("GET", "http://example.test/", nil))
			if event.Delivery == nil || event.Delivery.Status != w.Code || event.Delivery.Bytes != int64(w.Body.Len()) || !event.Delivery.Returned {
				t.Fatalf("boundary event %+v body %s", event.Delivery, w.Body.String())
			}
			if local {
				if w.Code != 202 || w.Body.String() != "cached" || originals != 0 {
					t.Fatal("local delivery/source distinction", w.Code, w.Body.String(), originals)
				}
			} else if w.Code != 502 || !errors.Is(event.Delivery.ReadError, extension.ErrUnsupported) {
				t.Fatalf("upgrade delivery must reject rather than claim tunnel accounting: %d %+v", w.Code, event.Delivery)
			}
		})
	}
}
