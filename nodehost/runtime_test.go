package nodehost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/proxy"
	"github.com/NotRllyRn/proxykit/worker"
)

func disableFixture(t *testing.T, store, id string) {
	t.Helper()
	i := extensions.Installer{Store: store}
	s, e := i.State()
	if e != nil {
		t.Fatal(e)
	}
	s, e = i.Decide(s, id, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = i.SaveState(s); e != nil {
		t.Fatal(e)
	}
}
func runtimeProxy(t *testing.T, store string, up http.Handler, drain time.Duration, max int) (*Runtime, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(up)
	t.Cleanup(upstream.Close)
	gateway := httptest.NewUnstartedServer(nil)
	origin := "http://" + gateway.Listener.Addr().String()
	config := []byte(fmt.Sprintf(`{"gatewayOrigin":%q}`, origin))
	m, e := OpenRuntime(context.Background(), RuntimeOptions{Options: Options{Store: store, Gateway: origin, Worker: worker.Options{Config: config}}, Drain: drain, MaxDraining: max})
	if e != nil {
		t.Fatal(e)
	}
	p, e := proxy.NewWithRuntime(upstream.URL, m)
	if e != nil {
		t.Fatal(e)
	}
	gateway.Config.Handler = p
	gateway.Start()
	t.Cleanup(func() {
		gateway.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if e := m.Close(ctx); e != nil {
			t.Logf("runtime cleanup (deliberate worker-failure fixtures can fail): %v", e)
		}
	})
	return m, gateway
}
func getRuntime(t *testing.T, url string) (string, http.Header) {
	t.Helper()
	r, e := http.Get(url)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	b, e := io.ReadAll(r.Body)
	if e != nil {
		t.Fatal(e)
	}
	if r.StatusCode != 200 {
		t.Fatalf("HTTP %d: %s", r.StatusCode, b)
	}
	return string(b), r.Header
}
func waitFile(t *testing.T, p string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(p); e == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("missing %s", p)
}
func waitRetired(t *testing.T, m *Runtime) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.draining)
		stopping := false
		for _, w := range m.instances {
			stopping = stopping || w.stopping
		}
		m.mu.Unlock()
		if n == 0 && !stopping {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("generation did not retire")
}

const recorderSource = `export default api=>{api.on({id:'label',stage:'responseStart',bodyMode:'none'},i=>({action:'replace',message:{...i.response,header:{...i.response.header,'X-Generation':['recorder']}}}));};`

func TestRuntimeReusesStatefulWorkerAndRollsBackDependencies(t *testing.T) {
	store := t.TempDir()
	dir := t.TempDir()
	lock := filepath.Join(dir, "store-lock")
	setups := filepath.Join(dir, "setups")
	jobs := filepath.Join(dir, "jobs")
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	source := fmt.Sprintf(`import fs from 'node:fs';import http from 'node:http';export default async api=>{
 const fd=fs.openSync(%q,'wx');fs.appendFileSync(%q,process.pid+'\n');
 const server=http.createServer((q,s)=>s.end('stateful'));await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(%d,'127.0.0.1',resolve)});
 const timer=setInterval(()=>fs.appendFileSync(%q,'tick\n'),20);
 api.cleanup(async()=>{clearInterval(timer);await new Promise(resolve=>server.close(resolve));fs.closeSync(fd);fs.unlinkSync(%q)});
 api.on({id:'security',stage:'responseStart',bodyMode:'none'},i=>({action:'replace',message:{...i.response,header:{...i.response.header,'X-Stateful-Pid':[String(process.pid)]}}}));
 };`, lock, setups, port, jobs, lock)
	installFixture(t, store, "stateful", source)
	installFixture(t, store, "recorder", recorderSource)
	disableFixture(t, store, "recorder")
	installFixture(t, store, "dependent", `export default api=>api.on({id:'required',stage:'responseStart',bodyMode:'none',requires:['stateful/security']},()=>({action:'pass'}));`)
	disableFixture(t, store, "dependent")
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }), time.Second, 4)
	_, headers := getRuntime(t, gateway.URL)
	pid := headers.Get("X-Stateful-Pid")
	if pid == "" {
		t.Fatal("missing PID")
	}
	for n := 0; n < 3; n++ {
		if e = m.Change(context.Background(), "recorder", true); e != nil {
			t.Fatal(e)
		}
		_, h := getRuntime(t, gateway.URL)
		if h.Get("X-Stateful-Pid") != pid || h.Get("X-Generation") != "recorder" {
			t.Fatal(h)
		}
		if e = m.Change(context.Background(), "recorder", false); e != nil {
			t.Fatal(e)
		}
		waitRetired(t, m)
	}
	b, _ := os.ReadFile(setups)
	if strings.TrimSpace(string(b)) != pid {
		t.Fatalf("setup repeated: %q", b)
	}
	if _, e = os.Stat(lock); e != nil {
		t.Fatal("store lock lost", e)
	}
	body, _ := getRuntime(t, fmt.Sprintf("http://127.0.0.1:%d", port))
	if body != "stateful" {
		t.Fatal(body)
	}
	before, _ := os.ReadFile(jobs)
	time.Sleep(60 * time.Millisecond)
	after, _ := os.ReadFile(jobs)
	if len(after) <= len(before) {
		t.Fatal("background job stopped")
	}
	if e = m.Change(context.Background(), "dependent", true); e != nil {
		t.Fatal(e)
	}
	waitRetired(t, m)
	s, _ := m.installer.State()
	gen := m.current.id
	if e = m.Change(context.Background(), "stateful", false); e == nil || !strings.Contains(e.Error(), "requires") {
		t.Fatal("required hook removed", e)
	}
	now, _ := m.installer.State()
	if now.Revision != s.Revision || m.current.id != gen {
		t.Fatal("failed activation committed")
	}
	if e = m.Remove("stateful"); e == nil {
		t.Fatal("active code removed")
	}
	_, h := getRuntime(t, gateway.URL)
	if h.Get("X-Stateful-Pid") != pid {
		t.Fatal("old runtime changed")
	}
}

