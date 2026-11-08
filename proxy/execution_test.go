package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
)

func executionEngine(t *testing.T, c extension.Config, rs ...extension.Registration) *extension.Engine {
	t.Helper()
	e, err := extension.New(c, rs...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Error(err)
		}
	})
	return e
}
func executionProxy(t *testing.T, url string, e *extension.Engine, auth bool) *httptest.Server {
	t.Helper()
	p, err := NewWithEngine(url, e)
	if err != nil {
		t.Fatal(err)
	}
	p.ErrorLog = log.New(io.Discard, "", 0)
	t.Cleanup(p.Transport.(interface{ CloseIdleConnections() }).CloseIdleConnections)
	var h http.Handler = p
	if auth {
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bind ahead of authentication middleware; the proxy's second Bind
			// is idempotent and preserves this host-authenticated context.
			var err error
			r, err = e.Bind(r)
			if err != nil {
				http.Error(w, "invalid capability", 401)
				return
			}
			if a, ok := extension.AuthenticationFromContext(r.Context()); !ok || a.Subject == "" {
				user := r.Header.Get("X-Example-User")
				if user == "" {
					http.Error(w, "authentication required", 401)
					return
				}
				r = r.WithContext(extension.WithAuthentication(r.Context(), extension.Authentication{Subject: user, Claims: map[string]string{"scope": "personal"}}))
			}
			p.ServeHTTP(w, r)
		})
	}
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}
func hook(id, owner string, stage extension.Stage, h extension.Handler) extension.Registration {
	return extension.Registration{ID: id, Owner: owner, Stage: stage, Handler: h}
}

// These independent fixture registrations deliberately share no broker state.
func mockBroker(destination string) extension.Registration {
	return hook("broker", "broker", extension.Resolve, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Request.URL = destination + "/served"
		return extension.Result{Action: extension.Replace, Message: &in.Request, Credentials: http.Header{"Authorization": {"Bearer example-" + in.Auth.Subject}}, Metadata: map[string]string{"account": in.Auth.Subject}}, nil
	})
}
func blockingPolicy() extension.Registration {
	return hook("policy", "policy", extension.Upstream, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Auth.Subject == "denied" || in.Auth.Subject == "" {
			return extension.Result{}, fmt.Errorf("policy denied")
		}
		if in.Request.Header.Get("Authorization") != "" {
			return extension.Result{}, fmt.Errorf("upstream credential leaked into policy")
		}
		return extension.Result{}, nil
	})
}

type originalObservation struct {
	requested, served string
	id                extension.Identity
	body              string
}

func independentRecorder(mu *sync.Mutex, records map[string]*originalObservation) []extension.Registration {
	header := hook("original-headers", "recorder", extension.Observe, func(_ context.Context, in extension.Input) (extension.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		records[in.Identity.Exchange] = &originalObservation{requested: in.Requested.URL, served: in.Request.URL, id: in.Identity}
		if in.Request.Header.Get("Authorization") != "" {
			return extension.Result{}, fmt.Errorf("credential leaked to recorder")
		}
		return extension.Result{}, nil
	})
	body := hook("original-bytes", "recorder", extension.OriginalBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		records[in.Identity.Exchange].body += string(in.Response.Body)
		return extension.Result{}, nil
	})
	body.Body = extension.Unit
	return []extension.Registration{header, body}
}
func presentationDecorator() extension.Registration {
	r := hook("presentation", "presentation", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if len(in.Metadata) != 0 {
			return extension.Result{}, fmt.Errorf("broker metadata leaked")
		}
		in.Response.Body = []byte("decorated:" + string(in.Response.Body))
		return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
	})
	r.Body = extension.Whole
	return r
}

