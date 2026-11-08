package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Wire validation runs before convenience getters. Missing optional fields are
// allowed; null is allowed only where the protocol explicitly uses it.
type wireFields map[string]string

func fields(m map[string]json.RawMessage, spec wireFields) error {
	for k, typ := range spec {
		raw, ok := m[k]
		if !ok {
			continue
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			return fmt.Errorf("invalid %s", k)
		}
		if raw[0] == 'n' {
			if typ[0] == '?' {
				continue
			}
			return fmt.Errorf("null %s", k)
		}
		if typ[0] == '?' {
			typ = typ[1:]
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
		valid := false
		switch typ {
		case "string":
			_, valid = v.(string)
		case "object":
			_, valid = v.(map[string]any)
		case "array":
			var items []map[string]json.RawMessage
			valid = json.Unmarshal(raw, &items) == nil
			for _, item := range items {
				if item == nil {
					valid = false
				}
			}
		case "int":
			var n int64
			valid = json.Unmarshal(raw, &n) == nil
		}
		if !valid {
			return fmt.Errorf("invalid %s: expected %s", k, typ)
		}
	}
	return nil
}
func requiredString(m map[string]json.RawMessage, k, want string) error {
	if err := fields(m, wireFields{k: "string"}); err != nil {
		return err
	}
	if get[string](m, k) != want {
		return fmt.Errorf("invalid %s: expected %s", k, want)
	}
	return nil
}
func chatMessage(m map[string]json.RawMessage, delta bool) error {
	if err := fields(m, wireFields{"role": "string", "content": "?string", "tool_calls": "array"}); err != nil {
		return err
	}
	if _, ok := m["role"]; ok {
		if err := requiredString(m, "role", "assistant"); err != nil {
			return err
		}
	} else if !delta {
		return fmt.Errorf("missing assistant role")
	}
	for _, t := range get[[]map[string]json.RawMessage](m, "tool_calls") {
		if err := fields(t, wireFields{"index": "int", "id": "string", "type": "string", "function": "object"}); err != nil {
			return err
		}
		if _, ok := t["type"]; ok {
			if err := requiredString(t, "type", "function"); err != nil {
				return err
			}
		}
		if err := fields(get[map[string]json.RawMessage](t, "function"), wireFields{"name": "string", "arguments": "string"}); err != nil {
			return err
		}
	}
	return nil
}
func validateWire(m map[string]json.RawMessage, kind string, stream bool, event string) error {
	if err := fields(m, wireFields{"id": "string", "model": "string", "usage": "?object"}); err != nil {
		return err
	}
	if kind == Chat {
		if err := fields(m, wireFields{"object": "string", "created": "int", "system_fingerprint": "?string", "service_tier": "?string", "choices": "array"}); err != nil {
			return err
		}
		if _, ok := m["object"]; ok {
			want := "chat.completion"
			if stream {
				want = "chat.completion.chunk"
			}
			if err := requiredString(m, "object", want); err != nil {
				return err
			}
		}
		if _, ok := m["choices"]; !ok {
			return fmt.Errorf("missing choices")
		}
		var cs []map[string]json.RawMessage
		if err := json.Unmarshal(m["choices"], &cs); err != nil {
			return err
		}
		for _, c := range cs {
			key := "message"
			if stream {
				key = "delta"
			}
			if err := fields(c, wireFields{"index": "int", key: "object", "finish_reason": "?string"}); err != nil {
				return err
			}
			if _, ok := c["index"]; ok && get[int](c, "index") != 0 {
				return ErrUnsupported
			}
			v, err := object(c[key])
			if err != nil {
				return err
			}
			// Ordinary Chat content can be opaque multimodal data, never flattened.
			if !stream {
				copy := clone(v)
				delete(copy, "content")
				if err = chatMessage(copy, false); err != nil {
					return err
				}
			} else if err = chatMessage(v, true); err != nil {
				return err
			}
		}
		return nil
	}
	if err := fields(m, wireFields{"type": "string", "role": "string", "content": "array", "stop_reason": "?string", "stop_sequence": "?string", "index": "int", "message": "object", "content_block": "object", "delta": "object"}); err != nil {
		return err
	}
	if !stream {
		if err := requiredString(m, "type", "message"); err != nil {
			return err
		}
		if err := requiredString(m, "role", "assistant"); err != nil {
			return err
		}
		if get[string](m, "stop_reason") == "" {
			return fmt.Errorf("missing terminal stop_reason")
		}
	}
	for _, b := range get[[]map[string]json.RawMessage](m, "content") {
		if err := anthropicBlock(b); err != nil {
			return err
		}
	}
	if stream {
		if event == "message_start" {
			if _, ok := m["message"]; !ok {
				return fmt.Errorf("missing initial message")
			}
		}
		if v, ok := m["message"]; ok {
			child, err := object(v)
			if err != nil {
				return err
			}
			if err = validateWire(child, Messages, true, ""); err != nil {
				return err
			}
			if err = requiredString(child, "type", "message"); err != nil {
				return err
			}
			if err = requiredString(child, "role", "assistant"); err != nil {
				return err
			}
			if _, ok := child["content"]; !ok {
				return fmt.Errorf("missing initial content")
			}
			if get[string](child, "stop_reason") != "" || get[string](child, "stop_sequence") != "" {
				return fmt.Errorf("completed message in stream start")
			}
		}
		if v, ok := m["content_block"]; ok {
			child, err := object(v)
			if err != nil {
				return err
			}
			if err = anthropicBlock(child); err != nil {
				return err
			}
		}
		if v, ok := m["delta"]; ok {
			child, err := object(v)
			if err != nil {
				return err
			}
			if err = fields(child, wireFields{"type": "string", "text": "string", "partial_json": "string", "stop_reason": "?string", "stop_sequence": "?string"}); err != nil {
				return err
			}
			if event == "content_block_delta" {
				key := "text"
				if get[string](child, "type") == "input_json_delta" {
					key = "partial_json"
				}
				if _, ok := child[key]; !ok {
					return fmt.Errorf("missing %s", key)
				}
			}
		}
	}
	return nil
}
func anthropicBlock(b map[string]json.RawMessage) error {
	if err := fields(b, wireFields{"type": "string", "text": "string", "id": "string", "name": "string", "input": "object"}); err != nil {
		return err
	}
	switch get[string](b, "type") {
	case "text":
		if _, ok := b["text"]; !ok {
			return fmt.Errorf("missing text")
		}
	case "tool_use":
		if _, ok := b["input"]; !ok {
			return fmt.Errorf("missing tool input")
		}
	}
	return nil
}
