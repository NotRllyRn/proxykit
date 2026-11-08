package aihost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/recall"
	"github.com/NotRllyRn/proxykit/extension"
)

func TestSocketReauthorizesPreviouslySelectedContinuation(t *testing.T) {
	var upstreamCalls, authorizationChecks atomic.Int32
	var revoked atomic.Bool
	upstream := mixedUpstream(t, &upstreamCalls)
	defer upstream.Close()
	app := testApp(t.TempDir(), upstream.URL)
	policy := extension.Registration{Owner: "approval", ID: "approval.authorize", Stage: extension.Resolve, Handler: func(_ context.Context, in extension.Input) (extension.Result, error) {
		if in.Identity.Depth > 0 {
			authorizationChecks.Add(1)
			if revoked.Load() {
				return extension.Result{}, errors.New("destination authorization revoked")
			}
		}
		return extension.Result{}, nil
	}}
	server := gateway(t, app, upstream.URL, policy)
	defer server.Close()
	status, _, wire := post(t, server.URL, clientWire(t, []ai.Message{{Role: "user", Text: "original"}}, false))
	if status != http.StatusOK {
		t.Fatalf("handoff %d %s", status, wire)
	}
	st, e := app.Store.Read(context.Background(), recall.Key{Tenant: "personal", Conversation: "conversation"})
	if e != nil {
		t.Fatal(e)
	}
	replay := append(st.Turn.Public, ai.Message{Role: "tool", ToolID: "client-id", Text: "client result"})
	revoked.Store(true)
	req := clientWire(t, replay, false)
	request, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", bytes.NewReader(req))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Recall-Revision", strconv.FormatUint(st.Revision, 10))
	response, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	if response.StatusCode < 400 || upstreamCalls.Load() != 2 || authorizationChecks.Load() != 3 {
		t.Fatalf("authorization reused: status=%d upstream=%d checks=%d", response.StatusCode, upstreamCalls.Load(), authorizationChecks.Load())
	}
}
