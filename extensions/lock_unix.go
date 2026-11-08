//go:build linux || darwin || freebsd

package extensions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock is nonblocking and held for the server's entire lifetime. Closing the
// descriptor releases ownership even after a crash. Never unlink the lock file.
func Lock(store string) (func(), error) {
	if e := validateStore(store); e != nil {
		return nil, e
	}
	st, e := os.Lstat(store)
	if e != nil {
		return nil, e
	}
	if st.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("extension store must be private (0700)")
	}
	if st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("extension store must be owned by current UID")
	}
	fd, e := syscall.Open(filepath.Join(store, ".owner-lock"), syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "store owner lock")
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		if errors.Is(e, syscall.EWOULDBLOCK) || errors.Is(e, syscall.EAGAIN) {
			return nil, fmt.Errorf("%w: %v", ErrStoreBusy, e)
		}
		return nil, e
	}
	return func() { _ = f.Close() }, nil
}
