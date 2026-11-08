package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/nodehost"
)

func testRuntime(t *testing.T) (string, *nodehost.Runtime) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("Unix management only")
	}
	store := t.TempDir()
	if e := os.Chmod(store, 0700); e != nil {
		t.Fatal(e)
	}
	unlock, e := extensions.Lock(store)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(unlock)
	m, e := nodehost.OpenRuntime(context.Background(), nodehost.RuntimeOptions{Options: nodehost.Options{Store: store, Gateway: "http://127.0.0.1:1"}, Drain: 20 * time.Millisecond, Cleanup: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := m.Close(ctx); e != nil {
			t.Error(e)
		}
	})
	return store, m
}
func TestProtectedManagementStrictCommandsAndOwnership(t *testing.T) {
	store, m := testRuntime(t)
	stop, e := Listen(store, m)
	if e != nil {
		t.Fatal(e)
	}
	defer stop(context.Background())
	st, e := os.Stat(socket(store))
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal(st, e)
	}
	if _, e = extensions.Lock(store); !errors.Is(e, extensions.ErrStoreBusy) {
		t.Fatal("server ownership missing", e)
	}
	s, e := Call(context.Background(), store, Request{Op: "status"})
	if e != nil || s.Status.Generation != 1 {
		t.Fatal(s, e)
	}
	if _, e = Call(context.Background(), store, Request{Op: "enable", ID: "missing"}); e == nil {
		t.Fatal("missing ID enabled")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket(store))
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: time.Second}
	for _, body := range []string{`{"op":"unknown"}`, `{"op":"status","op":"status"}`, `{"op":"status","\u006fp":"status"}`, `null`, `[]`, `{"op":1}`, `{"op":"status","id":"unexpected"}`, `{"op":"status","trust":true}`, `{"op":"install","id":"unexpected"}`, `{"op":"status","extra":1}`, `{"op":"status"} {}`, strings.Repeat("x", 8193)} {
		r, e := client.Post("http://local/", "application/json", bytes.NewBufferString(body))
		if e != nil {
			t.Fatal(e)
		}
		var response Response
		e = json.NewDecoder(r.Body).Decode(&response)
		r.Body.Close()
		if e != nil || response.Error == "" {
			t.Fatalf("accepted %q: %+v %v", body, response, e)
		}
	}
	response, e := client.Get("http://local/status")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(b), "only POST") {
		t.Fatal(string(b))
	}
	if runtime.GOOS == "linux" {
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		if peerAllowed(a) {
			t.Fatal("non-Unix peer accepted")
		}
	}
}
func TestControlRefusesUnsafePathsAndRecoversStaleSocket(t *testing.T) {
	for _, kind := range []string{"wide-directory", "symlink-directory", "regular-socket-path", "symlink-socket-path", "stale-socket"} {
		t.Run(kind, func(t *testing.T) {
			store, m := testRuntime(t)
			dir := filepath.Dir(socket(store))
			switch kind {
			case "wide-directory":
				os.Mkdir(dir, 0755)
			case "symlink-directory":
				os.Symlink(t.TempDir(), dir)
			default:
				os.Mkdir(dir, 0700)
				switch kind {
				case "regular-socket-path":
					os.WriteFile(socket(store), []byte("preserve"), 0600)
				case "symlink-socket-path":
					os.Symlink(filepath.Join(t.TempDir(), "target"), socket(store))
				case "stale-socket":
					l, e := net.ListenUnix("unix", &net.UnixAddr{Name: socket(store), Net: "unix"})
					if e != nil {
						t.Fatal(e)
					}
					l.SetUnlinkOnClose(false)
					l.Close()
				}
			}
			stop, e := Listen(store, m)
			if kind == "stale-socket" {
				if e != nil {
					t.Fatal(e)
				}
				stop(context.Background())
				return
			}
			if e == nil {
				stop(context.Background())
				t.Fatal("unsafe control path accepted")
			}
		})
	}
}
