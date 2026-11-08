package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// Decode one UTF-8 JSON value, rejecting duplicate keys as well as unknown struct fields.
func decodeFrame(b []byte, target any) error {
	if !utf8.Valid(b) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value func() error
	value = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return errors.New("invalid or duplicate JSON key")
				}
				seen[s] = true
				if err = value(); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		case '[':
			for d.More() {
				if err = value(); err != nil {
					return err
				}
			}
			_, err = d.Token()
			return err
		}
		return errors.New("invalid JSON delimiter")
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func hookShapes(b []byte) error {
	var m struct {
		Hooks []map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for _, h := range m.Hooks {
		if len(h) < 3 || len(h) > 4 {
			return errors.New("invalid hook fields")
		}
		for _, key := range []string{"id", "stage", "bodyMode"} {
			v, ok := h[key]
			if !ok || bytes.Equal(v, []byte("null")) {
				return errors.New("missing hook field")
			}
		}
		for key, v := range h {
			if key != "id" && key != "stage" && key != "bodyMode" && key != "order" {
				return errors.New("unknown hook field")
			}
			if key == "order" && bytes.Equal(v, []byte("null")) {
				return errors.New("order must be numeric")
			}
		}
	}
	return nil
}
func messageShape(b []byte, kind string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	shapes := map[string][]string{"ready": {"v", "type", "hooks"}, "result": {"v", "type", "id", "output"}, "error": {"v", "type", "id", "error"}, "stopped": {"v", "type"}}
	allowed, ok := shapes[kind]
	if !ok {
		return errors.New("unknown worker message")
	}
	if len(fields) != len(allowed) {
		return errors.New("invalid worker message shape")
	}
	for _, key := range allowed {
		if _, ok := fields[key]; !ok {
			return errors.New("missing worker message field")
		}
	}
	return nil
}
