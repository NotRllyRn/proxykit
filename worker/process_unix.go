//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package worker

import (
	"os/exec"
	"syscall"
)

// Keep terminal signals in the host's foreground group. Shutdown is a protocol
// operation. Linux additionally uses this private group for bounded descendant
// termination while retaining ownership of the leader's PID.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
