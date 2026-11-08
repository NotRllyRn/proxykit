package nodehost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/proxy"
	"github.com/NotRllyRn/proxykit/worker"
)

func installFixture(t *testing.T, store, id, source string) {
	t.Helper()
	dir := filepath.Join(store, id, "package")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := extensions.Manifest{ManifestVersion: 1, ID: id, Version: "1.0.0", Entry: "main.mjs"}
	m, _ := json.Marshal(manifest)
	record, _ := json.Marshal(extensions.Installation{Manifest: manifest, Source: "https://github.com/example/repository", Commit: strings.Repeat("a", 40)})
	for path, b := range map[string][]byte{filepath.Join(dir, "main.mjs"): []byte(source), filepath.Join(dir, "proxykit-extension.json"): m, filepath.Join(store, id, "installation.json"): record} {
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func closeHost(t *testing.T, h *Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.Close(ctx); err != nil {
		t.Logf("worker shutdown (crash fixtures deliberately fail): %v", err)
	}
}
func openProxy(t *testing.T, store string, cfg extension.Config, auth bool, capacities ...int) (*httptest.Server, *Host, *atomic.Int32) {
	t.Helper()
	return openProxyWithUpstream(t, store, cfg, auth, nil, capacities...)
}
func openProxyWithUpstream(t *testing.T, store string, cfg extension.Config, auth bool, inspect func(*http.Request), capacities ...int) (*httptest.Server, *Host, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if inspect != nil {
			inspect(r)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Upstream", "original")
		_, _ = io.WriteString(w, "upstream")
	}))
	t.Cleanup(upstream.Close)
	gateway := httptest.NewUnstartedServer(nil)
	origin := "http://" + gateway.Listener.Addr().String()
	capacity := 4
	if len(capacities) > 0 {
		capacity = capacities[0]
	}
	h, err := Open(context.Background(), Options{Store: store, Gateway: origin, Engine: cfg, Worker: worker.Options{Stderr: os.Stderr, Concurrency: capacity, CallTimeout: 2 * time.Second, Config: json.RawMessage(`{"gatewayOrigin":` + mustJSON(origin) + `}`)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeHost(t, h) })
	p, err := proxy.NewWithEngine(upstream.URL, h.Engine)
	if err != nil {
		t.Fatal(err)
	}
	gateway.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth {
			bound, err := h.Engine.Bind(r)
			if err != nil {
				http.Error(w, err.Error(), 403)
				return
			}
			r = bound
			if _, ok := extension.AuthenticationFromContext(r.Context()); !ok {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					http.Error(w, "authentication required", 401)
					return
				}
				r = r.WithContext(extension.WithAuthentication(r.Context(), extension.Authentication{Subject: "Example user", Claims: map[string]string{"tier": "test"}}))
			}
			r.Header.Del("Authorization")
		}
		p.ServeHTTP(w, r)
	})
	gateway.Start()
	t.Cleanup(gateway.Close)
	return gateway, h, &calls
}
func get(t *testing.T, url string) (int, string) {
	t.Helper()
	c := http.Client{Timeout: 4 * time.Second}
	r, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return r.StatusCode, string(b)
}

