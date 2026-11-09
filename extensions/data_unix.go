//go:build linux || darwin || freebsd

package extensions

import (
	"fmt"
	"os"
	"syscall"
)

func privateDataDir(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 || st.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return fmt.Errorf("extension data path must be a real private directory (0700) owned by current UID: %s", path)
	}
	return nil
}
