//go:build !linux

package control

import "net"

// Private owner-validated directory/socket permissions enforce local access on
// supported non-Linux Unix hosts. Linux additionally verifies SO_PEERCRED.
func peerAllowed(net.Conn) bool { return true }