func TestRuntimePinsStreamsNativeChildrenAndScopedHTTP(t *testing.T) {
	store := t.TempDir()
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	source := fmt.Sprintf(`import fs from 'node:fs';export default api=>{
 api.on({id:'scoped',stage:'request',bodyMode:'none'},async(i,c)=>{
 if(!i.request.url.endsWith('/scoped'))return {action:'pass'};
 fs.writeFileSync(%q,'ready');while(!fs.existsSync(%q)){await new Promise(r=>setTimeout(r,5));if(c.signal.aborted)throw Error('canceled')}
 return {action:'respond',message:await c.http({method:'GET',url:api.config.gatewayOrigin+'/leaf',status:0,header:{},body:''})};});
 api.on({id:'native',stage:'responseStart',bodyMode:'none'},i=>i.request.url.endsWith('/native')&&i.identity.depth===0?{action:'child',request:{method:'GET',url:new URL('/leaf',i.request.url).href,status:0,header:{},body:''}}:{action:'pass'});
 };`, started, release)
	installFixture(t, store, "stateful", source)
	installFixture(t, store, "recorder", recorderSource)
	disableFixture(t, store, "recorder")
	streamRelease := make(chan struct{})
	nativeStarted := make(chan struct{})
	nativeRelease := make(chan struct{})
	var once sync.Once
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: first\n\n")
			w.(http.Flusher).Flush()
			<-streamRelease
			io.WriteString(w, "data: last\n\n")
		case "/native":
			once.Do(func() { close(nativeStarted) })
			<-nativeRelease
			io.WriteString(w, "parent")
		default:
			io.WriteString(w, "leaf")
		}
	}), 2*time.Second, 4)
	type answer struct {
		b string
		h http.Header
		e error
	}
	fetch := func(path string) chan answer {
		ch := make(chan answer, 1)
		go func() {
			r, e := http.Get(gateway.URL + path)
			if e != nil {
				ch <- answer{e: e}
				return
			}
			b, e := io.ReadAll(r.Body)
			r.Body.Close()
			ch <- answer{string(b), r.Header, e}
		}()
		return ch
	}
	scoped := fetch("/scoped")
	waitFile(t, started)
	native := fetch("/native")
	<-nativeStarted
	stream, e := http.Get(gateway.URL + "/stream")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Change(context.Background(), "recorder", true); e != nil {
		t.Fatal(e)
	}
	_, h := getRuntime(t, gateway.URL+"/leaf")
	if h.Get("X-Generation") != "recorder" {
		t.Fatal(h)
	}
	if e = os.WriteFile(release, []byte("go"), 0600); e != nil {
		t.Fatal(e)
	}
	close(nativeRelease)
	for _, ch := range []chan answer{scoped, native} {
		a := <-ch
		if a.e != nil || a.b != "leaf" || a.h.Get("X-Generation") != "" {
			t.Fatalf("descendant crossed generation: %+v", a)
		}
	}
	m.mu.Lock()
	count := len(m.draining)
	m.mu.Unlock()
	if count == 0 {
		t.Fatal("stream lease lost")
	}
	close(streamRelease)
	b, e := io.ReadAll(stream.Body)
	stream.Body.Close()
	if e != nil || !strings.Contains(string(b), "last") {
		t.Fatal(string(b), e)
	}
	waitRetired(t, m)
}

