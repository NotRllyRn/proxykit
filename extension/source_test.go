package extension_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/NotRllyRn/proxykit/extension"
)

func TestOriginalSourceSurvivesEffectiveMetadataChanges(t *testing.T) {
	for _, whole := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "whole"}[whole], func(t *testing.T) {
			var observations []extension.Input
			wrongFormat := false
			wrong := reg("wrong-format", extension.OriginalBody, func(context.Context, extension.Input) (extension.Result, error) {
				wrongFormat = true
				return extension.Result{}, nil
			})
			wrong.Body, wrong.Formats = extension.Unit, []string{"application/json"}
			source := reg("source", extension.OriginalBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
				observations = append(observations, in)
				return extension.Result{}, nil
			})
			source.Body, source.Formats = extension.Unit, []string{"text/plain"}
			mutate := reg("metadata", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
				in.Response.Status = 201
				in.Response.Header.Set("Content-Type", "application/json")
				in.Response.Header.Set("X-Source", "decorated")
				return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
			})
			rs := []extension.Registration{source, mutate, wrong}
			if whole {
				modifier := reg("whole", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
					in.Response.Body = []byte("generated")
					return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
				})
				modifier.Body = extension.Whole
				rs = append(rs, modifier)
			}
			e := engine(t, extension.Config{UnitBytes: 3}, rs...)
			resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) {
				r := response("upstream")
				r.Header.Set("X-Source", "original")
				return r, nil
			})).RoundTrip(request(""))
			if err != nil {
				t.Fatal(err)
			}
			body := read(t, resp)
			want := "upstream"
			if whole {
				want = "generated"
			}
			if body != want || resp.StatusCode != 201 || resp.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("effective %d %v %q", resp.StatusCode, resp.Header, body)
			}
			var original strings.Builder
			for _, in := range observations {
				if in.Response.Status != 200 || in.Response.Header.Get("Content-Type") != "text/plain" || in.Response.Header.Get("X-Source") != "original" {
					t.Fatalf("source metadata: %+v", in.Response)
				}
				original.Write(in.Response.Body)
			}
			if wrongFormat {
				t.Fatal("effective MIME misclassified original source")
			}
			if original.String() != "upstream" {
				t.Fatalf("original %q", original.String())
			}
		})
	}
}

func TestOriginalSourceLocalChildAndAbandonment(t *testing.T) {
	var seen []extension.Input
	var abandoned bool
	original := reg("original", extension.OriginalBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		seen = append(seen, in)
		return extension.Result{}, nil
	})
	original.Body = extension.Unit
	local := reg("local", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if strings.HasSuffix(in.Request.URL, "/local") {
			return extension.Result{Action: extension.Respond, Message: &extension.Message{Status: 202, Header: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("local bytes")}}, nil
		}
		return extension.Result{}, nil
	})
	child := reg("child", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth == 0 && !strings.HasSuffix(in.Request.URL, "/local") {
			destination := "http://example.test/child"
			if strings.HasSuffix(in.Request.URL, "/child-local") {
				destination = "http://example.test/local"
			}
			r, _ := http.NewRequest("GET", destination, nil)
			return extension.Result{Action: extension.Child, Request: r}, nil
		}
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{}, original, local, child)
	rt := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/child" {
			return response("child upstream"), nil
		}
		res := response("abandoned parent")
		res.Body = &sourceClose{Reader: strings.NewReader("abandoned parent"), closed: &abandoned}
		return res, nil
	}))
	r, err := rt.RoundTrip(func() *http.Request { r := request(""); r.URL.Path = "/local"; return r }())
	if err != nil {
		t.Fatal(err)
	}
	if read(t, r) != "local bytes" || len(seen) != 0 {
		t.Fatal("local output labeled upstream", seen)
	}
	r, err = rt.RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	if read(t, r) != "child upstream" || !abandoned {
		t.Fatal("child replacement/abandonment")
	}
	var b strings.Builder
	for _, in := range seen {
		if in.Identity.Depth != 1 || in.Identity.Parent == "" {
			t.Fatalf("wrong source identity %+v", in.Identity)
		}
		b.Write(in.Response.Body)
	}
	if b.String() != "child upstream" {
		t.Fatalf("wrong source bytes %q", b.String())
	}
	seen = nil
	localChild := request("")
	localChild.URL.Path = "/child-local"
	r, err = rt.RoundTrip(localChild)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, r) != "local bytes" || len(seen) != 0 {
		t.Fatal("generated child output labeled upstream", seen)
	}
}

type sourceClose struct {
	io.Reader
	closed *bool
}

func (b *sourceClose) Close() error { *b.closed = true; return nil }
