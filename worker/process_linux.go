package worker

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

// Observe exit without reaping. The unreaped leader reserves its PID (and thus
// our numeric PGID) until group signaling finishes. Only Cmd.Wait reaps it;
// calling Process.Wait ourselves would race os/exec's state and pipe handling.
func observeExit(cmd *exec.Cmd) error {
	var info [16]uint64 // Linux siginfo_t: 128 bytes, naturally aligned.
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1 /* P_PID */, uintptr(cmd.Process.Pid), uintptr(unsafe.Pointer(&info[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func (p *process) wait() error {
	if err := observeExit(p.cmd); err != nil {
		// If observation is unavailable (for example under seccomp), fail
		// closed to direct-child termination. Never signal a numeric group
		// after losing evidence that we still own its leader.
		p.killOnce.Do(func() { _ = p.cmd.Process.Kill() })
		return fmt.Errorf("observe worker exit: %w (wait: %v)", err, p.cmd.Wait())
	}
	// Once.Do waits for any concurrent kill to finish before returning.
	p.kill()
	return p.cmd.Wait()
}

func killProcess(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
