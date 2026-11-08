package aihost

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/NotRllyRn/proxykit/ai"
)

// This intentionally narrow importer rejects unknown/opaque request semantics,
// including provider-held IDs, signed content, supplied Turn/identity and extras.
func strict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

type chatCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
type chatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string     `json:"role"`
		Content string     `json:"content"`
		ToolID  string     `json:"tool_call_id"`
		Calls   []chatCall `json:"tool_calls"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
	Stream      bool     `json:"stream"`
	MaxTokens   int      `json:"max_tokens"`
	Temperature *float64 `json:"temperature"`
}

func decodeRequest(b []byte) (ai.Request, error) {
	var wire chatRequest
	if err := strict(b, &wire); err != nil {
		return ai.Request{}, err
	}
	req := ai.Request{Requested: ai.Route{Model: wire.Model, Protocol: ai.Chat}, Stream: wire.Stream, Settings: ai.Settings{MaxTokens: wire.MaxTokens, Temperature: wire.Temperature}}
	if wire.Model == "" || len(wire.Messages) == 0 {
		return req, errors.New("model/messages required")
	}
	for _, m := range wire.Messages {
		next := ai.Message{Role: m.Role, Text: m.Content, ToolID: m.ToolID}
		for _, c := range m.Calls {
			if c.Type != "function" {
				return req, ai.ErrUnsupported
			}
			next.Calls = append(next.Calls, ai.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: json.RawMessage(c.Function.Arguments)})
		}
		req.Messages = append(req.Messages, next)
	}
	seen := map[string]bool{}
	for _, t := range wire.Tools {
		if t.Type != "function" || t.Function.Name == "" || seen[t.Function.Name] || !json.Valid(t.Function.Parameters) {
			return req, ai.ErrUnsupported
		}
		seen[t.Function.Name] = true
		req.Tools = append(req.Tools, ai.ToolDefinition{Name: t.Function.Name, Description: t.Function.Description, Schema: t.Function.Parameters})
	}
	// Pending calls in a resumed public transcript are completed by client results.
	if err := ai.ValidateMessages(req.Messages); err != nil {
		return req, err
	}
	return req, nil
}
