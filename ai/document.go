package ai

import (
	"encoding/json"
	"errors"
)

// Document is the explicit same-wire raw escape hatch. Unknown fields, signed
// blocks and provider references survive because only explicitly replaced root
// fields are changed. It is NOT a semantic transcript importer or migration API.
// Callers own validation/order when combining these edits with semantic tools.
type Document struct {
	protocol string
	fields   map[string]json.RawMessage
}

func ParseDocument(protocol string, b []byte) (Document, error) {
	if protocol == "" {
		return Document{}, errors.New("missing protocol")
	}
	m, e := object(b)
	return Document{protocol, m}, e
}
func (d Document) Protocol() string { return d.protocol }
func (d Document) Field(key string) json.RawMessage {
	return append(json.RawMessage(nil), d.fields[key]...)
}
func (d Document) Replace(key string, value json.RawMessage) (Document, error) {
	if d.protocol == "" || d.fields == nil {
		return Document{}, errors.New("uninitialized document")
	}
	if !json.Valid(value) {
		return Document{}, errors.New("invalid JSON replacement")
	}
	out := Document{d.protocol, clone(d.fields)}
	out.fields[key] = append(json.RawMessage(nil), value...)
	return out, nil
}
func (d Document) Encode(protocol string) ([]byte, error) {
	if d.protocol == "" || d.fields == nil || protocol != d.protocol {
		return nil, ErrUnsupported
	}
	return json.Marshal(d.fields)
}

// Passthrough is explicit and unchanged; Encode intentionally rejects semantic
// edits on opaque responses. Do not use Passthrough after editing Response.
func (a Adapter) Passthrough(r Response) ([]byte, error) {
	if r.Source != a.Kind || !json.Valid(r.Original) {
		return nil, ErrUnsupported
	}
	return append([]byte(nil), r.Original...), nil
}