func TestInstalledWholeOriginalLocalAndDelivery(t *testing.T) {
	store := t.TempDir()
	evidence := filepath.Join(t.TempDir(), "evidence")
	source := `import {appendFileSync} from 'node:fs'; export default api=>{
 api.on({id:'local',stage:'request',bodyMode:'none'}, i=>i.request.url.endsWith('/local')?{action:'respond',message:{method:'',url:'',status:201,header:{'Content-Type':['text/plain']},body:Buffer.from('local').toString('base64')}}:{action:'pass'});
 api.on({id:'observe',stage:'observe',bodyMode:'none'},i=>{if(i.response.header['X-Upstream']?.[0]!=='original' && i.response.status!==201)throw Error('original facts');return {action:'pass'}});
 api.on({id:'whole',stage:'responseBody',bodyMode:'buffered'},i=>({action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(i.response.body,'base64').toString()+' changed').toString('base64')}}));
 api.on({id:'delivery',stage:'delivery',bodyMode:'none'},(i,c)=>{if(c.http)throw Error('delivery helper');appendFileSync(PATH,JSON.stringify(i)+'\n');return {action:'pass'}});
 };`
	source = strings.ReplaceAll(source, "PATH", mustJSON(evidence))
	installFixture(t, store, "example-user", source)
	server, _, calls := openProxy(t, store, extension.Config{}, false)
	if status, body := get(t, server.URL+"/upstream"); status != 200 || body != "upstream changed" {
		t.Fatalf("%d %q", status, body)
	}
	if status, body := get(t, server.URL+"/local"); status != 201 || body != "local changed" {
		t.Fatalf("%d %q", status, body)
	}
	if calls.Load() != 1 {
		t.Fatal("local dispatch reached upstream")
	}
	deadline := time.Now().Add(time.Second)
	var lines []string
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(evidence)
		lines = strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(lines) == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(lines) != 2 {
		t.Fatalf("missing delivery: %v", lines)
	}
	var input Input
	if err := json.Unmarshal([]byte(lines[0]), &input); err != nil {
		t.Fatal(err)
	}
	if input.Delivery == nil || input.Delivery.Bytes != 16 || !input.Delivery.Returned || input.Response.Body != nil || input.Identity.Root == "" {
		t.Fatalf("bad evidence %+v", input)
	}
}
func mustJSON(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestOptionalCrashPreservesLastAcceptedWholeAndRequiredBlocks(t *testing.T) {
	for _, failure := range []string{"failOpen", "block"} {
		t.Run(failure, func(t *testing.T) {
			store := t.TempDir()
			installFixture(t, store, "first", `export default api=>api.on({id:'edit',stage:'responseBody',bodyMode:'buffered',order:-7},i=>({action:'replace',message:{...i.response,body:Buffer.from('accepted').toString('base64')}}));`)
			installFixture(t, store, "second", `export default api=>api.on({id:'crash',stage:'responseBody',bodyMode:'buffered',order:4,failure:'`+failure+`'},()=>process.exit(9));`)
			server, _, _ := openProxy(t, store, extension.Config{}, false)
			status, body := get(t, server.URL)
			if failure == "failOpen" && (status != 200 || body != "accepted") {
				t.Fatalf("%d %q", status, body)
			}
			if failure == "block" && status != 502 {
				t.Fatalf("required worker failure allowed %d %q", status, body)
			}
		})
	}
}
func TestOrdinaryHookErrorRecoveryAndRequiredDenial(t *testing.T) {
	for _, failure := range []string{"failOpen", "block"} {
		t.Run(failure, func(t *testing.T) {
			store := t.TempDir()
			installFixture(t, store, "example-user", `export default api=>{
 api.on({id:'accepted',stage:'responseBody',bodyMode:'buffered',order:-1},i=>({action:'replace',message:{...i.response,body:Buffer.from('accepted').toString('base64')}}));
 api.on({id:'error',stage:'responseBody',bodyMode:'buffered',failure:'`+failure+`'},i=>{if(i.request.url.endsWith('/fail'))throw Error('😀\\\"\u0000'.repeat(512));return {action:'pass'}});
};`)
			server, _, calls := openProxy(t, store, extension.Config{}, false)
			status, body := get(t, server.URL+"/fail")
			if failure == "failOpen" && (status != 200 || body != "accepted") {
				t.Fatalf("optional error lost accepted input: %d %q", status, body)
			}
			if failure == "block" && status != 502 {
				t.Fatalf("required error allowed response: %d %q", status, body)
			}
			if status, body := get(t, server.URL+"/fresh"); status != 200 || body != "accepted" {
				t.Fatalf("worker did not survive ordinary error: %d %q", status, body)
			}
			if calls.Load() != 2 {
				t.Fatalf("upstream calls: %d", calls.Load())
			}
		})
	}
}

func TestOpaqueStreamRecoveryAndSequence(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "first", `export default api=>{const seen=new Map();api.on({id:'edit',stage:'responseBody',bodyMode:'stream',order:-1},i=>{const s=i.stream;if(s.end || s.sequence!==(seen.get(s.streamId)??0))throw Error('sequence');seen.set(s.streamId,s.sequence+1);return {action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(s.data,'base64').toString().toUpperCase()).toString('base64')}}});};`)
	installFixture(t, store, "second", `export default api=>api.on({id:'crash',stage:'responseBody',bodyMode:'stream',order:2,failure:'failOpen'},()=>process.exit(9));`)
	server, h, _ := openProxy(t, store, extension.Config{UnitBytes: 3}, false)
	for n := 0; n < 2; n++ {
		if status, body := get(t, server.URL); status != 200 || body != "UPSTREAM" {
			t.Fatalf("%d %q", status, body)
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		n := len(h.streams)
		h.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("stream bookkeeping not retired by Finish")
}
func TestManagedHTTPAuthenticationFullChainAndOneSlotRejection(t *testing.T) {
	for _, limits := range []struct {
		name              string
		admission, worker int
	}{{"full-chain", 8, 4}, {"native-one-slot", 1, 4}, {"worker-one-slot", 8, 1}} {
		t.Run(limits.name, func(t *testing.T) {
			admission := limits.admission
			store := t.TempDir()
			installFixture(t, store, "example-user", `export default api=>{
 api.on({id:'managed',stage:'request',bodyMode:'none'},async(i,c)=>{
  if(!i.request.url.endsWith('/outer'))return {action:'pass'};
  if(i.auth.subject!=='Example user' || i.auth.claims.tier!=='test')throw Error('missing trusted authentication');
  const origin=api.config.gatewayOrigin;
  const r=await c.http({method:'GET',url:origin+'/inner',status:0,header:{},body:''});
  return {action:'respond',message:r};
 });
 api.on({id:'decorate',stage:'responseBody',bodyMode:'buffered'},i=>({action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(i.response.body,'base64').toString()+'!').toString('base64')}}));
 };`)
			server, h, calls := openProxy(t, store, extension.Config{Admission: admission}, true, limits.worker)
			req, _ := http.NewRequest("GET", server.URL+"/outer", nil)
			req.Header.Set("Authorization", "Bearer fixture")
			c := http.Client{Timeout: 4 * time.Second}
			resp, err := c.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if admission == 8 && limits.worker > 1 && (resp.StatusCode != 200 || string(b) != "upstream!!" || calls.Load() != 1) {
				t.Fatalf("managed chain %d %q calls=%d", resp.StatusCode, b, calls.Load())
			}
			if (admission == 1 || limits.worker == 1) && (resp.StatusCode != 502 || calls.Load() != 0) {
				t.Fatalf("one-slot did not reject %d %q", resp.StatusCode, b)
			}
			forged, _ := http.NewRequest("GET", server.URL+"/inner", nil)
			forged.Header.Set("X-Proxykit-Capability", "forged")
			fr, err := c.Do(forged)
			if err != nil {
				t.Fatal(err)
			}
			fr.Body.Close()
			if fr.StatusCode != 403 {
				t.Fatal("forged scope accepted")
			}
			if _, err := h.Engine.Client(context.Background(), server.URL); err == nil {
				t.Fatal("unscoped client accepted")
			}
			if status, _ := get(t, server.URL+"/inner"); status != 401 {
				t.Fatal("implicit auth bypass")
			}
		})
	}
}
func TestStartupValidationRollbackAndOrdering(t *testing.T) {
	cases := []string{
		`{id:'bad',stage:'nonsense',bodyMode:'none'}`,
		`{id:'bad',stage:'observe',bodyMode:'buffered'}`,
		`{id:'bad',stage:'request',bodyMode:'none',requires:['missing']}`,
		`{id:'bad',stage:'request',bodyMode:'none',after:['bad']}`,
		`{id:'bad',stage:'request',bodyMode:'none',formats:['not mime']}`,
	}
	for _, descriptor := range cases {
		t.Run(descriptor, func(t *testing.T) {
			store := t.TempDir()
			closed := filepath.Join(t.TempDir(), "closed")
			installFixture(t, store, "first", `import {writeFileSync} from 'node:fs';export default api=>{api.cleanup(()=>writeFileSync(`+mustJSON(closed)+`, 'closed'));api.on({id:'ok',stage:'request',bodyMode:'none'},()=>({action:'pass'}))};`)
			installFixture(t, store, "second", `export default api=>api.on(`+descriptor+`,()=>({action:'pass'}));`)
			h, err := Open(context.Background(), Options{Store: store, Gateway: "http://127.0.0.1:1"})
			if err == nil {
				closeHost(t, h)
				t.Fatal("accepted invalid descriptors")
			}
			if _, err := os.Stat(closed); err != nil {
				t.Fatalf("startup rollback did not run cleanup: %v", err)
			}
		})
	}
	t.Run("numeric and dependency order", func(t *testing.T) {
		store := t.TempDir()
		installFixture(t, store, "example-user", `export default api=>{
  api.on({id:'a',stage:'responseBody',bodyMode:'buffered',order:10},i=>({action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(i.response.body,'base64').toString()+'a').toString('base64')}}));
  api.on({id:'z',stage:'responseBody',bodyMode:'buffered',order:-3},i=>({action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(i.response.body,'base64').toString()+'z').toString('base64')}}));
  };`)
		server, _, _ := openProxy(t, store, extension.Config{}, false)
		if _, body := get(t, server.URL); body != "upstreamza" {
			t.Fatal(body)
		}
	})
	t.Run("contradictory numeric order", func(t *testing.T) {
		store := t.TempDir()
		installFixture(t, store, "example-user", `export default api=>{api.on({id:'a',stage:'request',bodyMode:'none',order:1,after:['b']},()=>({action:'pass'}));api.on({id:'b',stage:'request',bodyMode:'none',order:2},()=>({action:'pass'}))};`)
		h, err := Open(context.Background(), Options{Store: store, Gateway: "http://127.0.0.1:1"})
		if err == nil {
			closeHost(t, h)
			t.Fatal("discarded numeric ordering")
		}
	})
}
func TestChildUsesFullChainAndCredentialsStaySeparate(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "child-route", `export default api=>{
 api.on({id:'credential',stage:'resolve',bodyMode:'none'},()=>({action:'pass',credentials:{Authorization:['Bearer example-upstream']}}));
 api.on({id:'check',stage:'upstream',bodyMode:'none'},i=>{if(i.request.header.Authorization || i.credentials)throw Error('credential snapshot leak');return {action:'pass'}});
 api.on({id:'child',stage:'responseStart',bodyMode:'none'},i=>i.request.url.endsWith('/outer')?{action:'child',request:{method:'GET',url:i.request.url.replace('/outer','/inner'),header:{},status:0,body:''}}:{action:'pass'});
 api.on({id:'body',stage:'responseBody',bodyMode:'buffered'},i=>({action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(i.response.body,'base64').toString()+'!').toString('base64')}}));
}`)
	credentials := make(chan string, 2)
	s, _, _ := openProxyWithUpstream(t, store, extension.Config{}, false, func(r *http.Request) {
		credentials <- r.URL.Path + ":" + r.Header.Get("Authorization")
	})
	r, err := http.Get(s.URL + "/outer")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil || r.StatusCode != 200 || string(b) != "upstream!!" {
		t.Fatalf("child chain: %d %q %v", r.StatusCode, b, err)
	}
	for _, path := range []string{"/outer", "/inner"} {
		select {
		case got := <-credentials:
			if want := path + ":Bearer example-upstream"; got != want {
				t.Fatalf("actual upstream credential: got %q, want %q", got, want)
			}
		default:
			t.Fatalf("missing actual upstream request for %s", path)
		}
	}
}

func TestStrictResultBoundsAndActions(t *testing.T) {
	d := worker.HookDescriptor{Stage: "responseBody", BodyMode: "buffered"}
	for _, raw := range []string{`null`, `{}`, `{"action":"pass","unknown":1}`, `{"action":"pass","action":"replace"}`, `{"action":"replace","message":{"method":"","url":"","header":{},"status":200,"body":"YQ"}}`, `{"action":"pass","metadata":null}`, `{"action":"pass","metadata":{"x":null}}`, `{"action":"replace","message":{"method":"","url":"","header":{"X":[null]},"status":200,"body":"YQ=="}}`, `{"action":"replace","message":{"method":"","url":"","header":{"X":["a\r\nb"]},"status":200,"body":"YQ=="}}`, `{"action":"replace","message":{"method":"","url":"","header":{"X":null},"status":200,"body":"YQ=="}}`, `{"action":"replace","message":{"method":"","url":"","header":{},"status":200,"body":"YWJjZA=="}}`} {
		if _, err := result([]byte(raw), d, 3, 2); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
