package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extension"
)

func TestInformationalSuppressionIsExplicitAndManaged(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", "</"+strings.TrimPrefix(r.URL.Path, "/")+">; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Del("Link")
		fmt.Fprint(w, r.URL.Path)
	}))
	defer upstream.Close()
	for _, tc := range []struct {
		name              string
		managed, suppress bool
		hints             int
	}{
		{"ordinary", false, false, 1},
		{"ordinary-option-ignored", false, true, 1},
		{"managed-default", true, false, 2},
		{"managed-suppressed", true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p *Proxy
			var err error
			if tc.managed {
				e := executionEngine(t, extension.Config{}, hook("choose", "example", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
					if in.Identity.Depth > 0 {
						return extension.Result{}, nil
					}
					r, _ := http.NewRequest("GET", upstream.URL+"/selected", nil)
					return extension.Result{Action: extension.Child, Request: r}, nil
				}))
				p, err = NewWithEngine(upstream.URL, e)
			} else {
				p, err = New(upstream.URL)
			}
			if err != nil {
				t.Fatal(err)
			}
			p.SuppressInformationalResponses = tc.suppress
			defer p.Transport.(interface{ CloseIdleConnections() }).CloseIdleConnections()
			server := httptest.NewServer(p)
			defer server.Close()
			var hints []string
			r, _ := http.NewRequest("GET", server.URL+"/discarded", nil)
			r = r.WithContext(httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{Got1xxResponse: func(code int, h textproto.MIMEHeader) error {
				if code != 103 {
					t.Errorf("informational code %d", code)
				}
				hints = append(hints, h.Get("Link"))
				return nil
			}}))
			resp, err := server.Client().Do(r)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if len(hints) != tc.hints {
				t.Fatalf("hints %v, want %d", hints, tc.hints)
			}
			want := "/discarded"
			if tc.managed {
				want = "/selected"
			}
			if string(b) != want || resp.StatusCode != 200 || resp.Header.Get("Link") != "" {
				t.Fatalf("final status=%d headers=%v body=%q", resp.StatusCode, resp.Header, b)
			}
		})
	}
}

func TestSuppressionDoesNotSuppressUpgrade(t *testing.T) {
	w := httptest.NewRecorder()
	d := &deliveryWriter{ResponseWriter: w, suppressInformational: true}
	d.WriteHeader(103)
	d.WriteHeader(101)
	if w.Code != 101 {
		t.Fatalf("upgrade status %d", w.Code)
	}
}

func TestNativeHookErrorNotReturnedInHTTP(t *testing.T) {
	const secret = "fake-credential-http-93bc"
	for _, panics := range []bool{false, true} {
		t.Run(fmt.Sprint(panics), func(t *testing.T) {
			e := executionEngine(t, extension.Config{}, hook("native", "example", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
				if panics {
					panic(secret)
				}
				return extension.Result{}, fmt.Errorf("credential=%s", secret)
			}))
			p, err := NewWithEngine("http://example.test", e)
			if err != nil {
				t.Fatal(err)
			}
			p.ErrorLog = log.New(io.Discard, "", 0)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, httptest.NewRequest("GET", "http://gateway.test/", nil))
			if w.Code != 502 || strings.Contains(w.Body.String()+fmt.Sprint(w.Header()), secret) {
				t.Fatalf("public response %d %q", w.Code, w.Body.String())
			}
		})
	}
}
