package extension_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(s string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader(s)), ContentLength: int64(len(s))}
}
func request(body string) *http.Request {
	r, _ := http.NewRequest("POST", "http://example.test/input", strings.NewReader(body))
	r.Header.Set("Content-Type", "text/plain")
	return r
}
func engine(t *testing.T, c extension.Config, rs ...extension.Registration) *extension.Engine {
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
func read(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func pass(context.Context, extension.Input) (extension.Result, error) { return extension.Result{}, nil }
func reg(id string, s extension.Stage, h extension.Handler) extension.Registration {
	return extension.Registration{Owner: "example", ID: id, Stage: s, Handler: h}
}

func TestOrderingValidationAndFrozenRegistration(t *testing.T) {
	var got []string
	makeHook := func(id string, s extension.Stage) extension.Registration {
		return reg(id, s, func(_ context.Context, in extension.Input) (extension.Result, error) {
			got = append(got, id)
			return extension.Result{}, nil
		})
	}
	a, b, z := makeHook("a", extension.Prepare), makeHook("b", extension.Prepare), makeHook("z", extension.Resolve)
	a.After = []string{"b", "absent"}
	z.Requires = []string{"a"}
	e := engine(t, extension.Config{}, z, a, b)
	a.After[0] = "a" // registration slices are frozen
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
	if !reflect.DeepEqual(got, []string{"b", "a", "z"}) {
		t.Fatal(got)
	}
	for _, rs := range [][]extension.Registration{
		{a, a},
		{func() extension.Registration {
			r := reg("a", extension.Prepare, pass)
			r.Requires = []string{"missing"}
			return r
		}()},
		{func() extension.Registration {
			r := reg("a", extension.Prepare, pass)
			r.After = []string{"b"}
			return r
		}(), func() extension.Registration {
			r := reg("b", extension.Prepare, pass)
			r.After = []string{"a"}
			return r
		}()},
		{func() extension.Registration {
			r := reg("a", extension.Prepare, pass)
			r.After = []string{"b"}
			return r
		}(), reg("b", extension.Resolve, pass)},
	} {
		if e, err := extension.New(extension.Config{}, rs...); err == nil {
			e.Close()
			t.Fatalf("accepted invalid registrations: %+v", rs)
		}
	}
}

func TestWholeRecoveryMetadataAndIsolation(t *testing.T) {
	first := reg("a", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Request.Body = append(in.Request.Body, 'A')
		return extension.Result{Action: extension.Replace, Message: &in.Request, Metadata: map[string]string{"accepted": "yes"}}, nil
	})
	first.Body = extension.Whole
	bad := reg("b", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Request.Body[0] = '!'
		in.Request.Header.Set("bad", "mutation")
		in.Metadata["accepted"] = "no"
		return extension.Result{Metadata: in.Metadata}, errors.New("failed")
	})
	bad.Body = extension.Whole
	bad.Failure = extension.FailOpen
	last := reg("c", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if string(in.Request.Body) != "inputA" || in.Metadata["accepted"] != "yes" || in.Request.Header.Get("bad") != "" {
			return extension.Result{}, fmt.Errorf("mutated input %+v", in)
		}
		in.Request.Body = append(in.Request.Body, 'C')
		return extension.Result{Action: extension.Replace, Message: &in.Request}, nil
	})
	last.Body = extension.Whole
	stranger := reg("d", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if len(in.Metadata) != 0 {
			return extension.Result{}, errors.New("owner namespace leak")
		}
		return extension.Result{}, nil
	})
	stranger.Owner = "independent"
	e := engine(t, extension.Config{}, first, bad, last, stranger)
	resp, err := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if string(b) != "inputAC" {
			t.Errorf("body %s", b)
		}
		if r.ContentLength != 7 {
			t.Errorf("length %d", r.ContentLength)
		}
		return response("ok"), nil
	})).RoundTrip(request("input"))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
}

