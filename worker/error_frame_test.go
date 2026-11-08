package worker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Exercise the embedded runtime wire directly so even discarded CANCELLED replies
// and the complete encoded frame (not a re-encoding of its message) are checked.
func TestRuntimeErrorFrames(t *testing.T) {
	for _, limit := range []int{256, 1024, 1 << 20} {
		for _, kind := range []string{"multibyte", "escaped"} {
			t.Run(fmt.Sprintf("%d/%s", limit, kind), func(t *testing.T) {
				w := newTestWorker(t, `export default api=>{
 const text=(api.config.kind==='multibyte'?'😀':'\u0000"\\\n').repeat(512);
 let release;
 api.on({id:'echo',stage:'observe',bodyMode:'none'},async (input,context)=>{
  if(input==='hold')return await new Promise(r=>{release=()=>r('held')});
  if(input==='fail')throw Error(text);
  if(input==='cancel')await new Promise((_,reject)=>context.signal.addEventListener('abort',()=>reject(Error(text)),{once:true}));
  if(input==='release')release();
  return input;
 });
};`, nil)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, w.options.NodeBinary, "--input-type=module", "-e", runtimeSource)
				stdin, err := cmd.StdinPipe()
				if err != nil {
					t.Fatal(err)
				}
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					t.Fatal(err)
				}
				var logs bytes.Buffer
				cmd.Stderr = &logs
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
				send := func(m any) {
					t.Helper()
					b, err := json.Marshal(m)
					if err != nil {
						t.Fatal(err)
					}
					var header [4]byte
					binary.BigEndian.PutUint32(header[:], uint32(len(b)))
					if _, err := stdin.Write(append(header[:], b...)); err != nil {
						t.Fatal(err)
					}
				}
				receive := func() map[string]any {
					t.Helper()
					var header [4]byte
					if _, err := io.ReadFull(stdout, header[:]); err != nil {
						t.Fatalf("read frame: %v", err)
					}
					n := binary.BigEndian.Uint32(header[:])
					if n == 0 || n > uint32(limit) {
						t.Fatalf("frame bytes %d exceed %d", n, limit)
					}
					b := make([]byte, n)
					if _, err := io.ReadFull(stdout, b); err != nil {
						t.Fatal(err)
					}
					if !utf8.Valid(b) {
						t.Fatal("invalid UTF-8 frame")
					}
					var m map[string]any
					if err := json.Unmarshal(b, &m); err != nil {
						t.Fatal(err)
					}
					return m
				}
				invoke := func(id, input string) {
					send(map[string]any{"v": 1, "type": "invoke", "id": id, "hook": "echo", "input": input})
				}
				checkError := func(m map[string]any, id, code string) {
					t.Helper()
					if m["type"] != "error" || m["id"] != id {
						t.Fatalf("wrong correlated error: %v", m)
					}
					e, ok := m["error"].(map[string]any)
					if !ok || e["code"] != code {
						t.Fatalf("wrong error code: %v", m)
					}
					text, ok := e["message"].(string)
					if !ok || text == "" || strings.ContainsRune(text, '\uFFFD') {
						t.Fatalf("invalid diagnostic: %v", m)
					}
					unit := "😀"
					if kind == "escaped" {
						unit = "\x00\"\\\n"
					}
					if !strings.HasPrefix(strings.Repeat(unit, 512), text) {
						t.Fatalf("not an original diagnostic prefix: %q", text)
					}
				}
				send(map[string]any{"v": 1, "type": "init", "entry": w.options.Entry, "config": map[string]string{"kind": kind}, "maxFrame": limit, "concurrency": 4})
				if m := receive(); m["type"] != "ready" {
					t.Fatal(m)
				}
				invoke("1", "hold")
				const maximumID = "18446744073709551615"
				invoke(maximumID, "fail")
				checkError(receive(), maximumID, "HOOK_ERROR")
				invoke("2", "release")
				results := map[string]any{}
				for range 2 {
					m := receive()
					if m["type"] != "result" {
						t.Fatal(m)
					}
					results[m["id"].(string)] = m["output"]
				}
				if results["1"] != "held" || results["2"] != "release" {
					t.Fatalf("unrelated work lost: %v", results)
				}
				invoke("3", "cancel")
				send(map[string]any{"v": 1, "type": "cancel", "id": "3"})
				checkError(receive(), "3", "CANCELLED")
				invoke("4", "fresh")
				if m := receive(); m["type"] != "result" || m["id"] != "4" || m["output"] != "fresh" {
					t.Fatalf("subsequent invocation: %v", m)
				}
				send(map[string]any{"v": 1, "type": "shutdown"})
				if m := receive(); m["type"] != "stopped" {
					t.Fatal(m)
				}
				if err := cmd.Wait(); err != nil {
					t.Fatalf("runtime exit: %v: %s", err, logs.String())
				}
			})
		}
	}
}
