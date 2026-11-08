package nodehost

import (
	"encoding/json"
	"errors"
	"strings"
)

func validToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)) {
			continue
		}
		return false
	}
	return true
}

// encoding/json accepts null as a zero string: validate the exact wire shape as
// well as HTTP field syntax, rather than silently normalizing malformed output.
func headerShape(raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return errors.New("headers must be an object")
	}
	for k, v := range fields {
		if !validToken(k) {
			return errors.New("invalid header name")
		}
		var values []json.RawMessage
		if len(v) == 0 || v[0] != '[' || json.Unmarshal(v, &values) != nil {
			return errors.New("headers must be string arrays")
		}
		for _, raw := range values {
			var value string
			if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &value) != nil {
				return errors.New("header values must be strings")
			}
			for i := 0; i < len(value); i++ {
				c := value[i]
				if (c < 32 && c != '\t') || c == 127 {
					return errors.New("invalid header value")
				}
			}
		}
	}
	return nil
}
