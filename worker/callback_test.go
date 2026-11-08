package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCallbackCorrelationExpiryAndCancellation(t *testing.T) {
	w := newTestWorker(t, `let saved;export default api=>api.on({id:'echo',stage:'request',bodyMode:'none'},async(i,c)=>{
 if(i.mode==='save'){saved=c.http;return {action:'pass'}};
 if(i.mode==='expired'){try{await saved({});return 'unsafe'}catch{return 'expired'}};
 if(i.mode==='concurrent'){const first=c.http({first:true});let rejected=false;try{await c.http({second:true})}catch{rejected=true};return {first:await first,rejected}};
 return await c.http(i);
 });`, nil)
	start(t, w)
	callback := func(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
			return b, nil
		}
	}
	for _, input := range []string{`{"v":1,"stage":"request","mode":"save"}`, `{"v":1,"stage":"request","mode":"expired"}`, `{"v":1,"stage":"request","mode":"concurrent"}`} {
		out, err := w.InvokeWithCallback(context.Background(), "echo", []byte(input), callback)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(input, "expired") && string(out) != `"expired"` {
			t.Fatal(string(out))
		}
		if strings.Contains(input, "concurrent") && !strings.Contains(string(out), `"rejected":true`) {
			t.Fatal(string(out))
		}
	}
	entered := make(chan struct{})
	exited := make(chan struct{})
	callback = func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := w.InvokeWithCallback(ctx, "echo", []byte(`{"v":1,"stage":"request"}`), callback)
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("callback scope survived cancellation")
	}
	// The Node promise receives abort and settles; this is not an uncooperative kill.
	time.Sleep(40 * time.Millisecond)
	if _, err := w.Invoke(context.Background(), "echo", []byte(`{"v":1,"stage":"request","mode":"expired"}`)); err != nil {
		t.Fatal(err)
	}
}

// Rejected local frames must not create gaps in Go's strict consecutive call IDs.
func TestCallbackUnsentFrameDoesNotConsumeCorrelation(t *testing.T) {
	for _, mode := range []string{"oversize", "circular", "bigint"} {
		t.Run(mode, func(t *testing.T) {
			w := newTestWorker(t, `export default api=>api.on({id:'echo',stage:'request',bodyMode:'none'},async(i,c)=>{
 let rejected=0;
 const rejectFrames=async()=>{
   for(let n=0;n<32;n++){
     let request;
     if(i.mode==='oversize') request={body:'x'.repeat(4096)};
     else if(i.mode==='circular'){request={};request.self=request;}
     else request={value:1n};
     try{await c.http(request)}catch{rejected++}
   }
 };
 await rejectFrames();
 const first=await c.http({ok:true});
 await rejectFrames();
 const second=await c.http({ok:true});
 return {rejected,first,second};
 });`, func(o *Options) {
				o.MaxFrame = 1024
				o.Concurrency = 1
			})
			start(t, w)
			var executions atomic.Int32
			callback := func(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
				executions.Add(1)
				if string(request) != `{"ok":true}` {
					t.Errorf("locally rejected frame reached Go: %s", request)
				}
				return json.RawMessage(`{"ok":true}`), nil
			}
			input := json.RawMessage(`{"v":1,"stage":"request","mode":"` + mode + `"}`)
			// A second invocation also proves the worker remains alive after recovery.
			for invocation := 0; invocation < 2; invocation++ {
				out, err := w.InvokeWithCallback(context.Background(), "echo", input, callback)
				if err != nil {
					t.Fatal(err)
				}
				if string(out) != `{"rejected":64,"first":{"ok":true},"second":{"ok":true}}` {
					t.Fatalf("unexpected recovery output: %s", out)
				}
			}
			if n := executions.Load(); n != 4 {
				t.Fatalf("callback executions = %d, want 4", n)
			}
		})
	}
}

func TestCallbackForgedAndExpiredScopesFailProtocol(t *testing.T) {
	for _, scope := range []string{"999", "1"} {
		t.Run(scope, func(t *testing.T) {
			source := `export default api=>api.on({id:'echo',stage:'request',bodyMode:'none'},async()=>{
 const body=Buffer.from(JSON.stringify({v:1,type:'call',id:'` + scope + `',callId:'1',request:{}}));const header=Buffer.alloc(4);header.writeUInt32BE(body.length);process.stdout.write(Buffer.concat([header,body]));return {action:'pass'};
 });`
			w := newTestWorker(t, source, nil)
			start(t, w)
			// No callback authorizes scope 1; scope 999 never existed.
			_, err := w.Invoke(context.Background(), "echo", []byte(`{}`))
			if err == nil {
				t.Fatal("forged scope accepted")
			}
		})
	}
}
func TestCallbackSlotLivesUntilActualSettlement(t *testing.T) {
	w := &Worker{options: Options{MaxFrame: 1024}, callbackSlots: make(chan struct{}, 1)}
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := &pending{ctx: ctx, callback: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-release
		return json.RawMessage(`{}`), nil
	}}
	p := &process{owner: w, pending: map[string]*pending{"1": first}, writes: make(chan []byte, 1)}
	p.mu.Lock()
	err := p.receiveCallLocked("1", "1", json.RawMessage(`{}`))
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	// A cancelled/settled Node invocation cannot release a still-running Go callback.
	cancel()
	p.mu.Lock()
	delete(p.pending, "1")
	p.pending["2"] = &pending{ctx: context.Background(), callback: func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Error("callback admitted before settlement")
		return nil, nil
	}}
	err = p.receiveCallLocked("2", "1", json.RawMessage(`{}`))
	p.mu.Unlock()
	if err != nil {
		t.Fatalf("overload killed protocol: %v", err)
	}
	b := <-p.writes
	if !strings.Contains(string(b), `"error":"worker callback capacity exceeded"`) || !strings.Contains(string(b), `"id":"2"`) {
		t.Fatalf("uncorrelated overload: %s", b)
	}
}

