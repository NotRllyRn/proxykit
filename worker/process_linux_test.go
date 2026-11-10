package worker

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWaitReservesLeaderUntilGroupKillCompletes(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 0")
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &process{cmd: cmd}
	release := make(chan struct{})
	killing := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	// Hold a concurrent kill in progress. wait must join killOnce *before*
	// reaping, not after; a dead leader still reserves the group's numeric ID.
	go p.killOnce.Do(func() {
		killProcess(cmd)
		close(killing)
		<-release
	})
	<-killing
	if err := observeExit(cmd); err != nil {
		releaseOnce.Do(func() { close(release) })
		_ = cmd.Wait()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- p.wait() }()
	defer func() {
		releaseOnce.Do(func() { close(release) })
		<-done // only the waiter calls Cmd.Wait
	}()
	path := "/proc/" + strconv.Itoa(cmd.Process.Pid) + "/stat"
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("leader reaped before group kill completed: %v", err)
		}
		if !strings.Contains(string(data), ") Z ") {
			t.Fatalf("leader not reserved as zombie: %s", data)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWaitKillsInheritedStderrBeforeReaping(t *testing.T) {
	var stderr bytes.Buffer
	cmd := exec.Command("sh", "-c", "sleep 30 >&2 & echo $!; exit 0")
	configureProcess(cmd)
	cmd.Stderr = &stderr // exercise os/exec's copier, not a direct file
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &process{cmd: cmd}
	done := make(chan error, 1)
	go func() { done <- p.wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		p.kill()
		<-done
		t.Fatal("inherited stderr blocked Wait")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		t.Fatal(err)
	}
	// Orphaned descendants may remain zombies until the system reaps them.
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err == nil && !strings.Contains(string(data), ") Z ") {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("descendant survived group termination: %s", data)
	}
}
