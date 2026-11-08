package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/NotRllyRn/proxykit/ai"
)

// FileSink is a synchronous JSON-lines original recorder for a private local
// directory, owned by one process. Calls through this instance are serialized.
// Retention, encryption and uncertain partial-write recovery are operator duties;
// an error must not be retried as a successful inference/tool operation.
func FileSink(path string) ai.AttemptSink {
	var mu sync.Mutex
	return func(ctx context.Context, attempts []ai.Attempt) error {
		mu.Lock()
		defer mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := json.Marshal(attempts)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(append(data, '\n'))
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		// Persist the initial directory entry as well as the data before selection.
		d, err := os.Open(filepath.Dir(path))
		if err != nil {
			return err
		}
		err = d.Sync()
		closeErr = d.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
}