func TestRuntimeForcedDrainAndGenerationBound(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "recorder", recorderSource)
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			io.WriteString(w, "first")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		io.WriteString(w, "ok")
	}), 120*time.Millisecond, 1)
	stream, e := http.Get(gateway.URL + "/stream")
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Body.Close()
	if e = m.Change(context.Background(), "recorder", false); e != nil {
		t.Fatal(e)
	}
	if e = m.Change(context.Background(), "recorder", true); e == nil || !strings.Contains(e.Error(), "limit") {
		t.Fatal(e)
	}
	_, e = io.ReadAll(stream.Body)
	if e == nil {
		t.Fatal("forced stream completed gracefully")
	}
	waitRetired(t, m)
	s, e := m.Status()
	if e != nil || s.Forced != 1 || !strings.Contains(s.Detail, "forced") {
		t.Fatal(s, e)
	}
}

func TestRuntimeSetupAndPersistenceRollback(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "recorder", recorderSource)
	installFixture(t, store, "bad", `export default ()=>{throw Error('setup rejected')}`)
	disableFixture(t, store, "bad")
	statePath := filepath.Join(store, ".enabled.json")
	backup := statePath + ".backup"
	cleaned := filepath.Join(t.TempDir(), "cleaned")
	installFixture(t, store, "persist", fmt.Sprintf(`import fs from 'node:fs';export default api=>{api.cleanup(()=>{fs.rmdirSync(%q);fs.renameSync(%q,%q);fs.writeFileSync(%q,'cleaned')});fs.renameSync(%q,%q);fs.mkdirSync(%q);api.on({id:'prepared',stage:'request',bodyMode:'none'},()=>({action:'pass'}));};`, statePath, backup, statePath, cleaned, statePath, backup, statePath))
	disableFixture(t, store, "persist")
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }), time.Second, 4)
	state, _ := os.ReadFile(filepath.Join(store, ".enabled.json"))
	gen := m.current.id
	if e := m.Change(context.Background(), "bad", true); e == nil {
		t.Fatal("setup accepted")
	}
	after, _ := os.ReadFile(filepath.Join(store, ".enabled.json"))
	if string(after) != string(state) || m.current.id != gen {
		t.Fatal("rollback failed")
	}
	_, h := getRuntime(t, gateway.URL)
	if h.Get("X-Generation") != "recorder" {
		t.Fatal("prior pipeline lost")
	}
	// Inject a rename error only after candidate setup, exercising the actual
	// activation abort path, not just a direct SaveState call. Fixture cleanup
	// removes the fault and restores the untouched prior state bytes.
	if e := m.Change(context.Background(), "persist", true); e == nil {
		t.Fatal("persistence failure published")
	}
	waitFile(t, cleaned)
	after, _ = os.ReadFile(statePath)
	if string(after) != string(state) || m.current.id != gen || m.instances["persist"] != nil {
		t.Fatal("persistence rollback failed")
	}
	_, h = getRuntime(t, gateway.URL)
	if h.Get("X-Generation") != "recorder" {
		t.Fatal("prior pipeline lost")
	}
	status, e := m.Status()
	if e != nil {
		t.Fatal(e)
	}
	failed := false
	for _, row := range status.Rows {
		if row.ID == "persist" {
			failed = strings.Contains(row.Failed, "activation:") && !row.Active
		}
	}
	if !failed {
		t.Fatal("activation diagnostic missing", status)
	}
}

