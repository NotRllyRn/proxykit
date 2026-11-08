//go:build !linux && !darwin && !freebsd

package extensions

import "errors"

func Lock(string) (func(), error) {
	return nil, errors.New("extension management requires Linux, macOS or FreeBSD Unix locking/sockets")
}
