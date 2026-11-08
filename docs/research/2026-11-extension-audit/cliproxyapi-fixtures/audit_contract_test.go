package pluginhost

import (
	"context"
	"errors"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"testing"
	"time"
)

func TestAuditRequiredInterceptorErrorDoesNotBlockOrRollbackSibling(t *testing.T) {
	called := false
	h := newHostWithRecords(
		capabilityRecord{id: "first", priority: 30, plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{RequestInterceptor: requestInterceptorFunc(func(context.Context, pluginapi.RequestInterceptRequest) (pluginapi.RequestInterceptResponse, error) {
			return pluginapi.RequestInterceptResponse{Body: []byte(`{"compacted":true}`)}, nil
		})}}},
		capabilityRecord{id: "policy", priority: 20, plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{RequestInterceptor: requestInterceptorFunc(func(context.Context, pluginapi.RequestInterceptRequest) (pluginapi.RequestInterceptResponse, error) {
			return pluginapi.RequestInterceptResponse{Terminate: true, StatusCode: 403}, errors.New("archive unavailable")
		})}}},
		capabilityRecord{id: "observer", priority: 10, plugin: pluginapi.Plugin{Capabilities: pluginapi.Capabilities{RequestInterceptor: requestInterceptorFunc(func(_ context.Context, r pluginapi.RequestInterceptRequest) (pluginapi.RequestInterceptResponse, error) {
			called = true
			if string(r.Body) != `{"compacted":true}` {
				t.Errorf("observer body=%s", r.Body)
			}
			return pluginapi.RequestInterceptResponse{}, nil
		})}}})
	out := h.InterceptRequestBeforeAuth(context.Background(), pluginapi.RequestInterceptRequest{Body: []byte(`{"original":true}`)})
	if out.Terminate || !called || string(out.Body) != `{"compacted":true}` {
		t.Fatalf("unexpected result: %+v observer=%v", out, called)
	}
	t.Log("policy error ignored; sibling compaction retained; downstream observer sees modified body")
}

type auditBlockingClient struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (c *auditBlockingClient) Call(context.Context, string, []byte) ([]byte, error) {
	c.started <- struct{}{}
	<-c.release
	c.finished <- struct{}{}
	return nil, nil
}
func (c *auditBlockingClient) Shutdown() {}
func TestAuditCancellationReturnsWhileNativeWorkRemainsAndConcurrentCallsEnter(t *testing.T) {
	const n = 24
	inner := &auditBlockingClient{make(chan struct{}, n), make(chan struct{}), make(chan struct{}, n)}
	c := newGuardedPluginClient(inner)
	defer c.Shutdown()
	defer close(inner.release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { _, err := c.Call(ctx, "audit", nil); done <- err }()
	}
	for i := 0; i < n; i++ {
		select {
		case <-inner.started:
		case <-time.After(3 * time.Second):
			t.Fatal("calls serialized or blocked")
		}
	}
	cancel()
	for i := 0; i < n; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("cancellation blocked")
		}
	}
	select {
	case <-inner.finished:
		t.Fatal("inner work unexpectedly finished")
	default:
	}
	t.Log("24 concurrent inner calls admitted; cancellation returned to every caller while inner work remained blocked")
}
