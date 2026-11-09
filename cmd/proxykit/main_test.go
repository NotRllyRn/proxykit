package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestArguments(t *testing.T) {
	t.Setenv("PROXYKIT_EXTENSIONS_DIR", filepath.Join(t.TempDir(), "extensions"))
	for _, args := range [][]string{nil, {"-unknown"}, {"-upstream", "ftp://example.com"}, {"-upstream", "http://example.com", "extra"}, {"-upstream", "http://example.com", "-listen", "invalid address"}} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"-help"}, &output); err != nil || !strings.Contains(output.String(), "-upstream") {
		t.Fatalf("help: %s %v", &output, err)
	}
}

// Exercise the actual executable, real sockets, and termination signal.
func TestSystemGracefulShutdown(t *testing.T) {
	t.Setenv("PROXYKIT_EXTENSIONS_DIR", filepath.Join(t.TempDir(), "extensions"))
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX termination signals")
	}
	binary := filepath.Join(t.TempDir(), "proxykit")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	release := make(chan struct{})
	defer close(release)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "first\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "last\n")
	}))
	// Cleanup must release the upstream before closing its server on failure.
	t.Cleanup(upstream.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-listen", "127.0.0.1:0", "-upstream", upstream.URL)
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	address := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if _, value, found := strings.Cut(scanner.Text(), "listening on "); found {
				address <- value
				return
			}
		}
	}()
	var listen string
	select {
	case listen = <-address:
	case <-ctx.Done():
		t.Fatal("executable did not start")
	}
	request, _ := http.NewRequestWithContext(ctx, "GET", "http://"+listen+"/example", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || line != "first\n" {
		t.Fatalf("first chunk: %q %v", line, err)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// Wait until shutdown closes the listener, then let the active response finish.
	for {
		connection, err := net.DialTimeout("tcp", listen, 100*time.Millisecond)
		if err != nil {
			break
		}
		_ = connection.Close()
		select {
		case <-ctx.Done():
			t.Fatal("listener did not close")
		case <-time.After(10 * time.Millisecond):
		}
	}
	release <- struct{}{}
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "last\n" {
		t.Fatalf("drain: %q %v", rest, err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("exit: %v", err)
	}
}
