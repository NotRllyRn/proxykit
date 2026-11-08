//go:build !linux && !darwin && !freebsd

package control

import (
	"errors"
	"os"
)

func checkOwner(os.FileInfo) error { return errors.New("management requires supported Unix host") }
