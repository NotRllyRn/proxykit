package worker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAsyncSetupClosesRegistrations(t *testing.T) {
	w := newTestWorker(t, `export default async api=>{await new Promise(r=>setTimeout(r,10));api.on({id:'echo',stage:'observe',bodyMode:'none'},()=>{let closed=false;try{api.on({id:'late',stage:'observe',bodyMode:'none'},()=>null)}catch{closed=true}return {closed};});};`, nil)
	start(t, w)
	if got := invoke(t, w, `{}`); got != `{"closed":true}` {
		t.Fatal(got)
	}
	if len(w.Hooks()) != 1 {
		t.Fatal("late registration leaked")
	}
}
func TestStopCancelsWaitingCallWithoutWaitingCallTimeout(t *testing.T) {
	w := newTestWorker(t, `export default api=>api.on({id:'echo',stage:'observe',bodyMode:'none'},async (input,{signal})=>{await new Promise(resolve=>{if(signal.aborted) resolve();else signal.addEventListener('abort',resolve,{once:true})});return null;});`, nil)
	start(t, w)
	done := make(chan error, 1)
	go func() { _, err := w.Invoke(context.Background(), "echo", json.RawMessage(`{}`)); done <- err }()
	waitPending(t, w, 1)
	if err := w.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stopped call succeeded")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stopped call still waiting")
	}
}
func TestBoundedStreamUnitsCrossActualProcess(t *testing.T) {
	sdk, err := filepath.Abs("../sdk/index.mjs")
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`import {streamBytes,streamUnit} from %q; export default api=>api.on({id:'echo',stage:'body',bodyMode:'stream'},unit=>{const bytes=streamBytes(unit,4);return streamUnit(unit.streamId,unit.sequence,bytes,unit.end,4);});`, sdk)
	w := newTestWorker(t, source, nil)
	start(t, w)
	input := `{"streamId":"example","sequence":0,"data":"AP+A","end":false}`
	if got := invoke(t, w, input); got != input {
		t.Fatal(got)
	}
	if _, err = w.Invoke(context.Background(), "echo", json.RawMessage(`{"streamId":"example","sequence":1,"data":"AAAAAAA=","end":true}`)); err == nil {
		t.Fatal("oversize decoded unit accepted")
	}
	if got := invoke(t, w, `{"streamId":"example","sequence":1,"data":"","end":true}`); got == "" {
		t.Fatal("missing final unit")
	}
}
func TestRuntimeRejectsMalformedHostFrames(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node unavailable")
	}
	for name, payload := range map[string][]byte{"zero": {0, 0, 0, 0}, "oversize": {127, 255, 255, 255}, "json": framed([]byte(`{`)), "version": framed([]byte(`{"v":2,"type":"init"}`)), "unknown": framed([]byte(`{"v":1,"type":"unknown"}`)), "invoke-before-ready": framed([]byte(`{"v":1,"type":"invoke","id":"1","hook":"x","input":null}`)), "invalid-utf8": framed([]byte{'"', 255, '"'})} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "--input-type=module", "-e", runtimeSource)
			cmd.Stdin = bytes.NewReader(payload)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Run(); err == nil {
				t.Fatal("malformed frame accepted")
			}
			if ctx.Err() != nil {
				t.Fatal("runtime hung", stderr.String())
			}
		})
	}
}
func framed(b []byte) []byte {
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}
func TestFragmentedFrames(t *testing.T) {
	w := newTestWorker(t, echo, nil)
	start(t, w)
	p := w.process()
	p.mu.Lock()
	p.pending["test-fragment"] = &pending{ch: make(chan reply, 1)}
	p.mu.Unlock()
	b := framed([]byte(`{"v":1,"type":"result","id":"test-fragment","output":true}`))
	r := &singleByteReader{bytes.NewReader(b)}
	p.reader(r)
	p.mu.Lock()
	_, exists := p.pending["test-fragment"]
	p.mu.Unlock()
	if exists {
		t.Fatal("fragmented reply not decoded")
	}
}

type singleByteReader struct{ *bytes.Reader }

func (r *singleByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
