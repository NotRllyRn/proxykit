package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"
	"time"
)

// These are wire fixtures, not a WebSocket implementation. The proxy must relay
// frames without interpreting messages, masking, fragmentation or control frames.
func frame(opcode byte, final, masked bool, payload []byte) []byte {
	header := []byte{opcode, 0}
	if final {
		header[0] |= 0x80
	}
	if len(payload) < 126 {
		header[1] = byte(len(payload))
	} else {
		header[1] = 126
		header = binary.BigEndian.AppendUint16(header, uint16(len(payload)))
	}
	if masked {
		header[1] |= 0x80
		mask := []byte{1, 2, 3, 4}
		header = append(header, mask...)
		for index, value := range payload {
			header = append(header, value^mask[index%4])
		}
		return header
	}
	return append(header, payload...)
}

func readExact(t *testing.T, reader io.Reader, want []byte) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(reader, got); err != nil {
		t.Errorf("read: %v", err)
		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("wire bytes changed: got %d bytes, want %d", len(got), len(want))
	}
}

func TestWebSocketPassThrough(t *testing.T) {
	for _, secure := range []bool{false, true} {
		for _, path := range []string{"/v1/responses", "/v1/realtime?model=example-model", "/v1/live/sessions", "/ws/example.v1.Service/BidiGenerateContent?key=example"} {
			t.Run(fmt.Sprintf("tls=%t%s", secure, path), func(t *testing.T) {
				// Multiple turns, fragmented text, binary audio, ping/pong, and close.
				clientFrames := [][]byte{
					frame(1, true, true, []byte(`{"type":"response.create","input":"Example user"}`)),
					frame(1, false, true, []byte(`{"previous_response_id":`)),
					frame(9, true, true, []byte("ping")),
					frame(0, true, true, []byte(`"example-response"}`)),
					frame(2, true, true, bytes.Repeat([]byte{0, 255, 128, 1}, 8192)),
					frame(10, true, true, []byte("server ping")),
					frame(8, true, true, []byte{3, 232}),
				}
				serverFrames := [][]byte{
					frame(1, true, false, []byte(`{"type":"response.completed","id":"example-response"}`)),
					frame(1, false, false, []byte("part")),
					frame(10, true, false, []byte("ping")),
					frame(0, true, false, []byte(" end")),
					frame(2, true, false, bytes.Repeat([]byte{128, 0, 255}, 4096)),
					frame(9, true, false, []byte("server ping")),
					frame(8, true, false, []byte{3, 232}),
				}
				finished := make(chan struct{})
				upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer close(finished)
					if r.URL.RequestURI() != path || r.Header.Get("Authorization") != "Bearer example" || r.Header.Get("Sec-WebSocket-Protocol") != "example-events" || r.Header.Get("Origin") != "https://example.com" {
						t.Error("upgrade metadata changed")
					}
					connection, buffered, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer connection.Close()
					_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
					_, _ = io.WriteString(buffered, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\nSec-WebSocket-Protocol: example-events\r\n\r\n")
					// Server can initiate traffic before receiving a client message.
					_, _ = buffered.Write(frame(1, true, false, []byte("ready")))
					_ = buffered.Flush()
					for index, want := range clientFrames {
						readExact(t, buffered, want)
						if _, err := connection.Write(serverFrames[index]); err != nil {
							t.Error(err)
							return
						}
					}
				}))
				if secure {
					upstream.EnableHTTP2 = true // Upgrade requests must still negotiate HTTP/1.1.
					upstream.StartTLS()
				} else {
					upstream.Start()
				}
				defer upstream.Close()
				server := startProxy(t, upstream.URL, func(handler *httputil.ReverseProxy) {
					if secure {
						handler.Transport = upstream.Client().Transport
					}
				})
				connection, err := net.DialTimeout("tcp", strings.TrimPrefix(server.URL, "http://"), 5*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = fmt.Fprintf(connection, "GET %s HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Protocol: example-events\r\nAuthorization: Bearer example\r\nOrigin: https://example.com\r\n\r\n", path)
				buffered := bufio.NewReader(connection)
				response, err := http.ReadResponse(buffered, nil)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != 101 || response.Header.Get("Sec-WebSocket-Accept") != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" || response.Header.Get("Sec-WebSocket-Protocol") != "example-events" {
					t.Fatalf("upgrade: %+v", response)
				}
				readExact(t, buffered, frame(1, true, false, []byte("ready")))
				for index, outgoing := range clientFrames {
					if _, err := connection.Write(outgoing); err != nil {
						t.Fatal(err)
					}
					readExact(t, buffered, serverFrames[index])
				}
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Fatal("upstream did not close")
				}
			})
		}
	}
}

func TestWebSocketDisconnect(t *testing.T) {
	for _, disconnect := range []string{"client", "upstream", "context"} {
		t.Run(disconnect, func(t *testing.T) {
			upstreamConnection := make(chan net.Conn, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, buffered, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = io.WriteString(buffered, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
				_ = buffered.Flush()
				upstreamConnection <- connection
			}))
			defer upstream.Close()
			handler, err := New(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer handler.Transport.(*http.Transport).CloseIdleConnections()
			server := httptest.NewUnstartedServer(handler)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server.Config.BaseContext = func(net.Listener) context.Context { return ctx }
			server.Start()
			defer server.Close()
			connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
			_, _ = io.WriteString(connection, "GET /v1/responses HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
			reader := bufio.NewReader(connection)
			response, err := http.ReadResponse(reader, nil)
			if err != nil || response.StatusCode != 101 {
				t.Fatalf("upgrade: %v %v", response, err)
			}
			backend := <-upstreamConnection
			defer backend.Close()
			_ = backend.SetDeadline(time.Now().Add(5 * time.Second))
			switch disconnect {
			case "client":
				_ = connection.Close()
			case "upstream":
				_ = backend.Close()
			case "context":
				cancel()
			}
			var source io.Reader = reader
			if disconnect == "client" {
				source = backend
			}
			if _, err := source.Read(make([]byte, 1)); err == nil {
				t.Fatal("connection remained open")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("disconnect was not propagated")
			}
		})
	}
}
