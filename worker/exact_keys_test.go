package worker

import (
	"encoding/json"
	"testing"
)

func TestExactStructuredWireKeys(t *testing.T) {
	var wire struct {
		V     int              `json:"v"`
		Hooks []HookDescriptor `json:"hooks"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Input json.RawMessage `json:"input"`
	}
	for _, raw := range []string{
		`{"V":1}`, `{"v":1,"V":1}`,
		`{"hooks":[{"id":"x","Stage":"request","bodyMode":"none"}]}`,
		`{"hooks":[{"id":"x","stage":"request","bodyMode":"none","Failure":"block"}]}`,
		`{"error":{"Code":"x","message":"x"}}`,
		`{"error":{"code":"x","message":"x","Message":null}}`,
	} {
		if err := DecodeJSON([]byte(raw), &wire); err == nil {
			t.Fatalf("accepted non-exact wire field: %s", raw)
		}
	}
	// Arbitrary application map keys are not wire struct field names.
	if err := DecodeJSON([]byte(`{"v":1,"input":{"Metadata":null,"metadata":{}},"hooks":[{"id":"x","stage":"request","bodyMode":"none"}]}`), &wire); err != nil {
		t.Fatal(err)
	}
}