func TestTimeoutKeepsAdmissionUntilActualExit(t *testing.T) {
	entered, exit, ended := make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := reg("slow", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		close(entered)
		<-exit
		in.Request.Header.Set("late", "ignored")
		close(ended)
		return extension.Result{Action: extension.Replace, Message: &in.Request}, nil
	})
	e := engine(t, extension.Config{Admission: 1, InvocationTimeout: 20 * time.Millisecond}, h)
	rt := e.Transport(transport(func(*http.Request) (*http.Response, error) { t.Error("unexpected dispatch"); return response(""), nil }))
	_, err := rt.RoundTrip(request(""))
	<-entered
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	_, err = rt.RoundTrip(request(""))
	if !errors.Is(err, extension.ErrOverload) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err = e.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(exit)
	<-ended
	if err = e.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = rt.RoundTrip(request("")); !errors.Is(err, extension.ErrClosed) {
		t.Fatal(err)
	}
}

func TestOneSlotNestedCallRejectsWithoutDeadlock(t *testing.T) {
	h := reg("recursive", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
		_, err := in.Call(ctx, request("child"))
		if !errors.Is(err, extension.ErrOverload) {
			return extension.Result{}, fmt.Errorf("nested: %w", err)
		}
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{Admission: 1}, h)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
}

func TestStreamingRecoveryAndOriginalRecorder(t *testing.T) {
	var original bytes.Buffer
	observe := reg("independent-recorder", extension.OriginalBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		original.Write(in.Response.Body)
		in.Response.Body[0] = '!'
		return extension.Result{}, nil
	})
	observe.Body = extension.Unit
	observe.Owner = "recorder"
	a := reg("a", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Body = bytes.ToUpper(in.Response.Body)
		return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
	})
	a.Body = extension.Unit
	b := reg("b", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Body[0] = '!'
		return extension.Result{}, errors.New("unit failure")
	})
	b.Body = extension.Unit
	b.Failure = extension.FailOpen
	c := reg("c", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Body = append(in.Response.Body, '+')
		return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
	})
	c.Body = extension.Unit
	// UnitBytes bounds both input and replacement; use shorter source reads.
	e := engine(t, extension.Config{UnitBytes: 4}, observe, a, b, c)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) {
		r := response("")
		r.Body = io.NopCloser(&shortReader{s: "abcdef"})
		return r, nil
	})).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, resp); got != "ABC+DEF+" {
		t.Fatal(got)
	}
	if original.String() != "abcdef" {
		t.Fatal(original.String())
	}
	if resp.ContentLength != -1 {
		t.Fatal(resp.ContentLength)
	}
}

type shortReader struct{ s string }

func (r *shortReader) Read(p []byte) (int, error) {
	if r.s == "" {
		return 0, io.EOF
	}
	if len(p) > 3 {
		p = p[:3]
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}

func TestLocalAndChildFullChainsAndProvenance(t *testing.T) {
	var prepare, finish int
	var ids []extension.Identity
	requestHook := reg("local", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		prepare++
		if strings.Contains(in.Request.URL, "local") {
			m := extension.Message{Status: 200, Header: http.Header{}, Body: []byte("local")}
			return extension.Result{Action: extension.Respond, Message: &m}, nil
		}
		return extension.Result{}, nil
	})
	child := reg("fallback", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		ids = append(ids, in.Identity)
		if in.Identity.Depth == 0 {
			r, _ := http.NewRequest("GET", "http://example.test/local", nil)
			return extension.Result{Action: extension.Child, Request: r}, nil
		}
		return extension.Result{}, nil
	})
	var provenance []extension.Identity
	provenanceHook := reg("provenance", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth == 0 {
			provenance = in.Provenance
		}
		return extension.Result{}, nil
	})
	provenanceHook.After = []string{"fallback"}
	decorate := reg("decorator", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Body = append(in.Response.Body, '!')
		return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
	})
	decorate.Body = extension.Whole
	terminal := reg("terminal", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
		finish++
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{}, requestHook, child, provenanceHook, decorate, terminal)
	calls := 0
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { calls++; return response("original"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, resp); got != "local!!" {
		t.Fatal(got)
	}
	if calls != 1 || prepare != 2 || finish != 2 || len(provenance) != 2 {
		t.Fatalf("%d %d %d %+v", calls, prepare, finish, provenance)
	}
	if ids[0].Root != ids[1].Root || ids[1].Parent != ids[0].Exchange || ids[0].Exchange == ids[1].Exchange || ids[1].Attempt != 2 {
		t.Fatal(ids)
	}
}

