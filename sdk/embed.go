package sdk

import "embed"

// Files is the host SDK used by the pinned source compiler and its emitted runtime.
//
//go:embed index.mjs index.d.mts http.d.mts
var Files embed.FS
