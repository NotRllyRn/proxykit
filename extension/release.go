package extension

import (
	"io"
	"net/http"
	"sync"
)

type upgradeBody struct {
	io.ReadWriteCloser
	release func()
	once    sync.Once
	err     error
}

func (b *upgradeBody) Close() error {
	b.once.Do(func() { b.err = b.ReadWriteCloser.Close(); b.release() })
	return b.err
}

type trailerBody struct {
	io.ReadCloser
	trailer http.Header
}

func (b *trailerBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		clean(b.trailer)
	}
	return n, err
}

type releaseBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (b *releaseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.release)
	}
	return n, err
}
func (b *releaseBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }
