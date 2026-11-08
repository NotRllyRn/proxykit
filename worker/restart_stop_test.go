package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestartJoinsInProgressStop(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cleaning")
	w := newTestWorker(t, fmt.Sprintf(`import {writeFileSync} from 'node:fs';export default api=>{api.on({id:'echo',stage:'observe',bodyMode:'none'},x=>x);api.cleanup(async()=>{writeFileSync(%q,'started');await new Promise(r=>setTimeout(r,100));});};`, marker), nil)
	start(t, w)
	stopped := make(chan error, 1)
	go func() { stopped <- w.Stop(context.Background()) }()
	deadline := time.After(time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("cleanup not started")
		case <-time.After(time.Millisecond):
		}
	}
	if err := w.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if got := invoke(t, w, `{"active":true}`); got != `{"active":true}` {
		t.Fatal(got)
	}
}
