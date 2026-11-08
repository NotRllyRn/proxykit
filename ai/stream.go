package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type Event struct {
	Name, ID, Data string
	Opaque         []string
}
type StreamLimits struct{ Bytes, EventBytes, Events int }

func (l StreamLimits) defaults() StreamLimits {
	if l.Bytes <= 0 {
		l.Bytes = 4 << 20
	}
	if l.EventBytes <= 0 {
		l.EventBytes = 256 << 10
	}
	if l.Events <= 0 {
		l.Events = 10000
	}
	return l
}

// ReadEvents retains unknown fields/comments as Opaque. A partial event at EOF
// is rejected, not silently converted into a successful completion.
func ReadEvents(r io.Reader, l StreamLimits) ([]Event, error) {
	l = l.defaults()
	br := bufio.NewReader(io.LimitReader(r, int64(l.Bytes)+1))
	var out []Event
	var e Event
	total, size := 0, 0
	dirty := false
	for {
		line, err := br.ReadString('\n')
		total += len(line)
		size += len(line)
		if total > l.Bytes || size > l.EventBytes {
			return nil, ErrLimit
		}
		if err != nil {
			if err == io.EOF && line == "" && !dirty {
				return out, nil
			}
			return nil, fmt.Errorf("truncated SSE: %w", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if dirty {
				e.Data = strings.TrimSuffix(e.Data, "\n")
				out = append(out, e)
				if len(out) > l.Events {
					return nil, ErrLimit
				}
			}
			e = Event{}
			size = 0
			dirty = false
			continue
		}
		dirty = true
		k, v, ok := strings.Cut(line, ":")
		if strings.HasPrefix(v, " ") {
			v = v[1:]
		}
		if !ok {
			v = ""
		}
		switch k {
		case "event":
			e.Name = v
		case "id":
			e.ID = v
		case "data":
			e.Data += v + "\n"
		default:
			e.Opaque = append(e.Opaque, line)
		}
	}
}
func WriteEvents(es []Event) []byte {
	var b strings.Builder
	for _, e := range es {
		if e.Name != "" {
			fmt.Fprintf(&b, "event: %s\n", e.Name)
		}
		if e.ID != "" {
			fmt.Fprintf(&b, "id: %s\n", e.ID)
		}
		for _, l := range e.Opaque {
			b.WriteString(l + "\n")
		}
		if e.Data != "" {
			for _, l := range strings.Split(e.Data, "\n") {
				b.WriteString("data: " + l + "\n")
			}
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
func event(name string, v any) Event {
	b, _ := json.Marshal(v)
	return Event{Name: name, Data: string(b)}
}

// DecodeStream buffers a supported single answer. Events are also returned so
// callers can preserve unknown/opaque events unchanged, not silently drop them.
// No speculative bytes are delivered to clients here.
func (a Adapter) DecodeStream(rd io.Reader, l StreamLimits) (Response, []Event, error) {
	es, err := ReadEvents(rd, l)
	if err != nil {
		return Response{}, nil, err
	}
	r := Response{Source: a.Kind, Events: clone(es), Extra: map[string]json.RawMessage{}}
	done, finish, started := false, false, false
	calls := map[int]*ToolCall{}
	blocks := map[int]string{}
	closed := map[int]bool{}
	for _, e := range es {
		if e.Data == "" {
			continue
		}
		if done {
			return r, es, errors.New("data after terminal event")
		}
		if a.Kind == Chat && e.Data == "[DONE]" {
			if !finish {
				return r, es, errors.New("missing finish reason")
			}
			done = true
			continue
		}
		m, er := object([]byte(e.Data))
		if er != nil {
			return r, es, er
		}
		if er := validateWire(m, a.Kind, true, e.Name); er != nil {
			return r, es, er
		}
		if a.Kind == Chat {
			for _, key := range []string{"system_fingerprint", "service_tier"} {
				if raw, ok := m[key]; ok {
					r.Extra[key] = append([]byte(nil), raw...)
				}
			}
			if e.Name != "" {
				return r, es, ErrUnsupported
			}
			known := clone(m)
			take(known, "id", "object", "created", "model", "choices", "usage", "system_fingerprint", "service_tier")
			if len(known) > 0 {
				return r, es, fmt.Errorf("%w: unknown stream fields retained", ErrUnsupported)
			}
			if get[string](m, "model") != "" {
				r.Model = get[string](m, "model")
			}
			if get[string](m, "id") != "" {
				r.ID = get[string](m, "id")
			}
			cs := get[[]map[string]json.RawMessage](m, "choices")
			if raw, ok := m["usage"]; ok && string(raw) != "null" {
				var e error
				r.Usage, e = decodeUsage(raw, "prompt_tokens", "completion_tokens")
				if e != nil {
					return r, es, e
				}
				u := get[map[string]json.RawMessage](m, "usage")
				take(u, "prompt_tokens", "completion_tokens", "total_tokens")
				if len(u) > 0 {
					return r, es, ErrUnsupported
				}
			}
			for _, c := range cs {
				if finish {
					return r, es, errors.New("choice after finish")
				}
				knownChoice := clone(c)
				take(knownChoice, "index", "delta", "finish_reason")
				if len(knownChoice) > 0 {
					return r, es, fmt.Errorf("%w: unknown choice fields retained in events", ErrUnsupported)
				}
				var choiceIndex int
				if er := json.Unmarshal(c["index"], &choiceIndex); er != nil {
					return r, es, er
				}
				if choiceIndex != 0 {
					return r, es, ErrUnsupported
				}
				d := get[map[string]json.RawMessage](c, "delta")
				if d == nil {
					return r, es, errors.New("missing stream delta")
				}
				if v, ok := d["content"]; ok && string(v) != "null" {
					var text string
					if er := json.Unmarshal(v, &text); er != nil {
						return r, es, ErrUnsupported
					}
				}
				r.Text += get[string](d, "content")
				for _, v := range get[[]map[string]json.RawMessage](d, "tool_calls") {
					var idx int
					if er := json.Unmarshal(v["index"], &idx); er != nil {
						return r, es, er
					}
					if idx < 0 || idx > 1024 {
						return r, es, ErrLimit
					}
					t := calls[idx]
					if t == nil {
						t = &ToolCall{}
						calls[idx] = t
					}
					t.ID += get[string](v, "id")
					f := get[map[string]json.RawMessage](v, "function")
					if typ := get[string](v, "type"); typ != "" && typ != "function" {
						return r, es, ErrUnsupported
					}
					vf := clone(v)
					take(vf, "index", "id", "type", "function")
					ff := clone(f)
					take(ff, "name", "arguments")
					if len(vf) > 0 || len(ff) > 0 {
						return r, es, ErrUnsupported
					}
					t.Name += get[string](f, "name")
					t.Arguments = append(t.Arguments, []byte(get[string](f, "arguments"))...)
				}
				take(d, "role", "content", "tool_calls")
				if len(d) > 0 {
					return r, es, fmt.Errorf("%w: unknown stream delta retained in events", ErrUnsupported)
				}
				if v := get[string](c, "finish_reason"); v != "" {
					r.Stop = v
					finish = true
				}
			}
		} else if a.Kind == Messages {
			if get[string](m, "type") != e.Name {
				return r, es, fmt.Errorf("%w: event/data type mismatch", ErrUnsupported)
			}
			known := clone(m)
			take(known, "type", "index", "message", "content_block", "delta", "usage")
			if len(known) > 0 {
				return r, es, ErrUnsupported
			}
			idx := get[int](m, "index")
			if strings.HasPrefix(e.Name, "content_block_") {
				if er := json.Unmarshal(m["index"], &idx); er != nil {
					return r, es, er
				}
			}
			switch e.Name {
			case "message_start":
				if started {
					return r, es, errors.New("duplicate start")
				}
				started = true
				v := get[map[string]json.RawMessage](m, "message")
				if len(get[[]json.RawMessage](v, "content")) != 0 {
					return r, es, ErrUnsupported
				}
				if role := get[string](v, "role"); role != "" && role != "assistant" {
					return r, es, ErrUnsupported
				}
				r.ID = get[string](v, "id")
				r.Model = get[string](v, "model")
				var er error
				r.Usage, er = decodeUsage(v["usage"], "input_tokens", "output_tokens")
				if er != nil {
					return r, es, er
				}
				u := get[map[string]json.RawMessage](v, "usage")
				take(u, "input_tokens", "output_tokens")
				if len(u) > 0 {
					return r, es, ErrUnsupported
				}
				vv := clone(v)
				take(vv, "id", "model", "type", "role", "content", "stop_reason", "stop_sequence", "usage")
				if len(vv) > 0 {
					return r, es, ErrUnsupported
				}
			case "content_block_start":
				if !started || finish || idx < 0 || idx > 1024 || blocks[idx] != "" {
					return r, es, errors.New("invalid block start")
				}
				v := get[map[string]json.RawMessage](m, "content_block")
				typ := get[string](v, "type")
				vv := clone(v)
				if typ == "text" {
					take(vv, "type", "text")
				} else if typ == "tool_use" {
					take(vv, "type", "id", "name", "input")
				}
				if len(vv) > 0 {
					return r, es, ErrUnsupported
				}
				blocks[idx] = typ
				if typ == "text" {
					if len(calls) > 0 {
						return r, es, ErrUnsupported
					}
					r.Text += get[string](v, "text")
				} else if typ == "tool_use" {
					if string(v["input"]) != "{}" {
						return r, es, fmt.Errorf("%w: nonempty streamed initial tool input", ErrUnsupported)
					}
					calls[idx] = &ToolCall{ID: get[string](v, "id"), Name: get[string](v, "name")}
				} else {
					return r, es, fmt.Errorf("%w: opaque block retained in events", ErrUnsupported)
				}
			case "content_block_delta":
				if blocks[idx] == "" || closed[idx] {
					return r, es, errors.New("delta outside block")
				}
				v := get[map[string]json.RawMessage](m, "delta")
				vv := clone(v)
				if get[string](v, "type") == "text_delta" {
					take(vv, "type", "text")
				} else {
					take(vv, "type", "partial_json")
				}
				if len(vv) > 0 {
					return r, es, ErrUnsupported
				}
				switch get[string](v, "type") {
				case "text_delta":
					if blocks[idx] != "text" {
						return r, es, ErrUnsupported
					}
					r.Text += get[string](v, "text")
				case "input_json_delta":
					if calls[idx] == nil {
						return r, es, ErrUnsupported
					}
					calls[idx].Arguments = append(calls[idx].Arguments, []byte(get[string](v, "partial_json"))...)
				default:
					return r, es, ErrUnsupported
				}
			case "content_block_stop":
				if blocks[idx] == "" || closed[idx] {
					return r, es, errors.New("invalid block stop")
				}
				if call := calls[idx]; call != nil && len(call.Arguments) == 0 {
					call.Arguments = json.RawMessage(`{}`)
				}
				closed[idx] = true
			case "message_delta":
				if !started || finish || len(closed) != len(blocks) {
					return r, es, errors.New("premature/duplicate message delta")
				}
				v := get[map[string]json.RawMessage](m, "delta")
				r.Stop = get[string](v, "stop_reason")
				if raw := v["stop_sequence"]; len(raw) > 0 && string(raw) != "null" {
					return r, es, ErrUnsupported
				}
				take(v, "stop_reason", "stop_sequence")
				if len(v) > 0 {
					return r, es, ErrUnsupported
				}
				u, er := decodeUsage(m["usage"], "input_tokens", "output_tokens")
				if er != nil {
					return r, es, er
				}
				if u.Output != nil {
					r.Usage.Output = u.Output
				}
				fields := get[map[string]json.RawMessage](m, "usage")
				take(fields, "input_tokens", "output_tokens")
				if len(fields) > 0 {
					return r, es, ErrUnsupported
				}
				finish = r.Stop != ""
			case "message_stop":
				if !started || !finish || len(closed) != len(blocks) {
					return r, es, errors.New("incomplete message")
				}
				done = true
			case "ping":
			default:
				return r, es, fmt.Errorf("%w: unknown event retained", ErrUnsupported)
			}
		} else {
			return r, es, ErrUnsupported
		}
	}
	if !done {
		return r, es, errors.New("truncated stream: missing terminal event")
	}
	// Sort tool calls by provider index, not map iteration.
	for i := 0; i <= 1024; i++ {
		if t := calls[i]; t != nil {
			r.Calls = append(r.Calls, *t)
		}
	}
	if len(r.Calls) != len(calls) {
		return r, es, ErrLimit
	}
	if err := validateResponse(r); err != nil {
		return r, es, err
	}
	if len(r.Calls) > 0 {
		if err := ValidateMessages(append([]Message{{Role: "assistant", Calls: r.Calls}}, toolPlaceholders(r.Calls)...)); err != nil {
			return r, es, err
		}
	}
	return r, es, nil
}

// EncodeStream emits valid incremental forms whose accumulated value agrees
// with Encode. It is buffered output, not a live upstream forwarding promise.
func (a Adapter) EncodeStream(r Response) ([]byte, error) {
	if err := Portable(r); err != nil {
		return nil, err
	}
	if _, err := a.Encode(r); err != nil {
		return nil, err
	}
	var es []Event
	if a.Kind == Chat {
		chunk := func(delta any, stop any) Event {
			return event("", map[string]any{"id": r.ID, "model": r.Model, "created": 0, "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": stop}}})
		}
		es = append(es, chunk(map[string]any{"role": "assistant", "content": r.Text}, nil))
		for i, c := range r.Calls {
			es = append(es, chunk(map[string]any{"tool_calls": []any{map[string]any{"index": i, "id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Arguments)}}}}, nil))
		}
		stop := "stop"
		if len(r.Calls) > 0 {
			stop = "tool_calls"
		}
		es = append(es, chunk(map[string]any{}, stop))
		u := map[string]any{}
		if r.Usage.Input != nil {
			u["prompt_tokens"] = *r.Usage.Input
		}
		if r.Usage.Output != nil {
			u["completion_tokens"] = *r.Usage.Output
		}
		if len(u) > 0 {
			es = append(es, event("", map[string]any{"id": r.ID, "choices": []any{}, "usage": u}))
		}
		es = append(es, Event{Data: "[DONE]"})
	} else {
		u := map[string]any{}
		if r.Usage.Input != nil {
			u["input_tokens"] = *r.Usage.Input
		}
		es = append(es, event("message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": r.ID, "model": r.Model, "type": "message", "role": "assistant", "content": []any{}, "usage": u}}))
		idx := 0
		add := func(block, delta any) {
			es = append(es, event("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": block}), event("content_block_delta", map[string]any{"type": "content_block_delta", "index": idx, "delta": delta}), event("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx}))
			idx++
		}
		if r.Text != "" {
			add(map[string]any{"type": "text", "text": ""}, map[string]any{"type": "text_delta", "text": r.Text})
		}
		for _, c := range r.Calls {
			add(map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": map[string]any{}}, map[string]any{"type": "input_json_delta", "partial_json": string(c.Arguments)})
		}
		stop := "end_turn"
		if len(r.Calls) > 0 {
			stop = "tool_use"
		}
		u = map[string]any{}
		if r.Usage.Output != nil {
			u["output_tokens"] = *r.Usage.Output
		}
		es = append(es, event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": u}), event("message_stop", map[string]any{"type": "message_stop"}))
	}
	return WriteEvents(es), nil
}

// DecodeBuffered selects ordinary JSON or bounded SSE without an HTTP client.
func (a Adapter) DecodeBuffered(b []byte, stream bool, l StreamLimits) (Response, error) {
	if !stream {
		return a.Decode(b)
	}
	r, _, e := a.DecodeStream(bytes.NewReader(b), l)
	return r, e
}
