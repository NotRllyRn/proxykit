//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package worker

import "os/exec"

// Other platforms retain direct-child termination. Unix process-group signal
// isolation and descendant termination are not available through this fallback.
func configureProcess(cmd *exec.Cmd) {}
func killProcess(cmd *exec.Cmd)      { _ = cmd.Process.Kill() }
