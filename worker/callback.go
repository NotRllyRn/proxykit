package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Caller holds p.mu. Admission is independent of the native engine's slots.
func (p *process) receiveCallLocked(id, callID string, request json.RawMessage) error {
	c := p.pending[id]
	n, err := strconv.ParseUint(callID, 10, 64)
	if c == nil || c.ctx == nil || c.callback == nil || c.callbackActive || err != nil || n == 0 || strconv.FormatUint(n, 10) != callID || n != c.nextCall+1 || len(request) == 0 {
		return errors.New("invalid or expired worker callback scope")
	}
	// Validate correlation before treating cancellation as an ordinary in-flight race.
	c.nextCall = n
	if c.cancelled || c.ctx.Err() != nil {
		return nil
	}
	select {
	case p.owner.callbackSlots <- struct{}{}:
	default:
		b, _ := json.Marshal(map[string]any{"v": 1, "type": "callResult", "id": id, "callId": callID, "error": "worker callback capacity exceeded"})
		return p.enqueueLocked(b)
	}
	c.callbackActive = true
	go func() {
		output, err := c.callback(c.ctx, append(json.RawMessage(nil), request...))
		if err == nil && !json.Valid(output) {
			err = errors.New("invalid callback output")
		}
		reply := map[string]any{"v": 1, "type": "callResult", "id": id, "callId": callID}
		if err != nil {
			reply["error"] = fmt.Sprint(err)
		} else {
			reply["output"] = output
		}
		b, marshalErr := json.Marshal(reply)
		if marshalErr != nil || len(b) > p.owner.options.MaxFrame {
			b, _ = json.Marshal(map[string]any{"v": 1, "type": "callResult", "id": id, "callId": callID, "error": "callback response exceeds frame bound"})
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		// The callback and response preparation have settled. Release before the
		// writer can make this result observable to the next sequential call.
		<-p.owner.callbackSlots
		c.callbackActive = false
		if p.pending[id] != c || c.cancelled || c.ctx.Err() != nil || p.stopping || p.failure != nil {
			return
		}
		if err := p.enqueueLocked(b); err != nil {
			go p.fail(err)
		}
	}()
	return nil
}
