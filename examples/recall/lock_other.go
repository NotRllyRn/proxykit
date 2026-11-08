//go:build !linux && !darwin && !freebsd

package recall

import (
	"context"
	"errors"
)

func lock(context.Context, string) (func(), error) {
	return nil, errors.New("recall atomic-file example unsupported on this platform")
}
