//go:build linux

package control

import (
	"net"
	"os"
	"syscall"
)

func peerAllowed(c net.Conn) bool {
	u, ok := c.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, e := u.SyscallConn()
	if e != nil {
		return false
	}
	allowed := false
	e = raw.Control(func(fd uintptr) {
		cred, e := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		allowed = e == nil && cred.Uid == uint32(os.Getuid())
	})
	return e == nil && allowed
}
