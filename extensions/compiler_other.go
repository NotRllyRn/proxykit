//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package extensions

import "os/exec"

func isolateCompiler(cmd *exec.Cmd) {}
