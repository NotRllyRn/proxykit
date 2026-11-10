//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package worker

import (
	"os/exec"
	"syscall"
)

// Keep terminal signals in the host's foreground group. Shutdown is a protocol
// operation; killing the private group is the bounded fallback, including
// descendants that inherited it (for example npm and its subprocesses).
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcess(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
