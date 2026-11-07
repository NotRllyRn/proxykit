package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startProxy(t testing.TB, upstream string, configure func(*httputil.ReverseProxy)) *httptest.Server {
	t.Helper()
	handler, err := New(upstream)
	if err != nil {
		t.Fatal(err)
	}
	handler.ErrorLog = log.New(io.Discard, "", 0)
	transport := handler.Transport.(*http.Transport)
	t.Cleanup(transport.CloseIdleConnections)
	if configure != nil {
		configure(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestInvalidUpstream(t *testing.T) {
	for _, upstream := range []string{"", "/relative", "ftp://example.com", "http://", "http://user:password@example.com", "https://example.com/#fragment", "http://%"} {
		t.Run(upstream, func(t *testing.T) {
			if _, err := New(upstream); err == nil {
				t.Fatal("accepted invalid upstream")
			}
		})
	}
}

func TestForwardingRegression(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PATCH" || r.URL.RequestURI() != "/base/a%2Fb?fixed=1&q=a%2Bb&q=2" || string(body) != "request" {
			t.Errorf("unexpected request: %s %s %q", r.Method, r.URL.RequestURI(), body)
		}
		if r.Host != r.Context().Value(http.LocalAddrContextKey).(net.Addr).String() {
			t.Error("host not rewritten")
		}
		for _, header := range []string{"X-Hop", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
			if r.Header.Get(header) != "" {
				t.Errorf("leaked %s", header)
			}
		}
		if r.Header.Get("Authorization") != "Bearer example" {
			t.Error("lost authorization")
		}
		w.Header().Set("Connection", "X-Response-Hop")
		w.Header().Set("X-Response-Hop", "remove")
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Set("Trailer", "X-Checksum")
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "response")
		w.Header().Set("X-Checksum", "complete")
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL+"/base?fixed=1", nil)
	request, _ := http.NewRequest("PATCH", server.URL+"/a%2Fb?q=a%2Bb&q=2", strings.NewReader("request"))
	request.Host = "original.example"
	request.Header.Set("Authorization", "Bearer example")
	request.Header.Set("Connection", "X-Hop")
	for _, header := range []string{"X-Hop", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		request.Header.Set(header, "untrusted")
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 418 || string(body) != "response" || response.Header.Get("X-Response-Hop") != "" || len(response.Header.Values("Set-Cookie")) != 2 || response.Trailer.Get("X-Checksum") != "complete" {
		t.Fatalf("unexpected response: %+v body=%q err=%v", response, body, err)
	}
}

func TestCompressedBodyPreserved(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte("example payload"))
	_ = writer.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "" {
			t.Error("proxy injected compression negotiation")
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(body, compressed.Bytes()) || response.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("compressed representation changed")
	}
}

func TestStreamingAndCancellation(t *testing.T) {
	for _, contentType := range []string{"text/event-stream", "application/octet-stream"} {
		t.Run(contentType, func(t *testing.T) {
			cancelled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType)
				_, _ = io.WriteString(w, "first\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(cancelled)
			}))
			defer upstream.Close()
			server := startProxy(t, upstream.URL, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			line, err := bufio.NewReader(response.Body).ReadString('\n')
			if err != nil || line != "first\n" {
				t.Fatalf("stream stalled: %q %v", line, err)
			}
			cancel()
			select {
			case <-cancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("cancellation not propagated")
			}
		})
	}
}

func TestStreamingUpload(t *testing.T) {
	first := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := make([]byte, 5)
		if _, err := io.ReadFull(r.Body, buffer); err != nil || string(buffer) != "first" {
			t.Error("missing initial upload chunk")
		}
		close(first)
		rest, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append(buffer, rest...))
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL, reader)
	done := make(chan error, 1)
	go func() {
		response, err := server.Client().Do(request)
		if err == nil {
			defer response.Body.Close()
			body, readErr := io.ReadAll(response.Body)
			err = readErr
			if string(body) != "firstlast" {
				t.Errorf("unexpected upload %q", body)
			}
		}
		done <- err
	}()
	_, _ = io.WriteString(writer, "first")
	select {
	case <-first:
	case <-ctx.Done():
		t.Fatal("upload buffered until completion")
	}
	_, _ = io.WriteString(writer, "last")
	_ = writer.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 502 {
		t.Fatalf("got %d", response.StatusCode)
	}
}

func TestTruncatedResponseRegression(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "partial")
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	response, err := server.Client().Get(server.URL)
	if err != nil {
		return
	} // A reset before headers also correctly reports failure.
	defer response.Body.Close()
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("truncated response appeared complete")
	}
}

func TestConnectionReuse(t *testing.T) {
	var connections atomic.Int64
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	for range 3 {
		response, err := server.Client().Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}
	if connections.Load() != 1 {
		t.Fatalf("opened %d upstream connections", connections.Load())
	}
}

func TestNativeTransformComposition(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PUT" || r.Header.Get("X-Example") != "modified" || string(body) != "replacement" {
			t.Error("request transformation failed")
		}
		_, _ = io.WriteString(w, "original")
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, func(handler *httputil.ReverseProxy) {
		rewrite := handler.Rewrite
		handler.Rewrite = func(request *httputil.ProxyRequest) {
			rewrite(request)
			request.Out.Method = "PUT"
			request.Out.Header.Set("X-Example", "modified")
			request.Out.Body = io.NopCloser(strings.NewReader("replacement"))
			request.Out.ContentLength = int64(len("replacement"))
		}
		handler.ModifyResponse = func(response *http.Response) error {
			_ = response.Body.Close()
			response.Body = io.NopCloser(strings.NewReader("changed"))
			response.ContentLength = int64(len("changed"))
			response.Header.Set("Content-Length", "7")
			response.Header.Set("X-Example", "modified")
			return nil
		}
	})
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "changed" || response.Header.Get("X-Example") != "modified" {
		t.Fatal("response transformation failed")
	}
}

func TestHTTPSUpstream(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "secure") }))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, func(handler *httputil.ReverseProxy) { handler.Transport = upstream.Client().Transport })
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "secure" {
		t.Fatalf("got %q", body)
	}
}

func BenchmarkProxy(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 32*1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(payload) }))
	defer upstream.Close()
	server := startProxy(b, upstream.URL, nil)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			response, err := server.Client().Get(server.URL)
			if err != nil {
				b.Error(err)
				return
			}
			_, err = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func TestHeadAndNoContent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Length", "123")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	for _, method := range []string{"HEAD", "DELETE"} {
		request, _ := http.NewRequest(method, server.URL, nil)
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || len(body) != 0 {
			t.Fatalf("%s body=%q err=%v", method, body, err)
		}
		if method == "HEAD" && response.ContentLength != 123 {
			t.Error("lost HEAD content length")
		}
		if method == "DELETE" && response.StatusCode != 204 {
			t.Error("lost no-content status")
		}
	}
}

func TestUntrustedTLSRejected(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted upstream reached") }))
	upstream.Config.ErrorLog = log.New(io.Discard, "", 0)
	upstream.StartTLS()
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 502 {
		t.Fatalf("untrusted certificate accepted: %d", response.StatusCode)
	}
}