func TestSharedAdmissionAcrossEngines(t *testing.T) {
	slots := make(chan struct{}, 1)
	started := make(chan struct{})
	release := make(chan struct{})
	hook := extension.Registration{ID: "slow", Owner: "test", Stage: extension.Prepare, Handler: func(context.Context, extension.Input) (extension.Result, error) {
		close(started)
		<-release
		return extension.Result{}, nil
	}}
	first, e := extension.New(extension.Config{SharedAdmission: slots}, hook)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	second, e := extension.New(extension.Config{SharedAdmission: slots}, extension.Registration{ID: "other", Owner: "test", Stage: extension.Prepare, Handler: func(context.Context, extension.Input) (extension.Result, error) { return extension.Result{}, nil }})
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})
	done := make(chan struct{})
	go func() {
		r, _ := http.NewRequest("GET", "http://example.test", nil)
		response, _ := first.Transport(base).RoundTrip(r)
		if response != nil {
			response.Body.Close()
		}
		close(done)
	}()
	<-started
	r, _ := http.NewRequest("GET", "http://example.test", nil)
	_, e = second.Transport(base).RoundTrip(r)
	close(release)
	if !errors.Is(e, extension.ErrOverload) {
		t.Fatalf("shared admission bypassed: %v", e)
	}
	<-done
}

func TestRuntimeShutdownBoundsActiveStreamAndReaps(t *testing.T) {
	store := t.TempDir()
	cleaned := filepath.Join(t.TempDir(), "cleaned")
	installFixture(t, store, "stateful", fmt.Sprintf(`import fs from 'node:fs';export default api=>{api.cleanup(()=>fs.writeFileSync(%q,'done'));api.on({id:'security',stage:'request',bodyMode:'none'},()=>({action:'pass'}));};`, cleaned))
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}), 80*time.Millisecond, 4)
	w := m.instances["stateful"].worker
	r, e := http.Get(gateway.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if e = m.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > time.Second {
		t.Fatal("shutdown exceeded finite drain")
	}
	if _, e = io.ReadAll(r.Body); e == nil {
		t.Fatal("shutdown did not interrupt stream")
	}
	waitFile(t, cleaned)
	if !w.Reaped() {
		t.Fatal("shutdown left process unreaped")
	}
	response, e := http.Get(gateway.URL)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 503 {
		t.Fatal("closed runtime admitted ingress", response.StatusCode)
	}
	if e = m.Change(context.Background(), "stateful", true); !errors.Is(e, extension.ErrClosed) {
		t.Fatal("closed runtime mutated", e)
	}
	if e = m.Remove("stateful"); !errors.Is(e, extension.ErrClosed) {
		t.Fatal("closed runtime removed code", e)
	}
}

func TestRuntimeRejectsOverlappingReplacement(t *testing.T) {
	store := t.TempDir()
	dir := t.TempDir()
	lock := filepath.Join(dir, "owner")
	setups := filepath.Join(dir, "setups")
	installFixture(t, store, "stateful", fmt.Sprintf(`import fs from 'node:fs';export default api=>{const fd=fs.openSync(%q,'wx');fs.appendFileSync(%q,process.pid+'\n');api.cleanup(()=>{fs.closeSync(fd);fs.unlinkSync(%q)});api.on({id:'security',stage:'request',bodyMode:'none'},()=>({action:'pass'}));};`, lock, setups, lock))
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			io.WriteString(w, "first")
			w.(http.Flusher).Flush()
			<-release
		}
		io.WriteString(w, "last")
	}), 2*time.Second, 4)
	stream, e := http.Get(gateway.URL + "/stream")
	if e != nil {
		t.Fatal(e)
	}
	defer stream.Body.Close()
	if e = m.Change(context.Background(), "stateful", false); e != nil {
		t.Fatal(e)
	}
	state, _ := os.ReadFile(filepath.Join(store, ".enabled.json"))
	// Exercise the registry guard against a different installed identity. Actual
	// installer replacement is stricter still: an occupied ID is never overwritten.
	path := filepath.Join(store, "stateful", "installation.json")
	record, _ := os.ReadFile(path)
	record = []byte(strings.ReplaceAll(strings.ReplaceAll(string(record), "1.0.0", "2.0.0"), strings.Repeat("a", 40), strings.Repeat("b", 40)))
	if e = os.WriteFile(path, record, 0600); e != nil {
		t.Fatal(e)
	}
	if e = m.Change(context.Background(), "stateful", true); e == nil || !strings.Contains(e.Error(), "overlapping") {
		t.Fatal("overlap accepted", e)
	}
	after, _ := os.ReadFile(filepath.Join(store, ".enabled.json"))
	if string(after) != string(state) {
		t.Fatal("rejected replacement committed")
	}
	b, _ := os.ReadFile(setups)
	if len(strings.Fields(string(b))) != 1 {
		t.Fatal("second setup overlapped", string(b))
	}
	finish()
	io.Copy(io.Discard, stream.Body)
	stream.Body.Close()
	waitRetired(t, m)
	if e = m.Change(context.Background(), "stateful", true); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(setups)
	if len(strings.Fields(string(b))) != 2 {
		t.Fatal("replacement did not start", string(b))
	}
}

