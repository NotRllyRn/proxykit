package nodehost

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/worker"
)

func TestInstalledRequestHasTrailers(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "trailer-facts", `export default api => {
 api.on({id:'spoof',stage:'request',bodyMode:'none'}, i => {
   if (!Object.hasOwn(i, 'requestHasTrailers') || typeof i.requestHasTrailers !== 'boolean') throw Error('missing boolean');
   if (Object.keys(i.requested.header).some(k => k.toLowerCase() === 'trailer')) throw Error('unexpected Trailer header');
   const original = i.requestHasTrailers;
   i.requestHasTrailers = !original;
   return {action:'replace',message:{...i.request,header:{...i.request.header,Trailer:['X-Spoof'],RequestHasTrailers:[String(!original)]}},metadata:{requestHasTrailers:String(!original)}};
 });
 api.on({id:'check',stage:'resolve',bodyMode:'none'}, i => {
   if (!Object.hasOwn(i, 'requestHasTrailers') || typeof i.requestHasTrailers !== 'boolean') throw Error('missing boolean');
   if (i.metadata.requestHasTrailers !== String(!i.requestHasTrailers) || i.request.header.Trailer?.[0] !== 'X-Spoof') throw Error('spoof fixture');
   return {action:'respond',message:{method:'',url:'',status:200,header:{'Content-Type':['application/json']},body:Buffer.from(JSON.stringify({requestHasTrailers:i.requestHasTrailers})).toString('base64')}};
 });
};`)
	server, _, calls := openProxy(t, store, extension.Config{}, false)
	for _, tc := range []struct {
		name    string
		trailer http.Header
		want    string
	}{
		{name: "nil", want: `{"requestHasTrailers":false}`},
		{name: "empty", trailer: http.Header{}, want: `{"requestHasTrailers":false}`},
		{name: "declared", trailer: http.Header{"X-Checksum": nil}, want: `{"requestHasTrailers":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Go's server populates Request.Trailer and removes the Trailer
			// header before Bind. The installed worker must see the host fact.
			r, err := http.NewRequest("POST", server.URL+"/"+tc.name, strings.NewReader("example"))
			if err != nil {
				t.Fatal(err)
			}
			r.Trailer = tc.trailer
			r.ContentLength = -1
			r.Header.Set("RequestHasTrailers", "true")
			client := &http.Client{Timeout: 4 * time.Second}
			resp, err := client.Do(r)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 || string(body) != tc.want {
				t.Fatalf("status %d, body %s; want %s", resp.StatusCode, body, tc.want)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("local response reached upstream")
	}
}

func TestRequestHasTrailersNotWritable(t *testing.T) {
	for _, raw := range []string{
		`{"action":"pass","requestHasTrailers":true}`,
		`{"action":"pass","requestHasTrailers":false}`,
		`{"action":"replace","message":{"method":"POST","url":"http://example.test/","header":{},"status":0,"requestHasTrailers":true}}`,
		`{"action":"child","request":{"method":"POST","url":"http://example.test/","header":{},"status":0,"body":"","requestHasTrailers":true}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := result(json.RawMessage(raw), worker.HookDescriptor{Stage: "responseStart", BodyMode: "none"}, 1024, 1024); err == nil {
				t.Fatal("accepted writable requestHasTrailers")
			}
		})
	}
}