func TestExecutionSocketIndependentCompositionAndConcurrentCredentials(t *testing.T) {
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		user := r.Header.Get("X-Example-User")
		if r.Header.Get("Authorization") != "Bearer example-"+user {
			t.Errorf("cross-request credentials %q user %q", r.Header.Get("Authorization"), user)
		}
		if r.Header.Get("X-Proxykit-Root") != "" {
			t.Error("reserved header leaked upstream")
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Proxykit-Root", "upstream-forgery")
		fmt.Fprint(w, "original:"+user)
	}))
	defer upstream.Close()
	var mu sync.Mutex
	records := map[string]*originalObservation{}
	regs := []extension.Registration{mockBroker(upstream.URL), blockingPolicy(), presentationDecorator()}
	regs = append(regs, independentRecorder(&mu, records)...)
	e := executionEngine(t, extension.Config{}, regs...)
	gateway := executionProxy(t, "http://unused.example", e, true)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := fmt.Sprintf("example-%d", i)
			r, _ := http.NewRequest("POST", gateway.URL+"/requested", nil)
			r.Header.Set("X-Example-User", user)
			r.Header.Set("X-Proxykit-Root", "forged")
			resp, err := http.DefaultClient.Do(r)
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err != nil || string(b) != "decorated:original:"+user || resp.Header.Get("X-Proxykit-Root") != "" {
				t.Errorf("response %q %v %+v", b, err, resp.Header)
			}
		}(i)
	}
	wg.Wait()
	r, _ := http.NewRequest("GET", gateway.URL, nil)
	r.Header.Set("X-Example-User", "denied")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 502 || dispatches.Load() != 12 {
		t.Fatalf("policy failed: %d %d", resp.StatusCode, dispatches.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(records) != 12 {
		t.Fatal(len(records))
	}
	for _, r := range records {
		if !strings.Contains(r.requested, "/requested") || r.served != upstream.URL+"/served" || !strings.HasPrefix(r.body, "original:") || r.id.Root == "forged" {
			t.Fatalf("record %+v", r)
		}
	}
}

func TestExecutionSocketAuthenticatedOrdinaryHTTPReuse(t *testing.T) {
	var gateway *httptest.Server
	var e *extension.Engine
	var rootsMu sync.Mutex
	var roots []extension.Identity
	var scoped *http.Client
	h := hook("reenter", "example", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
		rootsMu.Lock()
		roots = append(roots, in.Identity)
		rootsMu.Unlock()
		if in.Auth.Subject != "Example user" {
			return extension.Result{}, fmt.Errorf("auth lost: %+v", in.Auth)
		}
		if in.Identity.Depth > 0 {
			return extension.Result{}, nil
		}
		client, err := e.Client(ctx, gateway.URL)
		if err != nil {
			return extension.Result{}, err
		}
		rootsMu.Lock()
		scoped = client
		rootsMu.Unlock()
		if _, err = client.Get("http://unrelated.invalid/"); err == nil {
			return extension.Result{}, fmt.Errorf("origin escape")
		}
		r, err := client.Get(gateway.URL + "/inner")
		if err != nil {
			return extension.Result{}, err
		}
		b, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			return extension.Result{}, err
		}
		if r.StatusCode != 200 {
			return extension.Result{}, fmt.Errorf("inner %d", r.StatusCode)
		}
		m := extension.Message{Status: 200, Header: http.Header{}, Body: append([]byte("outer:"), b...)}
		return extension.Result{Action: extension.Respond, Message: &m}, nil
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Example-User") != "" {
			t.Error("arbitrary parent headers inherited")
		}
		if r.Header.Get("Authorization") != "Bearer example-Example user" {
			t.Error("scoped auth did not reach broker")
		}
		fmt.Fprint(w, "inner")
	}))
	defer upstream.Close()
	e = executionEngine(t, extension.Config{}, h, mockBroker(upstream.URL), blockingPolicy())
	gateway = executionProxy(t, upstream.URL, e, true)
	r, _ := http.NewRequest("GET", gateway.URL+"/outer", nil)
	r.Header.Set("X-Example-User", "Example user")
	r.Header.Set("X-Proxykit-Parent", "spoof")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(b) != "outer:inner" {
		t.Fatalf("%q %v status %d", b, err, resp.StatusCode)
	}
	rootsMu.Lock()
	if len(roots) != 2 || roots[0].Root != roots[1].Root || roots[1].Parent != roots[0].Exchange || roots[0].Parent != "" {
		t.Fatal(roots)
	}
	scopedClient := scoped
	rootsMu.Unlock()
	if _, err := scopedClient.Get(gateway.URL + "/inner"); err == nil {
		t.Fatal("expired scope remained usable")
	}
	r, _ = http.NewRequest("GET", gateway.URL+"/outer", nil)
	r.Header.Set("X-Proxykit-Capability", "public-forgery")
	resp, err = http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
}

func TestExecutionSocketOneSlotManagedReentryRejects(t *testing.T) {
	var gateway *httptest.Server
	var e *extension.Engine
	h := hook("reenter", "example", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
		client, err := e.Client(ctx, gateway.URL)
		if err != nil {
			return extension.Result{}, err
		}
		r, err := client.Get(gateway.URL + "/nested")
		if err != nil {
			return extension.Result{}, err
		}
		r.Body.Close()
		if r.StatusCode != 502 {
			return extension.Result{}, fmt.Errorf("expected overload, got %d", r.StatusCode)
		}
		m := extension.Message{Status: 200, Header: http.Header{}, Body: []byte("rejected without deadlock")}
		return extension.Result{Action: extension.Respond, Message: &m}, nil
	})
	e = executionEngine(t, extension.Config{Admission: 1, InvocationTimeout: time.Second}, h)
	gateway = executionProxy(t, "http://unused.example", e, false)
	client := &http.Client{Timeout: 2 * time.Second}
	r, err := client.Get(gateway.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(b) != "rejected without deadlock" {
		t.Fatalf("%d %q", r.StatusCode, b)
	}
}

