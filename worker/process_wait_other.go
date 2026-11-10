//go:build !linux

package worker

import "os/exec"

// Without a non-reaping exit observation, do not signal a numeric PGID:
// Cmd.Wait releases the leader's PID before returning (including before its
// stderr copier finishes). os.Process.Kill handles exit/wait races safely.
func (p *process) wait() error  { return p.cmd.Wait() }
func killProcess(cmd *exec.Cmd) { _ = cmd.Process.Kill() }
