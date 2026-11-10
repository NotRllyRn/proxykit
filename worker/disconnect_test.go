package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDisconnectBoundsUncooperativeCleanup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleaning")
	w := newTestWorker(t, fmt.Sprintf(`import {writeFileSync} from 'node:fs';
export default api=>{api.cleanup(()=>{writeFileSync(%q,'');return new Promise(()=>{});});};`, marker), func(o *Options) { o.ShutdownTimeout = 100 * time.Millisecond })
	start(t, w)
	p := w.process()
	// Closing the protocol input is what the worker sees when the host dies.
	if err := p.stdin.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected worker survived its cleanup deadline")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("cleanup never attempted: %v", err)
	}
}

func TestStartupCancellationBoundsUncooperativeSetup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "setup")
	w := newTestWorker(t, fmt.Sprintf(`import {writeFileSync} from 'node:fs';
export default async api=>{writeFileSync(%q,'');await new Promise(()=>{});};`, marker), func(o *Options) {
		o.ShutdownTimeout = 2 * time.Second
		o.CancelGrace = 100 * time.Millisecond
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- w.Start(ctx) }()
	deadline := time.After(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("setup did not start")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup cancellation exceeded shutdown budget")
	}
	select {
	case <-w.process().exited:
	case <-time.After(time.Second):
		t.Fatal("uncooperative setup not killed")
	}
}