func TestAtomicRootBudgetForParallelChildren(t *testing.T) {
	var successes atomic.Int32
	h := reg("fanout", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth != 0 {
			return extension.Result{}, nil
		}
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, err := in.Call(ctx, request(""))
				if err == nil {
					io.Copy(io.Discard, r.Body)
					r.Body.Close()
					successes.Add(1)
				} else if !errors.Is(err, extension.ErrLimit) {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{Attempts: 4}, h)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
	if successes.Load() != 3 {
		t.Fatal(successes.Load())
	}
}

func TestBoundsFormatsAndExplicitUnsupportedModes(t *testing.T) {
	whole := reg("whole", extension.ResponseBody, pass)
	whole.Body = extension.Whole
	e := engine(t, extension.Config{BodyBytes: 3}, whole)
	_, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("four"), nil })).RoundTrip(request(""))
	if !errors.Is(err, extension.ErrLimit) {
		t.Fatal(err)
	}
	unit := reg("unit", extension.ResponseBody, pass)
	unit.Body = extension.Unit
	if e, err := extension.New(extension.Config{}, whole, unit); !errors.Is(err, extension.ErrUnsupported) {
		if e != nil {
			e.Close()
		}
		t.Fatal(err)
	}
	called := false
	h := reg("format", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
		called = true
		return extension.Result{}, nil
	})
	h.Formats = []string{"application/json"}
	e2 := engine(t, extension.Config{}, h)
	resp, err := e2.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
	if called {
		t.Fatal("format-inapplicable handler ran")
	}
	bad := reg("bad", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		return extension.Result{Action: extension.Respond, Message: &in.Response}, nil
	})
	e3 := engine(t, extension.Config{}, bad)
	_, err = e3.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if !errors.Is(err, extension.ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestLedgerBoundAndTerminalReadError(t *testing.T) {
	sentinel := errors.New("source read failure")
	var terminal error
	end := reg("terminal", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
		terminal = in.Err
		if !in.Truncated || len(in.Ledger) != 1 {
			return extension.Result{}, errors.New("ledger not bounded")
		}
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{LedgerEntries: 1}, reg("a", extension.Prepare, pass), reg("b", extension.Resolve, pass), end)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) {
		r := response("")
		r.Body = io.NopCloser(errorReader{sentinel})
		return r, nil
	})).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !errors.Is(err, sentinel) || !errors.Is(terminal, sentinel) {
		t.Fatalf("%v %v", err, terminal)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestWholeResponseRecoveryAndInvalidAtomicCommit(t *testing.T) {
	a := reg("a", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Body = []byte("accepted")
		return extension.Result{Action: extension.Replace, Message: &in.Response, Metadata: map[string]string{"state": "accepted"}}, nil
	})
	a.Body = extension.Whole
	b := reg("b", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Status = 101
		in.Response.Body = []byte("corrupt")
		return extension.Result{Action: extension.Replace, Message: &in.Response, Metadata: map[string]string{"state": "corrupt"}}, nil
	})
	b.Body = extension.Whole
	b.Failure = extension.FailOpen
	c := reg("c", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if string(in.Response.Body) != "accepted" || in.Metadata["state"] != "accepted" {
			return extension.Result{}, errors.New("invalid candidate partially committed")
		}
		return extension.Result{}, nil
	})
	c.Body = extension.Whole
	e := engine(t, extension.Config{}, a, b, c)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("original"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, resp); got != "accepted" {
		t.Fatal(got)
	}
}

