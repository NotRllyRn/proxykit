package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extensions"
)

// Real immutable archive installation -> compiled executable -> Node -> upstream
// -> decorated HTTP response, followed by process-level bounded cleanup.
func TestSystemInstalledNodeActivationAndShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX termination fixture")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	temp := t.TempDir()
	binary := filepath.Join(temp, "proxykit")
	marker := filepath.Join(temp, "cleanup")
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %v %s", err, out)
	}
	source := fmt.Sprintf(`import {writeFileSync,readFileSync,appendFileSync} from 'node:fs';import {join,isAbsolute} from 'node:path';import {defineHTTP} from './sdk.mjs';export default defineHTTP(api=>{
 if(!isAbsolute(api.dataDir)||api.dataDir.split('/').pop()!=='example-user')throw Error('wrong dataDir');
 writeFileSync(join(api.dataDir,'config'),'initialized',{mode:0o600});
 if(readFileSync(join(api.dataDir,'config'),'utf8')!=='initialized')throw Error('missing initialized dataDir');
 appendFileSync(join(api.dataDir,'setups'),'setup\n',{mode:0o600});
 api.on({id:'decorate',stage:'responseBody',bodyMode:'buffered'},i=>({action:'replace',message:{...i.response,body:Buffer.from(Buffer.from(i.response.body,'base64').toString()+' installed').toString('base64')}}));
 api.cleanup(()=>writeFileSync(%q,'clean'));
 });`, marker)
	sdk, err := os.ReadFile("../../sdk/index.mjs")
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"root/proxykit-extension.json": `{"manifestVersion":1,"apiVersion":1,"id":"example-user","version":"1.0.0","entry":"main.mjs"}`, "root/main.mjs": source, "root/sdk.mjs": string(sdk)} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			fmt.Fprintf(w, `{"sha":%q}`, strings.Repeat("b", 40))
		} else {
			_, _ = w.Write(archive.Bytes())
		}
	}))
	defer fake.Close()
	store := filepath.Join(temp, "extensions")
	var output bytes.Buffer
	installer := extensions.Installer{Store: store, APIBase: fake.URL, ArchiveBase: fake.URL}
	if err := runExtensionWithInstaller(context.Background(), []string{"install", "https://github.com/example/repository", "--trust"}, &output, installer); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(store, ".data", "example-user")
	if st, err := os.Stat(dataDir); err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		t.Fatalf("install did not create private storage: %v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "config")); !os.IsNotExist(err) {
		t.Fatal("install must not run setup", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/example" {
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, "upstream")
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-listen", "127.0.0.1:0", "-upstream", upstream.URL)
	command.Env = append(os.Environ(), "PROXYKIT_EXTENSIONS_DIR="+store)
	stderr, err := command.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	address := make(chan string, 1)
	logs := make(chan string, 1)
	go func() {
		var all strings.Builder
		scan := bufio.NewScanner(stderr)
		for scan.Scan() {
			line := scan.Text()
			all.WriteString(line + "\n")
			if _, value, ok := strings.Cut(line, "listening on "); ok {
				address <- value
			}
		}
		logs <- all.String()
	}()
	var listen string
	select {
	case listen = <-address:
	case text := <-logs:
		t.Fatalf("startup failed: %s", text)
	case <-ctx.Done():
		t.Fatal("executable startup timeout")
	}
	client := http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("http://" + listen + "/example")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(before) != "upstream" {
		t.Fatalf("plain install executed: %q", before)
	}
	manage := func(args ...string) string {
		t.Helper()
		c := exec.CommandContext(ctx, binary, append([]string{"extension"}, args...)...)
		c.Env = command.Env
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("management %v: %v %s", args, e, out)
		}
		return string(out)
	}
	manage("enable", "example-user")
	status := manage("status", "example-user")
	if !strings.Contains(status, "ACTIVE") || !strings.Contains(status, "generation=2") {
		t.Fatal(status)
	}
	resp, err = client.Get("http://" + listen + "/example")
	if err != nil {
		t.Fatal(err)
	}
	b, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil || resp.StatusCode != 200 || string(b) != "upstream installed" {
		t.Fatalf("HTTP %d %q %v", resp.StatusCode, b, readErr)
	}
	manage("disable", "example-user")
	resp, err = client.Get("http://" + listen + "/example")
	if err != nil {
		t.Fatal(err)
	}
	b, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(b) != "upstream" {
		t.Fatal(string(b), err)
	}
	// Wait until graceful retirement reaps the prior owner before re-enabling.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(marker); e == nil {
			status := manage("status")
			if strings.Contains(status, "draining=[]") && !strings.Contains(status, "cleanup/reaping") {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	manage("enable", "example-user")
	resp, err = client.Get("http://" + listen + "/example")
	if err != nil {
		t.Fatal(err)
	}
	b, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || string(b) != "upstream installed" {
		t.Fatal(string(b), err)
	}
	// A second server must not delete the live socket or mutate this store.
	second := exec.CommandContext(ctx, binary, "-listen", "127.0.0.1:0", "-upstream", upstream.URL)
	second.Env = command.Env
	if out, e := second.CombinedOutput(); e == nil || !strings.Contains(string(out), "store busy") {
		t.Fatalf("second owner: %v %s", e, out)
	}
	manage("status")
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("compiled shutdown %v; %s", err, <-logs)
	}
	if b, err := os.ReadFile(filepath.Join(dataDir, "setups")); err != nil || string(b) != "setup\nsetup\n" {
		t.Fatalf("persistent setup state: %q %v", b, err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "clean" {
		t.Fatalf("worker not cleaned/reaped: %q %v", b, err)
	}
}
