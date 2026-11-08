package worker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// DecodeJSON decodes one UTF-8 JSON value, rejecting duplicate keys and
// requiring exact (case-sensitive) struct field names.
func DecodeJSON(b []byte, target any) error { return decodeFrame(b, target) }

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
	if err := exactFields(b, reflect.TypeOf(target)); err != nil {
		return err
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(target)
}

// encoding/json matches struct fields case-insensitively. Wire contracts do not:
// validate each structured boundary before decoding the representation we commit.
// RawMessage and interface values are deliberately opaque JSON; their consumers
// validate them against their own schemas when they become structured values.
func exactFields(b []byte, typ reflect.Type) error {
	if typ == nil || typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return nil // Required/non-null fields are checked by the schema consumer.
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			return err
		}
		allowed := make(map[string]reflect.Type, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			allowed[name] = f.Type
		}
		for key, value := range fields {
			field, ok := allowed[key]
			if !ok {
				return errors.New("unknown or non-exact JSON field: " + key)
			}
			if err := exactFields(value, field); err != nil {
				return err
			}
		}
	case reflect.Map:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			return err
		}
		for _, value := range fields {
			if err := exactFields(value, typ.Elem()); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		// Byte slices use encoding/json's base64 string representation.
		if typ.Elem().Kind() == reflect.Uint8 {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(b, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := exactFields(value, typ.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func hookShapes(b []byte) error {
	var m struct {
		Hooks []map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for _, h := range m.Hooks {
		if len(h) < 3 || len(h) > 9 {
			return errors.New("invalid hook fields")
		}
		for _, key := range []string{"id", "stage", "bodyMode"} {
			v, ok := h[key]
			if !ok || bytes.Equal(v, []byte("null")) {
				return errors.New("missing hook field")
			}
		}
		for key, v := range h {
			if key != "id" && key != "stage" && key != "bodyMode" && key != "order" && key != "failure" && key != "formats" && key != "requires" && key != "before" && key != "after" {
				return errors.New("unknown hook field")
			}
			if bytes.Equal(v, []byte("null")) {
				return errors.New("null descriptor field")
			}
			if key == "failure" {
				var s string
				if json.Unmarshal(v, &s) != nil || (s != "block" && s != "failOpen") {
					return errors.New("invalid failure policy")
				}
			}
			if key == "formats" || key == "requires" || key == "before" || key == "after" {
				var values []string
				if json.Unmarshal(v, &values) != nil {
					return errors.New("invalid descriptor list")
				}
				for _, s := range values {
					if s == "" || len(s) > 256 {
						return errors.New("invalid descriptor list member")
					}
				}
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
	shapes := map[string][]string{"ready": {"v", "type", "hooks"}, "result": {"v", "type", "id", "output"}, "error": {"v", "type", "id", "error"}, "stopped": {"v", "type"}, "call": {"v", "type", "id", "callId", "request"}}
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
