package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const Chat = "openai-chat"
const Messages = "anthropic-messages"

// Protocol is deliberately extensible without editing a registry or transport.
// Implementations must reject transformations of unsupported opaque state.
type Protocol interface {
	Name() string
	Decode([]byte) (Response, error)
	Encode(Response) ([]byte, error)
	EncodeRequest(Request) ([]byte, error)
}

// StreamingProtocol is optional; protocols without SSE need only Protocol.
type StreamingProtocol interface {
	Protocol
	DecodeStream(io.Reader, StreamLimits) (Response, []Event, error)
	EncodeStream(Response) ([]byte, error)
}

var _ StreamingProtocol = Adapter{}

type Adapter struct{ Kind string }

func (a Adapter) Name() string { return a.Kind }
func object(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	err := json.Unmarshal(b, &m)
	if err == nil && m == nil {
		err = errors.New("expected JSON object")
	}
	return m, err
}
func put(m map[string]json.RawMessage, k string, v any) { b, _ := json.Marshal(v); m[k] = b }
func get[T any](m map[string]json.RawMessage, k string) T {
	var v T
	_ = json.Unmarshal(m[k], &v)
	return v
}
func take(m map[string]json.RawMessage, keys ...string) {
	for _, k := range keys {
		delete(m, k)
	}
}
func (a Adapter) Decode(b []byte) (Response, error) {
	m, err := object(b)
	if err != nil {
		return Response{}, err
	}
	if err := validateWire(m, a.Kind, false, ""); err != nil {
		return Response{}, err
	}
	r := Response{ID: get[string](m, "id"), Model: get[string](m, "model"), Source: a.Kind, Original: append([]byte(nil), b...)}
	if a.Kind == Chat {
		var choices []struct {
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Calls   []struct {
					ID, Type string
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			}
			Finish string `json:"finish_reason"`
		}
		if err = json.Unmarshal(m["choices"], &choices); err != nil || len(choices) != 1 {
			return r, fmt.Errorf("%w: exactly one choice required", ErrUnsupported)
		}
		c := choices[0]
		if c.Message.Role != "assistant" || c.Finish == "" {
			return r, errors.New("incomplete assistant response")
		}
		if string(c.Message.Content) != "null" && len(c.Message.Content) > 0 {
			if json.Unmarshal(c.Message.Content, &r.Text) != nil {
				r.Opaque = append([]byte(nil), b...)
			}
		}
		r.Stop = c.Finish
		for _, t := range c.Message.Calls {
			if t.Type != "function" {
				r.Opaque = append([]byte(nil), b...)
			}
			r.Calls = append(r.Calls, ToolCall{t.ID, t.Function.Name, json.RawMessage(t.Function.Arguments)})
		}
		var e error
		r.Usage, e = decodeUsage(m["usage"], "prompt_tokens", "completion_tokens")
		if e != nil {
			return r, e
		}
		// Nested unknown fields must not be lost during semantic transforms.
		ch := get[[]map[string]json.RawMessage](m, "choices")
		msg := get[map[string]json.RawMessage](ch[0], "message")
		take(msg, "role", "content", "tool_calls")
		if len(msg) > 0 {
			r.Opaque = append([]byte(nil), b...)
		}
		for _, v := range get[[]map[string]json.RawMessage](m, "choices") {
			take(v, "index", "message", "finish_reason")
			if len(v) > 0 {
				r.Opaque = append([]byte(nil), b...)
			}
		}
		for _, v := range get[[]map[string]json.RawMessage](get[map[string]json.RawMessage](ch[0], "message"), "tool_calls") {
			f := get[map[string]json.RawMessage](v, "function")
			take(f, "name", "arguments")
			take(v, "id", "type", "function")
			if len(v) > 0 || len(f) > 0 {
				r.Opaque = append([]byte(nil), b...)
			}
		}
		take(m, "id", "object", "created", "model", "choices", "usage")
	} else if a.Kind == Messages {
		blocks := get[[]map[string]json.RawMessage](m, "content")
		if blocks == nil {
			return r, errors.New("missing content blocks")
		}
		for _, v := range blocks {
			switch get[string](v, "type") {
			case "text":
				if len(r.Calls) > 0 {
					r.Opaque = append([]byte(nil), b...)
				}
				var text string
				if err := json.Unmarshal(v["text"], &text); err != nil {
					return r, err
				}
				r.Text += text
				take(v, "type", "text")
			case "tool_use":
				r.Calls = append(r.Calls, ToolCall{get[string](v, "id"), get[string](v, "name"), append([]byte(nil), v["input"]...)})
				take(v, "type", "id", "name", "input")
			default:
				r.Opaque = append([]byte(nil), b...)
			}
			if len(v) > 0 {
				r.Opaque = append([]byte(nil), b...)
			}
		}
		if v := m["stop_sequence"]; len(v) > 0 && string(v) != "null" {
			r.Opaque = append([]byte(nil), b...)
		}
		r.Stop = get[string](m, "stop_reason")
		var e error
		r.Usage, e = decodeUsage(m["usage"], "input_tokens", "output_tokens")
		if e != nil {
			return r, e
		}
		take(m, "id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage")
	} else {
		return r, ErrUnsupported
	}
	r.Extra = m
	if err := validateResponse(r); err != nil {
		return r, err
	}
	if len(r.Calls) > 0 {
		if err := ValidateMessages(append([]Message{{Role: "assistant", Calls: r.Calls}}, toolPlaceholders(r.Calls)...)); err != nil {
			return r, err
		}
	}
	return r, nil
}
func usageFields(m map[string]json.RawMessage, r Response, kind string) map[string]json.RawMessage {
	if r.Source == kind {
		if u, e := object(m["usage"]); e == nil {
			return u
		}
	}
	return map[string]json.RawMessage{}
}
func decodeUsage(raw json.RawMessage, input, output string) (Usage, error) {
	var u Usage
	if len(raw) == 0 || string(raw) == "null" {
		return u, nil
	}
	m, e := object(raw)
	if e != nil {
		return u, e
	}
	if err := fields(m, wireFields{input: "?int", output: "?int", "total_tokens": "?int"}); err != nil {
		return u, err
	}
	for k, p := range map[string]**int64{input: &u.Input, output: &u.Output} {
		if b, ok := m[k]; ok {
			if e = json.Unmarshal(b, p); e != nil {
				return u, e
			}
			if *p != nil && **p < 0 {
				return u, errors.New("negative usage")
			}
		}
	}
	return u, nil
}
func validateResponse(r Response) error {
	if r.Stop == "length" || r.Stop == "max_tokens" {
		return errors.New("truncated model completion")
	}
	if r.Source == Chat && r.Stop != "" && r.Stop != "stop" && r.Stop != "tool_calls" && r.Stop != "content_filter" {
		return ErrUnsupported
	}
	if r.Source == Messages && r.Stop != "" && r.Stop != "end_turn" && r.Stop != "tool_use" {
		return ErrUnsupported
	}
	if (r.Stop == "tool_calls" || r.Stop == "tool_use") && len(r.Calls) == 0 {
		return errors.New("missing tool calls")
	}
	if len(r.Calls) > 0 && r.Stop != "" && r.Stop != "tool_calls" && r.Stop != "tool_use" {
		return errors.New("tool calls with final text stop")
	}
	if r.Usage.Input != nil && *r.Usage.Input < 0 || r.Usage.Output != nil && *r.Usage.Output < 0 {
		return errors.New("negative usage")
	}
	if len(r.Calls) > 0 {
		return ValidateMessages(append([]Message{{Role: "assistant", Calls: r.Calls}}, toolPlaceholders(r.Calls)...))
	}
	return nil
}
func toolPlaceholders(cs []ToolCall) []Message {
	var ms []Message
	for _, c := range cs {
		ms = append(ms, Message{Role: "tool", ToolID: c.ID})
	}
	return ms
}
func Portable(r Response) error {
	if r.Stop != "" && r.Stop != "stop" && r.Stop != "tool_calls" && r.Stop != "end_turn" && r.Stop != "tool_use" {
		return ErrUnsupported
	}
	if len(r.Original) > 0 {
		m, e := object(r.Original)
		if e != nil {
			return e
		}
		if raw := m["usage"]; len(raw) > 0 && string(raw) != "null" {
			u, e := object(raw)
			if e != nil {
				return e
			}
			if r.Source == Chat {
				take(u, "prompt_tokens", "completion_tokens", "total_tokens")
			} else {
				take(u, "input_tokens", "output_tokens")
			}
			if len(u) > 0 {
				return fmt.Errorf("%w: unknown usage detail", ErrUnsupported)
			}
		}
	}
	if len(r.Opaque) > 0 || len(r.Extra) > 0 {
		return fmt.Errorf("%w: opaque or unknown provider state", ErrUnsupported)
	}
	return nil
}
func (a Adapter) Encode(r Response) ([]byte, error) {
	if _, err := json.Marshal(r); err != nil {
		return nil, err
	}
	if err := validateResponse(r); err != nil {
		return nil, err
	}
	if len(r.Opaque) > 0 {
		return nil, fmt.Errorf("%w: use original opaque bytes only for unchanged passthrough", ErrUnsupported)
	}
	if r.Source != "" && r.Source != a.Kind {
		if err := Portable(r); err != nil {
			return nil, err
		}
	}
	m := clone(r.Extra)
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	if r.Source == a.Kind && len(r.Original) > 0 {
		var err error
		m, err = object(r.Original)
		if err != nil {
			return nil, err
		}
		for k, v := range r.Extra {
			m[k] = v
		}
	}
	put(m, "id", r.ID)
	put(m, "model", r.Model)
	if a.Kind == Chat {
		calls := []any{}
		for _, c := range r.Calls {
			calls = append(calls, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Arguments)}})
		}
		msg := map[string]any{"role": "assistant", "content": r.Text}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		stop := "stop"
		if len(calls) > 0 {
			stop = "tool_calls"
		}
		if r.Source == Chat && r.Stop != "" {
			stop = r.Stop
		}
		put(m, "object", "chat.completion")
		put(m, "choices", []any{map[string]any{"index": 0, "message": msg, "finish_reason": stop}})
		u := usageFields(m, r, a.Kind)
		take(u, "prompt_tokens", "completion_tokens", "total_tokens")
		if r.Usage.Input != nil {
			put(u, "prompt_tokens", *r.Usage.Input)
		}
		if r.Usage.Output != nil {
			put(u, "completion_tokens", *r.Usage.Output)
		}
		// Derived totals must never survive edits to either source counter.
		if r.Usage.Input != nil && r.Usage.Output != nil {
			input, output := *r.Usage.Input, *r.Usage.Output
			if input > int64(^uint64(0)>>1)-output {
				return nil, errors.New("usage total overflow")
			}
			put(u, "total_tokens", input+output)
		}
		delete(m, "usage")
		if len(u) > 0 {
			put(m, "usage", u)
		}
	} else if a.Kind == Messages {
		content := []any{}
		if r.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": r.Text})
		}
		for _, c := range r.Calls {
			content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Arguments})
		}
		stop := "end_turn"
		if len(r.Calls) > 0 {
			stop = "tool_use"
		}
		if r.Source == Messages && r.Stop != "" {
			stop = r.Stop
		}
		put(m, "type", "message")
		put(m, "role", "assistant")
		put(m, "content", content)
		put(m, "stop_reason", stop)
		put(m, "stop_sequence", nil)
		u := usageFields(m, r, a.Kind)
		take(u, "input_tokens", "output_tokens")
		if r.Usage.Input != nil {
			put(u, "input_tokens", *r.Usage.Input)
		}
		if r.Usage.Output != nil {
			put(u, "output_tokens", *r.Usage.Output)
		}
		put(m, "usage", u)
	} else {
		return nil, ErrUnsupported
	}
	return json.Marshal(m)
}
func (a Adapter) EncodeRequest(r Request) ([]byte, error) {
	if r.Settings.MaxTokens < 0 {
		return nil, errors.New("negative max tokens")
	}
	if r.Settings.Temperature != nil && (*r.Settings.Temperature < 0 || *r.Settings.Temperature > 1) {
		return nil, ErrUnsupported
	}
	if err := ValidateMessages(r.Messages); err != nil {
		return nil, err
	}
	m := map[string]any{"model": r.Target.Model, "stream": r.Stream}
	ms := []any{}
	tools := []any{}
	for _, t := range r.Tools {
		if !json.Valid(t.Schema) {
			return nil, errors.New("invalid tool schema")
		}
		if a.Kind == Chat {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}})
		} else {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
		}
	}
	for _, v := range r.Messages {
		if a.Kind == Chat {
			msg := map[string]any{"role": v.Role, "content": v.Text}
			if v.ToolID != "" {
				msg["tool_call_id"] = v.ToolID
			}
			if len(v.Calls) > 0 {
				cs := []any{}
				for _, c := range v.Calls {
					cs = append(cs, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": string(c.Arguments)}})
				}
				msg["tool_calls"] = cs
			}
			ms = append(ms, msg)
		} else if a.Kind == Messages {
			if v.Role == "system" {
				return nil, fmt.Errorf("%w: use explicit Anthropic system adapter", ErrUnsupported)
			}
			role := v.Role
			content := []any{}
			if v.Role == "tool" {
				role = "user"
				content = append(content, map[string]any{"type": "tool_result", "tool_use_id": v.ToolID, "content": v.Text})
			} else {
				if v.Text != "" {
					content = append(content, map[string]any{"type": "text", "text": v.Text})
				}
				for _, c := range v.Calls {
					content = append(content, map[string]any{"type": "tool_use", "id": c.ID, "name": c.Name, "input": c.Arguments})
				}
			}
			ms = append(ms, map[string]any{"role": role, "content": content})
		} else {
			return nil, ErrUnsupported
		}
	}
	m["messages"] = ms
	if len(tools) > 0 {
		m["tools"] = tools
	}
	if r.Settings.MaxTokens > 0 {
		m["max_tokens"] = r.Settings.MaxTokens
	} else if a.Kind == Messages {
		return nil, errors.New("Anthropic requires max_tokens")
	}
	if r.Settings.Temperature != nil {
		m["temperature"] = *r.Settings.Temperature
	}
	return json.Marshal(m)
}