func TestRuntimeUpgradePinsUntilConnectionCloses(t *testing.T) {
	store := t.TempDir()
	cleaned := filepath.Join(t.TempDir(), "cleaned")
	installFixture(t, store, "recorder", fmt.Sprintf(`import fs from 'node:fs';export default api=>{api.cleanup(()=>fs.writeFileSync(%q,'done'));api.on({id:'label',stage:'request',bodyMode:'none'},()=>({action:'pass'}));};`, cleaned))
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer c.Close()
		fmt.Fprint(c, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: example\r\nConnection: Upgrade\r\n\r\n")
		io.Copy(c, c)
	}), 2*time.Second, 4)
	c, e := net.DialTimeout("tcp", strings.TrimPrefix(gateway.URL, "http://"), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "GET /upgrade HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: example\r\n\r\n")
	reader := bufio.NewReader(c)
	response, e := http.ReadResponse(reader, nil)
	if e != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", response, e)
	}
	if e = m.Change(context.Background(), "recorder", false); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(cleaned); !os.IsNotExist(e) {
		t.Fatal("cleanup before upgrade closes", e)
	}
	if _, e = c.Write([]byte("echo")); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 4)
	if _, e = io.ReadFull(reader, b); e != nil || string(b) != "echo" {
		t.Fatal(string(b), e)
	}
	c.Close()
	waitFile(t, cleaned)
	waitRetired(t, m)
}

func TestForcedCancellationOfSharedWorkerFailsClosed(t *testing.T) {
	store := t.TempDir()
	started := filepath.Join(t.TempDir(), "started")
	installFixture(t, store, "stateful", fmt.Sprintf(`import fs from 'node:fs';export default api=>api.on({id:'security',stage:'request',bodyMode:'none'},async i=>{if(i.request.url.endsWith('/hang')){fs.writeFileSync(%q,'started');await new Promise(()=>{})};return {action:'pass'}});`, started))
	installFixture(t, store, "recorder", recorderSource)
	disableFixture(t, store, "recorder")
	m, gateway := runtimeProxy(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }), 80*time.Millisecond, 1)
	m.options.Cleanup = 10 * time.Millisecond
	done := make(chan struct{})
	go func() {
		r, e := http.Get(gateway.URL + "/hang")
		if e == nil {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		close(done)
	}()
	waitFile(t, started)
	if e := m.Change(context.Background(), "recorder", true); e != nil {
		t.Fatal(e)
	}
	<-done
	time.Sleep(120 * time.Millisecond)
	if e := m.Change(context.Background(), "recorder", false); e == nil || !strings.Contains(e.Error(), "limit") {
		t.Fatal("unsettled generation escaped the bound", e)
	}
	waitRetired(t, m)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		health := m.instances["stateful"].worker.Health()
		m.mu.Unlock()
		if health != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, e := http.Get(gateway.URL + "/safe")
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, r.Body)
	r.Body.Close()
	if r.StatusCode != 502 {
		t.Fatalf("failed security worker bypassed: %d", r.StatusCode)
	}
	status, e := m.Status()
	if e != nil {
		t.Fatal(e)
	}
	failed := false
	for _, r := range status.Rows {
		if r.ID == "stateful" {
			failed = r.Failed != "" && r.Active && r.Enabled == "true"
		}
	}
	if !failed {
		t.Fatal(status)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
