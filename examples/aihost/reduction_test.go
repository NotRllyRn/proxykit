package aihost

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
)

func TestSocketReductionArchivesBeforeChangingPrivateContext(t *testing.T) {
	var mu sync.Mutex
	var actual [][]byte
	var calls atomic.Int32
	upstream := mixedUpstream(t, &calls, func(body []byte) { mu.Lock(); actual = append(actual, body); mu.Unlock() })
	defer upstream.Close()
	app := testApp(t.TempDir(), upstream.URL)
	app.Reduce = func(original []ai.Message) ([]ai.Message, error) {
		st, e := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
		if e != nil || len(st.Archive) != 1 || st.Archive[0][0].Text != "original public history" {
			t.Errorf("reduction ran without durable archive: %+v %v", st, e)
		}
		return []ai.Message{{Role: "user", Text: "private summary"}}, nil
	}
	server := gateway(t, app, upstream.URL)
	defer server.Close()
	status, _, wire := post(t, server.URL, clientWire(t, []ai.Message{{Role: "user", Text: "original public history"}}, false))
	if status != 200 {
		t.Fatalf("reduction %d %s", status, wire)
	}
	st, e := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	if e != nil || st.Turn.Public[0].Text != "original public history" || st.Turn.Working[0].Text != "private summary" {
		t.Fatalf("public/private conflated: %+v %v", st, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(actual) != 2 {
		t.Fatalf("upstream calls %d", len(actual))
	}
	for _, body := range actual {
		if !bytes.Contains(body, []byte("private summary")) || bytes.Contains(body, []byte("original public history")) {
			t.Fatalf("upstream not reduced: %s", body)
		}
	}
}
