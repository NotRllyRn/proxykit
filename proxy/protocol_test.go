package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Representative wire shapes are deliberately opaque. These tests establish
// transport preservation, not live provider access or API-schema validation.
func TestOpaqueEndpoints(t *testing.T) {
	for _, example := range []struct{ name, path, contentType, body string }{
		{"responses", "/v1/responses", "application/json", `{"model":"example-model","previous_response_id":"example-id","unknown":{"retain":true},"input":[{"type":"compaction","encrypted_content":"opaque+/="}]}`},
		{"compact", "/v1/responses/compact", "application/json", `{"input":[{"type":"reasoning","encrypted_content":"opaque+/="}]}`},
		{"chat", "/v1/chat/completions", "application/json", `{"messages":[{"role":"user","content":"Example user"}],"tools":[]}`},
		{"messages", "/v1/messages?beta=true", "application/json", `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"example","signature":"opaque"}]}]}`},
		{"google", "/v1beta/models/example-model:streamGenerateContent?alt=sse", "application/json", `{"contents":[{"parts":[{"text":"example","thoughtSignature":"opaque"}]}]}`},
		{"audio upload", "/v1/audio/transcriptions", "multipart/form-data; boundary=example", "--example\r\nContent-Disposition: form-data; name=\"file\"; filename=\"example.wav\"\r\nContent-Type: audio/wav\r\n\r\n\x00\xff\x80\r\n--example--\r\n"},
		{"webrtc signaling", "/v1/realtime/calls", "application/sdp", "v=0\r\ns=Example session\r\n"},
		{"binary event stream", "/model/example/invoke-with-response-stream", "application/vnd.amazon.eventstream", "\x00\x00\x00\x10\xff\x80\x00\x01opaque"},
		{"unknown future endpoint", "/custom/future:method?literal=a%2Bb", "application/octet-stream", "\x00\xff\x80opaque"},
	} {
		t.Run(example.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != example.body || r.URL.RequestURI() != example.path || r.Header.Get("Content-Type") != example.contentType {
					t.Error("request changed")
				}
				for header, value := range map[string]string{"Authorization": "Bearer example", "X-Api-Key": "example", "Anthropic-Version": "2023-06-01", "Anthropic-Beta": "example", "X-Goog-Api-Key": "example"} {
					if r.Header.Get(header) != value {
						t.Errorf("lost %s", header)
					}
				}
				w.Header().Set("Content-Type", example.contentType)
				w.Header().Set("X-Request-ID", "example-id")
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write(body)
			}))
			defer upstream.Close()
			server := startProxy(t, upstream.URL, nil)
			request, _ := http.NewRequest("POST", server.URL+example.path, strings.NewReader(example.body))
			request.Header.Set("Content-Type", example.contentType)
			for header, value := range map[string]string{"Authorization": "Bearer example", "X-Api-Key": "example", "Anthropic-Version": "2023-06-01", "Anthropic-Beta": "example", "X-Goog-Api-Key": "example"} {
				request.Header.Set(header, value)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != example.body || response.StatusCode != 202 || response.Header.Get("Content-Type") != example.contentType || response.Header.Get("X-Request-ID") != "example-id" || response.Header.Get("Retry-After") != "2" {
				t.Fatal("response changed")
			}
		})
	}
}

func TestKnownLengthStreaming(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/pcm")
		w.Header().Set("Content-Length", "10")
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, " last")
	}))
	t.Cleanup(upstream.Close)
	server := startProxy(t, upstream.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/audio/speech", nil)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	readExact(t, response.Body, []byte("first"))
	release <- struct{}{}
	readExact(t, response.Body, []byte(" last"))
}

func TestHTTP1FullDuplex(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).EnableFullDuplex()
		readExact(t, r.Body, []byte("first"))
		w.Header().Set("Trailer", "X-Result")
		_, _ = io.WriteString(w, "ack\n")
		w.(http.Flusher).Flush()
		// This cannot arrive until the client has read the acknowledgement.
		remaining, err := io.ReadAll(r.Body)
		if err != nil || string(remaining) != "last" || r.Trailer.Get("X-Input") != "complete" {
			t.Errorf("upload end: %q %v trailers=%v", remaining, err, r.Trailer)
		}
		_, _ = io.WriteString(w, "done\n")
		w.Header().Set("X-Result", "complete")
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = io.WriteString(connection, "POST /duplex HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\nTrailer: X-Input\r\n\r\n5\r\nfirst\r\n")
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	readExact(t, response.Body, []byte("ack\n"))
	_, _ = io.WriteString(connection, "4\r\nlast\r\n0\r\nX-Input: complete\r\n\r\n")
	remaining, err := io.ReadAll(response.Body)
	if err != nil || string(remaining) != "done\n" || response.Trailer.Get("X-Result") != "complete" {
		t.Fatalf("duplex response: %q %v trailers=%v", remaining, err, response.Trailer)
	}
}

func TestHTTP2DuplexAndTrailers(t *testing.T) {
	// A gRPC-shaped frame is relayed without decoding its envelope or protobuf.
	message := []byte{0, 0, 0, 0, 3, 0, 255, 1}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 || r.Header.Get("Te") != "trailers" {
			t.Error("HTTP/2 request metadata lost")
		}
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status")
		for range 2 {
			readExact(t, r.Body, message)
			_, _ = w.Write(message)
			w.(http.Flusher).Flush()
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Grpc-Status", "0")
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()
	handler, err := New(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport := handler.Transport.(*http.Transport)
	defer transport.CloseIdleConnections()
	transport.TLSClientConfig = upstream.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/example.Service/Stream", reader)
	request.Header.Set("Content-Type", "application/grpc")
	request.Header.Set("Te", "trailers")
	done := make(chan error, 1)
	go func() {
		response, err := server.Client().Do(request)
		if err != nil {
			done <- err
			return
		}
		defer response.Body.Close()
		if response.ProtoMajor != 2 {
			t.Error("downstream did not use HTTP/2")
		}
		readExact(t, response.Body, message)
		if _, err = writer.Write(message); err != nil {
			done <- err
			return
		}
		_ = writer.Close()
		data, err := io.ReadAll(response.Body)
		if !bytes.Equal(data, message) || response.Trailer.Get("Grpc-Status") != "0" {
			err = fmt.Errorf("response/trailers changed: %x %v", data, response.Trailer)
		}
		done <- err
	}()
	if _, err := writer.Write(message); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRejectedUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "example rate limit", http.StatusTooManyRequests)
	}))
	defer upstream.Close()
	server := startProxy(t, upstream.URL, nil)
	request, _ := http.NewRequest("GET", server.URL+"/v1/responses", nil)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 429 || response.Header.Get("Retry-After") != "5" || string(body) != "example rate limit\n" {
		t.Fatal("upgrade error changed")
	}
}
