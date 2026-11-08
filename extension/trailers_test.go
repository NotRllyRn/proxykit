package extension_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/NotRllyRn/proxykit/extension"
)

func TestRequestHasTrailers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trailer http.Header
		header  string
		want    bool
	}{
		{name: "nil"},
		{name: "empty", trailer: http.Header{}},
		{name: "declared without header", trailer: http.Header{"X-Checksum": nil}, want: true},
		{name: "populated without header", trailer: http.Header{"X-Checksum": {"example"}}, want: true},
		{name: "header only", header: "X-Checksum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen []bool
			e := engine(t, extension.Config{},
				reg("spoof", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
					seen = append(seen, in.RequestHasTrailers)
					// Mutating a detached Input or replacing headers/owner metadata
					// cannot replace a host-owned ingress fact.
					in.RequestHasTrailers = !tc.want
					in.Request.Header.Set("Trailer", "X-Spoof")
					in.Request.Header.Set("RequestHasTrailers", strconv.FormatBool(!tc.want))
					return extension.Result{Action: extension.Replace, Message: &in.Request, Metadata: map[string]string{"requestHasTrailers": strconv.FormatBool(!tc.want)}}, nil
				}),
				reg("check", extension.Resolve, func(_ context.Context, in extension.Input) (extension.Result, error) {
					seen = append(seen, in.RequestHasTrailers)
					if in.Metadata["requestHasTrailers"] != strconv.FormatBool(!tc.want) || in.Request.Header.Get("Trailer") != "X-Spoof" {
						return extension.Result{}, fmt.Errorf("spoof fixture did not reach next hook")
					}
					return extension.Result{}, nil
				}),
				reg("finish", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
					seen = append(seen, in.RequestHasTrailers)
					return extension.Result{}, nil
				}),
			)
			r := request("")
			r.Trailer = tc.trailer
			if tc.header != "" {
				r.Header.Set("Trailer", tc.header)
			}
			resp, err := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
				// Later trailer changes (including undeclared keys) are not ingress.
				clear(r.Trailer)
				if !tc.want {
					r.Trailer = http.Header{"X-Late": {"example"}}
				}
				return response("ok"), nil
			})).RoundTrip(r)
			if err != nil {
				t.Fatal(err)
			}
			read(t, resp)
			if len(seen) != 3 {
				t.Fatalf("hook observations: %v", seen)
			}
			for _, got := range seen {
				if got != tc.want {
					t.Fatalf("requestHasTrailers observations %v, want %v", seen, tc.want)
				}
			}
		})
	}
}

func TestRequestHasTrailersBindSnapshot(t *testing.T) {
	for _, original := range []bool{false, true} {
		t.Run(strconv.FormatBool(original), func(t *testing.T) {
			var seen bool
			e := engine(t, extension.Config{}, reg("check", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
				seen = true
				if in.RequestHasTrailers != original || in.Requested.URL != "http://example.test/input" || in.Request.URL != "http://example.test/rewritten" {
					return extension.Result{}, fmt.Errorf("lost original Bind snapshot: %+v", in)
				}
				return extension.Result{}, nil
			}))
			r := request("")
			r.Trailer = http.Header{}
			if original {
				r.Trailer["X-Checksum"] = nil
			}
			bound, err := e.Bind(r)
			if err != nil {
				t.Fatal(err)
			}
			// Mutate the shared map after Bind, then simulate gateway rewriting
			// and a second Bind. Neither may resnapshot the original fact.
			clear(r.Trailer)
			if !original {
				r.Trailer["X-Added"] = nil
			}
			bound.URL.Path = "/rewritten"
			bound, err = e.Bind(bound)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(bound)
			if err != nil {
				t.Fatal(err)
			}
			read(t, resp)
			if !seen {
				t.Fatal("hook not called")
			}
		})
	}
}

func TestRequestHasTrailersChildIndependence(t *testing.T) {
	for _, mode := range []string{"call", "child", "reentry"} {
		for _, parentTrailers := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/parent=%v", mode, parentTrailers), func(t *testing.T) {
				var e *extension.Engine
				var gateway *httptest.Server
				var seen []bool
				base := transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })
				e = engine(t, extension.Config{},
					reg("check", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
						seen = append(seen, in.RequestHasTrailers)
						want := parentTrailers
						if in.Identity.Depth == 1 {
							want = !parentTrailers
						}
						if in.RequestHasTrailers != want {
							return extension.Result{}, fmt.Errorf("depth %d: got %v, want %v", in.Identity.Depth, in.RequestHasTrailers, want)
						}
						return extension.Result{}, nil
					}),
					reg("nested", extension.ResponseStart, func(ctx context.Context, in extension.Input) (extension.Result, error) {
						if in.Identity.Depth != 0 {
							return extension.Result{}, nil
						}
						// Native children inherit the context containing the parent's
						// Bind snapshot, but must inspect their own supplied request.
						r := request("child").WithContext(ctx)
						if !parentTrailers {
							r.Trailer = http.Header{"X-Child": {"example"}}
							r.ContentLength = -1
						}
						if mode == "child" {
							return extension.Result{Action: extension.Child, Request: r}, nil
						}
						call := in.Call
						if mode == "reentry" {
							client, err := e.Client(ctx, gateway.URL)
							if err != nil {
								return extension.Result{}, err
							}
							r.URL.Scheme = "http"
							r.URL.Host = gateway.Listener.Addr().String()
							call = func(_ context.Context, r *http.Request) (*http.Response, error) { return client.Do(r) }
						}
						resp, err := call(ctx, r)
						if err != nil {
							return extension.Result{}, err
						}
						_, err = io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						return extension.Result{}, err
					}),
					reg("finish", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
						seen = append(seen, in.RequestHasTrailers)
						return extension.Result{}, nil
					}),
				)
				if mode == "reentry" {
					gateway = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						bound, err := e.Bind(r)
						if err != nil {
							http.Error(w, err.Error(), 500)
							return
						}
						bound.URL.Scheme, bound.URL.Host = "http", "example.test"
						// Reentry must use its own Bind snapshot, not the parent
						// and not the rewritten request's trailer map.
						bound.Trailer = nil
						if parentTrailers {
							bound.Trailer = http.Header{"X-Rewritten": nil}
						}
						resp, err := e.Transport(base).RoundTrip(bound)
						if err != nil {
							http.Error(w, err.Error(), 500)
							return
						}
						defer resp.Body.Close()
						io.Copy(w, resp.Body)
					}))
					t.Cleanup(gateway.Close)
				}
				r := request("")
				if parentTrailers {
					r.Trailer = http.Header{"X-Parent": nil}
				}
				bound, err := e.Bind(r)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := e.Transport(base).RoundTrip(bound)
				if err != nil {
					t.Fatal(err)
				}
				read(t, resp)
				want := []bool{parentTrailers, !parentTrailers, !parentTrailers, parentTrailers}
				if !reflect.DeepEqual(seen, want) {
					t.Fatalf("parent/child Prepare and Finish: got %v, want %v", seen, want)
				}
			})
		}
	}
}
