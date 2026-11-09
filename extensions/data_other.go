//go:build !linux && !darwin && !freebsd

package extensions

import "errors"

func privateDataDir(string) error {
	return errors.New("extension data directories unsupported on this platform")
}