func TestExecutionSocketTrailersAndDuplex(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NewResponseController(w).EnableFullDuplex()
		w.Header().Set("Trailer", "X-End, X-Proxykit-Private")
		w.WriteHeader(200)
		fmt.Fprint(w, "early\n")
		http.NewResponseController(w).Flush()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if string(b) != "upload" || r.Trailer.Get("X-Upload") != "done" || r.Trailer.Get("X-Proxykit-Private") != "" {
			t.Errorf("upload %q %+v", b, r.Trailer)
		}
		fmt.Fprint(w, "late\n")
		w.Header().Set("X-End", "done")
		w.Header().Set("X-Proxykit-Private", "secret")
	}))
	defer upstream.Close()
	e := executionEngine(t, extension.Config{})
	gateway := executionProxy(t, upstream.URL, e, false)
	reader, writer := io.Pipe()
	r, _ := http.NewRequest("POST", gateway.URL, reader)
	r.Trailer = http.Header{"X-Upload": nil, "X-Proxykit-Private": nil}
	result := make(chan *http.Response, 1)
	failure := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			failure <- err
			return
		}
		result <- resp
	}()
	var resp *http.Response
	select {
	case resp = <-result:
	case err := <-failure:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		writer.Close()
		t.Fatal("response blocked behind upload")
	}
	br := bufio.NewReader(resp.Body)
	s, err := br.ReadString('\n')
	if err != nil || s != "early\n" {
		t.Fatalf("%q %v", s, err)
	}
	r.Trailer.Set("X-Upload", "done")
	r.Trailer.Set("X-Proxykit-Private", "secret")
	io.WriteString(writer, "upload")
	writer.Close()
	s, err = br.ReadString('\n')
	if err != nil || s != "late\n" {
		t.Fatalf("%q %v", s, err)
	}
	io.Copy(io.Discard, br)
	resp.Body.Close()
	if resp.Trailer.Get("X-End") != "done" || resp.Trailer.Get("X-Proxykit-Private") != "" {
		t.Fatal(resp.Trailer)
	}
}

func TestExecutionSocketLocalHEADAndChildTrailers(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/child" {
			w.Header().Set("Trailer", "X-End")
			fmt.Fprint(w, "child")
			w.Header().Set("X-End", "selected")
			return
		}
		w.WriteHeader(503)
		fmt.Fprint(w, "retry")
	}))
	defer upstream.Close()
	local := hook("local", "cache", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Request.Method == http.MethodHead {
			m := extension.Message{Status: 200, Header: http.Header{}}
			return extension.Result{Action: extension.Respond, Message: &m}, nil
		}
		return extension.Result{}, nil
	})
	child := hook("fallback", "route", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Response.Status == 503 && in.Identity.Depth == 0 {
			r, _ := http.NewRequest("GET", upstream.URL+"/child", nil)
			return extension.Result{Action: extension.Child, Request: r}, nil
		}
		return extension.Result{}, nil
	})
	final := hook("headers", "presentation", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Header.Set("X-Full-Chain", "yes")
		return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
	})
	final.After = []string{"fallback"}
	e := executionEngine(t, extension.Config{}, local, child, final)
	gateway := executionProxy(t, upstream.URL, e, false)
	resp, err := http.Head(gateway.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || len(b) != 0 || resp.Header.Get("X-Full-Chain") != "yes" {
		t.Fatalf("HEAD %q %v %+v", b, err, resp.Header)
	}
	resp, err = http.Get(gateway.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(b) != "child" || resp.Trailer.Get("X-End") != "selected" {
		t.Fatalf("child %q %v %+v", b, err, resp.Trailer)
	}
}

func TestExecutionSocketUpgradePassthroughAndExplicitTransformRejection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		fmt.Fprint(buf, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: example\r\n\r\n")
		buf.Flush()
		io.Copy(c, c)
	}))
	defer upstream.Close()
	for _, transform := range []bool{false, true} {
		t.Run(fmt.Sprint(transform), func(t *testing.T) {
			var rs []extension.Registration
			if transform {
				rs = append(rs, hook("transform", "example", extension.ResponseStart, func(context.Context, extension.Input) (extension.Result, error) { return extension.Result{}, nil }))
			}
			e := executionEngine(t, extension.Config{}, rs...)
			gateway := executionProxy(t, upstream.URL, e, false)
			c, err := net.Dial("tcp", strings.TrimPrefix(gateway.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(2 * time.Second))
			fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: example\r\n\r\n")
			br := bufio.NewReader(c)
			resp, err := http.ReadResponse(br, nil)
			if err != nil {
				t.Fatal(err)
			}
			if transform {
				if resp.StatusCode != 502 {
					t.Fatal(resp.StatusCode)
				}
				resp.Body.Close()
				return
			}
			if resp.StatusCode != 101 {
				t.Fatal(resp.StatusCode)
			}
			fmt.Fprint(c, "ping")
			b := make([]byte, 4)
			if _, err = io.ReadFull(br, b); err != nil || string(b) != "ping" {
				t.Fatalf("%q %v", b, err)
			}
		})
	}
}
