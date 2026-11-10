//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package extensions

import (
	"os/exec"
	"syscall"
)

func isolateCompiler(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The host driver never spawns children. Group cancellation also covers an
	// accidental inherited child on timeout, without invoking a shell.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
