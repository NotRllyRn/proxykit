package extension

import (
	"context"
	"errors"
	"io"
	"net/http"
)

// scopedCall intersects caller cancellation with invocation authority. The
// preflight rejects retained calls before execute can consume root budgets or
// admission. Cancellation racing admission may still admit work, but cancels it.
func scopedCall(scope context.Context, call func(context.Context, *http.Request) (*http.Response, error)) func(context.Context, *http.Request) (*http.Response, error) {
	return func(ctx context.Context, r *http.Request) (*http.Response, error) {
		if err := scope.Err(); err != nil {
			if r != nil && r.Body != nil {
				r.Body.Close()
			}
			return nil, err
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(scope, cancel)
		if scope.Err() != nil {
			cancel()
		}
		release := func() { stop(); cancel() }
		resp, err := call(ctx, r)
		if err != nil {
			release()
			return resp, err
		}
		if duplex, ok := resp.Body.(io.ReadWriteCloser); ok {
			resp.Body = &upgradeBody{ReadWriteCloser: duplex, release: release}
		} else {
			resp.Body = &releaseBody{ReadCloser: resp.Body, release: release}
		}
		return resp, nil
	}
}

// Outcomes are shared across owners. Never include native error/panic strings:
// they can contain credentials or capability values. Private errors still return
// through the execution API; this is intentionally not a general secret scanner.
func errorOutcome(err error) string {
	for _, c := range []struct {
		err  error
		name string
	}{
		{context.DeadlineExceeded, "deadline"},
		{context.Canceled, "canceled"},
		{ErrClosed, "closed"},
		{ErrOverload, "overload"},
		{ErrLimit, "limit"},
		{ErrUnsupported, "unsupported"},
	} {
		if errors.Is(err, c.err) {
			return c.name
		}
	}
	return "error"
}