func TestLateFailOpenResultCannotMutateAcceptedState(t *testing.T) {
	release, ended := make(chan struct{}), make(chan struct{})
	h := reg("slow", extension.Prepare, func(_ context.Context, in extension.Input) (extension.Result, error) {
		<-release
		in.Request.Header.Set("Late", "wrong")
		close(ended)
		return extension.Result{Action: extension.Replace, Message: &in.Request}, nil
	})
	h.Failure = extension.FailOpen
	e := engine(t, extension.Config{InvocationTimeout: 10 * time.Millisecond}, h)
	resp, err := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
		close(release)
		<-ended
		if r.Header.Get("Late") != "" {
			t.Error("late result accepted")
		}
		return response("ok"), nil
	})).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
}

func TestOperationTimeoutClosesIdleResponseAndDrains(t *testing.T) {
	e := engine(t, extension.Config{OperationTimeout: 20 * time.Millisecond})
	closed := make(chan struct{})
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) {
		r := response("")
		r.Body = &closeSignal{closed: closed}
		return r, nil
	})).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("idle body not closed on deadline")
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

type closeSignal struct {
	closed chan struct{}
	once   sync.Once
}

func (r *closeSignal) Read([]byte) (int, error) { <-r.closed; return 0, io.ErrClosedPipe }
func (r *closeSignal) Close() error             { r.once.Do(func() { close(r.closed) }); return nil }

func TestTransportFailureAndPolicyFailureTerminalContracts(t *testing.T) {
	sentinel := errors.New("dial failed")
	var stages []string
	observe := reg("observe", extension.Observe, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if !errors.Is(in.Err, sentinel) {
			return extension.Result{}, fmt.Errorf("error not visible: %v", in.Err)
		}
		stages = append(stages, "observe")
		return extension.Result{}, nil
	})
	finish := reg("finish", extension.Finish, func(_ context.Context, in extension.Input) (extension.Result, error) {
		stages = append(stages, "finish")
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{}, observe, finish)
	_, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return nil, sentinel })).RoundTrip(request(""))
	if !errors.Is(err, sentinel) || !reflect.DeepEqual(stages, []string{"observe", "finish"}) {
		t.Fatalf("%v %v", err, stages)
	}
	stages = nil
	deny := reg("deny", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
		return extension.Result{}, errors.New("denied")
	})
	e2 := engine(t, extension.Config{}, deny, finish)
	_, err = e2.Transport(transport(func(*http.Request) (*http.Response, error) { t.Error("denial dispatched"); return response(""), nil })).RoundTrip(request(""))
	if err == nil || !reflect.DeepEqual(stages, []string{"finish"}) {
		t.Fatalf("%v %v", err, stages)
	}
}

func TestChildFailureRestoresParentCandidateAndMetadata(t *testing.T) {
	h := reg("child", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth > 0 {
			return extension.Result{}, nil
		}
		r, _ := http.NewRequest("GET", "http://example.test/fail", nil)
		return extension.Result{Action: extension.Child, Request: r, Metadata: map[string]string{"partial": "forbidden"}}, nil
	})
	h.Failure = extension.FailOpen
	check := reg("check", extension.ResponseStart, func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth == 0 && len(in.Metadata) != 0 {
			return extension.Result{}, errors.New("child failure committed metadata")
		}
		return extension.Result{}, nil
	})
	check.After = []string{"child"}
	e := engine(t, extension.Config{}, h, check)
	resp, err := e.Transport(transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/fail" {
			return nil, errors.New("child transport failed")
		}
		return response("parent"), nil
	})).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, resp); got != "parent" {
		t.Fatal(got)
	}
}

