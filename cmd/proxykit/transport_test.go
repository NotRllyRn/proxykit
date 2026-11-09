package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Run the real command lifecycle in-process so a process exit cannot hide leaked
// upgraded connections. Existing system tests also exercise a compiled binary.
func startCommand(t *testing.T, upstream string, extra ...string) (string, func()) {
	t.Helper()
	// Transport fixtures must not activate packages from the developer's store.
	t.Setenv("PROXYKIT_EXTENSIONS_DIR", filepath.Join(t.TempDir(), "extensions"))
	ctx, cancel := context.WithCancel(context.Background())
	reader, writer := io.Pipe()
	result := make(chan error, 1)
	go func() {
		args := append([]string{"-listen", "127.0.0.1:0", "-upstream", upstream}, extra...)
		result <- run(ctx, args, writer)
		_ = writer.Close()
	}()
	address := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			if _, value, found := strings.Cut(scanner.Text(), "listening on "); found {
				address <- value
			}
		}
	}()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("command exit: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("command failed to stop")
		}
		_ = reader.Close()
	}
	t.Cleanup(stop)
	select {
	case value := <-address:
		return value, stop
	case <-time.After(5 * time.Second):
		t.Fatal("command did not listen")
		return "", stop
	}
}

func tlsFiles(t *testing.T) (string, string, *tls.Config) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer fixture.Close()
	certificate := fixture.TLS.Certificates[0]
	directory := t.TempDir()
	certPath, keyPath := filepath.Join(directory, "cert.pem"), filepath.Join(directory, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, fixture.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
}

func TestCommandProtocols(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "example") }))
	defer upstream.Close()
	cert, key, tlsConfig := tlsFiles(t)
	for _, protocol := range []string{"http1", "h2c", "https2"} {
		t.Run(protocol, func(t *testing.T) {
			var args []string
			scheme := "http"
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			if protocol == "h2c" {
				transport.Protocols = new(http.Protocols)
				transport.Protocols.SetUnencryptedHTTP2(true)
			}
			if protocol == "https2" {
				scheme = "https"
				args = []string{"-tls-cert", cert, "-tls-key", key}
				transport.TLSClientConfig = tlsConfig.Clone()
			}
			defer transport.CloseIdleConnections()
			address, _ := startCommand(t, upstream.URL, args...)
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			response, err := client.Get(scheme + "://" + address + "/v1/responses")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			expected := 2
			if protocol == "http1" {
				expected = 1
			}
			if err != nil || string(body) != "example" || response.ProtoMajor != expected {
				t.Fatalf("response: %q %v protocol=%s", body, err, response.Proto)
			}
		})
	}
}

func TestCommandWebSocketShutdown(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%t", secure), func(t *testing.T) {
			closed := make(chan error, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, buffered, err := w.(http.Hijacker).Hijack()
				if err != nil {
					closed <- err
					return
				}
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				_, _ = io.WriteString(buffered, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
				_ = buffered.Flush()
				_, err = connection.Read(make([]byte, 1))
				closed <- err
			}))
			defer upstream.Close()
			var args []string
			cert, key, tlsConfig := tlsFiles(t)
			if secure {
				args = []string{"-tls-cert", cert, "-tls-key", key}
			}
			address, stop := startCommand(t, upstream.URL, args...)
			var connection net.Conn
			var err error
			dialer := &net.Dialer{Timeout: 5 * time.Second}
			if secure {
				connection, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
			} else {
				connection, err = dialer.Dial("tcp", address)
			}
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
			stop()
			if _, err := reader.ReadByte(); err == nil {
				t.Error("downstream remained open")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Error("downstream close timed out")
			}
			select {
			case err := <-closed:
				if err == nil {
					t.Error("upstream remained open")
				} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Error("upstream close timed out")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("upstream leaked")
			}
		})
	}
}

func TestTLSArguments(t *testing.T) {
	for _, args := range [][]string{
		{"-tls-cert", "example.pem"},
		{"-tls-key", "example.pem"},
		{"-listen", "127.0.0.1:0", "-tls-cert", "missing-cert.pem", "-tls-key", "missing-key.pem"},
	} {
		if err := run(context.Background(), append([]string{"-upstream", "http://example.com"}, args...), io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}
