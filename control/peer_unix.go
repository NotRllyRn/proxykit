//go:build linux || darwin || freebsd

package control

import (
	"fmt"
	"os"
	"syscall"
)

func checkOwner(st os.FileInfo) error {
	if st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return fmt.Errorf("control directory owned by another UID")
	}
	return nil
}
