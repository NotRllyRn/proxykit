package nodehost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/proxy"
	"github.com/NotRllyRn/proxykit/worker"
)

func TestExactResultAndMessageKeys(t *testing.T) {
	for _, raw := range []string{
		`{"Action":"pass"}`, `{"action":"pass","Action":"pass"}`,
		`{"action":"pass","Metadata":{"x":null}}`,
		`{"action":"pass","metadata":{},"Metadata":{"x":null}}`,
		`{"action":"pass","Credentials":{"Bad Name":["a\r\nb"]}}`,
		`{"action":"pass","credentials":{},"Credentials":{"x":[null]}}`,
		`{"action":"replace","Message":{}}`,
		`{"action":"child","Request":{}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := result(json.RawMessage(raw), worker.HookDescriptor{Stage: "resolve", BodyMode: "none"}, 1024, 1024); err == nil {
				t.Fatal("accepted alias")
			}
		})
	}
	for _, field := range []string{"method", "url", "header", "status", "body"} {
		for _, mixed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mixed=%v", field, mixed), func(t *testing.T) {
				m := map[string]any{"method": "GET", "url": "http://example.test/", "header": map[string]any{}, "status": 0, "body": ""}
				alias := strings.ToUpper(field[:1]) + field[1:]
				m[alias] = m[field]
				if field == "header" {
					m[alias] = map[string]any{"Bad Name": []any{"a\r\nb"}}
				}
				if !mixed {
					delete(m, field)
				}
				b, _ := json.Marshal(m)
				if _, err := decodeMessage(b, true, 1024); err == nil {
					t.Fatalf("accepted alias: %s", b)
				}
			})
		}
	}
}

func TestInstalledRequestOnlyAndNoHookUpgrade(t *testing.T) {
	for _, source := range []string{
		`export default api=>api.on({id:'request',stage:'request',bodyMode:'none'},i=>{if(i.request.header.Upgrade?.[0] !== 'example') throw Error('missing upgrade');return {action:'pass'}});`,
		`export default api=>{};`,
	} {
		t.Run(source, func(t *testing.T) {
			store := t.TempDir()
			installFixture(t, store, "upgrade", source)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, b, err := w.(http.Hijacker).Hijack()
				if err != nil {
					return
				}
				defer c.Close()
				fmt.Fprint(b, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: example\r\n\r\nhello")
				b.Flush()
				io.Copy(c, c)
			}))
			defer upstream.Close()
			gateway := httptest.NewUnstartedServer(nil)
			h, err := Open(context.Background(), Options{Store: store, Gateway: "http://" + gateway.Listener.Addr().String()})
			if err != nil {
				t.Fatal(err)
			}
			defer closeHost(t, h)
			p, err := proxy.NewWithEngine(upstream.URL, h.Engine)
			if err != nil {
				t.Fatal(err)
			}
			gateway.Config.Handler = p
			gateway.Start()
			defer gateway.Close()
			c, err := net.Dial("tcp", gateway.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(3 * time.Second))
			fmt.Fprint(c, "GET / HTTP/1.1\r\nHost: example.test\r\nConnection: Upgrade\r\nUpgrade: example\r\n\r\n")
			br := bufio.NewReader(c)
			resp, err := http.ReadResponse(br, nil)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 101 {
				t.Fatal(resp.StatusCode)
			}
			hello := make([]byte, 5)
			if _, err := io.ReadFull(br, hello); err != nil || string(hello) != "hello" {
				t.Fatalf("upstream greeting: %q %v", hello, err)
			}
			for _, payload := range []string{"ping", "another message"} {
				fmt.Fprint(c, payload)
				b := make([]byte, len(payload))
				if _, err := io.ReadFull(br, b); err != nil || string(b) != payload {
					t.Fatalf("bidirectional: %q %v", b, err)
				}
			}
		})
	}
}