func TestCancelledCallbackFrameAlreadySent(t *testing.T) {
	dir := t.TempDir()
	gate, _ := json.Marshal(filepath.Join(dir, "gate"))
	sent, _ := json.Marshal(filepath.Join(dir, "sent"))
	w := newTestWorker(t, `import fs from 'node:fs'; export default api=>api.on({id:'echo',stage:'request',bodyMode:'none'},async(i,c)=>{
 if(i.wait){while(!fs.existsSync(`+string(gate)+`)) await new Promise(r=>setTimeout(r,1));}
 const result=c.http({}); if(i.wait) fs.writeFileSync(`+string(sent)+`, 'sent'); return await result;
 });`, nil)
	start(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	callback := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Error("cancelled in-flight callback executed")
		return json.RawMessage(`{}`), nil
	}
	go func() {
		_, err := w.InvokeWithCallback(ctx, "echo", []byte(`{"v":1,"stage":"request","wait":true}`), callback)
		done <- err
	}()
	waitPending(t, w, 1)
	p := w.process()
	p.mu.Lock() // Prevent receive from processing the legitimate frame.
	if err := os.WriteFile(filepath.Join(dir, "gate"), nil, 0600); err != nil {
		p.mu.Unlock()
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "sent")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			p.mu.Unlock()
			t.Fatal("Node did not send callback")
		}
		time.Sleep(time.Millisecond)
	}
	cancel() // Context is dead even if cancelInvoke is still waiting for p.mu.
	p.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	waitPending(t, w, 0)
	out, err := w.InvokeWithCallback(context.Background(), "echo", []byte(`{"v":1,"stage":"request"}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"usable"`), nil
	})
	if err != nil || string(out) != `"usable"` {
		t.Fatalf("unrelated invocation: %s %v", out, err)
	}
}

func TestCallbackOverloadAcrossLiveRestartAndSequentialCalls(t *testing.T) {
	w := newTestWorker(t, `export default api=>api.on({id:'echo',stage:'request',bodyMode:'none'},async(i,c)=>{try {let out;for(let n=0;n<(i.count??1);n++) out=await c.http({});return out;}catch(e){return String(e.message)}});`, func(o *Options) { o.Concurrency = 1 })
	start(t, w)
	entered, release, settled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := w.InvokeWithCallback(ctx, "echo", []byte(`{"v":1,"stage":"request"}`), func(context.Context, json.RawMessage) (json.RawMessage, error) {
			close(entered)
			<-release
			close(settled)
			return json.RawMessage(`{}`), nil
		})
		done <- err
	}()
	<-entered
	cancel()
	<-done
	if err := w.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	callback := func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`"ok"`), nil }
	out, err := w.InvokeWithCallback(context.Background(), "echo", []byte(`{"v":1,"stage":"request"}`), callback)
	if err != nil || !strings.Contains(string(out), "capacity exceeded") {
		t.Fatalf("live overload: %s %v", out, err)
	}
	// Unblock old callback without closing the deferred channel twice.
	release <- struct{}{}
	<-settled
	// Join response preparation/slot release, not merely callback return.
	w.callbackSlots <- struct{}{}
	<-w.callbackSlots
	out, err = w.InvokeWithCallback(context.Background(), "echo", []byte(`{"v":1,"stage":"request","count":200}`), callback)
	if err != nil || string(out) != `"ok"` {
		t.Fatalf("sequential callback: %s %v", out, err)
	}
}

func TestCopiedDescriptorLists(t *testing.T) {
	w := newTestWorker(t, `export default api=>{const d={id:'echo',stage:'request',bodyMode:'none',failure:'failOpen',formats:['text/plain'],requires:['other'],before:['next'],after:['last']};api.on(d,()=>({action:'pass'}));d.formats[0]='mutated';};`, nil)
	start(t, w)
	first := w.Hooks()
	if first[0].Formats[0] != "text/plain" {
		t.Fatal("runtime retained mutable registration")
	}
	first[0].Formats[0] = "changed"
	first[0].Requires[0] = "changed"
	first[0].Before[0] = "changed"
	first[0].After[0] = "changed"
	second := w.Hooks()
	if second[0].Formats[0] != "text/plain" || second[0].Requires[0] != "other" || second[0].Before[0] != "next" || second[0].After[0] != "last" {
		t.Fatal(second)
	}
}