func TestStreamingHeaderMutationRejectedExplicitly(t *testing.T) {
	h := reg("late", extension.ResponseBody, func(_ context.Context, in extension.Input) (extension.Result, error) {
		in.Response.Header.Set("Too-Late", "yes")
		return extension.Result{Action: extension.Replace, Message: &in.Response}, nil
	})
	h.Body = extension.Unit
	e := engine(t, extension.Config{}, h)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("bytes"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !errors.Is(err, extension.ErrUnsupported) {
		t.Fatal(err)
	}
}

func TestDepthAndMetadataLimits(t *testing.T) {
	h := reg("recurse", extension.Prepare, func(ctx context.Context, in extension.Input) (extension.Result, error) {
		resp, err := in.Call(ctx, request(""))
		if in.Identity.Depth == 1 {
			if !errors.Is(err, extension.ErrLimit) {
				return extension.Result{}, fmt.Errorf("depth: %w", err)
			}
			return extension.Result{}, nil
		}
		if err != nil {
			return extension.Result{}, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return extension.Result{}, nil
	})
	e := engine(t, extension.Config{Depth: 1}, h)
	resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { return response("ok"), nil })).RoundTrip(request(""))
	if err != nil {
		t.Fatal(err)
	}
	read(t, resp)
	meta := reg("large-metadata", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
		return extension.Result{Metadata: map[string]string{"key": "oversized"}}, nil
	})
	e2 := engine(t, extension.Config{MetadataBytes: 3}, meta)
	_, err = e2.Transport(transport(func(*http.Request) (*http.Response, error) {
		t.Error("oversized metadata dispatched")
		return response(""), nil
	})).RoundTrip(request(""))
	if !errors.Is(err, extension.ErrLimit) {
		t.Fatal(err)
	}
}

func TestShortCircuitClosesRequestBody(t *testing.T) {
	for _, deny := range []bool{false, true} {
		t.Run(fmt.Sprint(deny), func(t *testing.T) {
			h := reg("local", extension.Prepare, func(context.Context, extension.Input) (extension.Result, error) {
				if deny {
					return extension.Result{}, errors.New("denied")
				}
				m := extension.Message{Status: 200, Header: http.Header{}}
				return extension.Result{Action: extension.Respond, Message: &m}, nil
			})
			e := engine(t, extension.Config{}, h)
			r := request("")
			closed := make(chan struct{})
			r.Body = &closeSignal{closed: closed}
			resp, err := e.Transport(transport(func(*http.Request) (*http.Response, error) { t.Error("unexpected dispatch"); return response(""), nil })).RoundTrip(r)
			if err == nil {
				read(t, resp)
			}
			select {
			case <-closed:
			default:
				t.Fatal("short-circuited request body not closed")
			}
		})
	}
}

func TestSetupRollbackAndShutdownCleanupOrder(t *testing.T) {
	var got []string
	a := reg("a", extension.Prepare, pass)
	a.Setup = func(context.Context) error { got = append(got, "setup-a"); return nil }
	a.Cleanup = func(context.Context) error { got = append(got, "cleanup-a"); return nil }
	b := reg("b", extension.Resolve, pass)
	b.Setup = func(context.Context) error { return errors.New("startup") }
	if e, err := extension.New(extension.Config{}, a, b); err == nil {
		e.Close()
		t.Fatal("setup accepted")
	}
	if !reflect.DeepEqual(got, []string{"setup-a", "cleanup-a"}) {
		t.Fatal(got)
	}
	got = nil
	b.Setup = nil
	b.Cleanup = func(context.Context) error { got = append(got, "cleanup-b"); return nil }
	e, err := extension.New(extension.Config{}, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	e.Close()
	if !reflect.DeepEqual(got, []string{"setup-a", "cleanup-b", "cleanup-a"}) {
		t.Fatal(got)
	}
}
