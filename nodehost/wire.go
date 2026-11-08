// Package nodehost activates trusted installed Node extensions on the native engine.
// It is an adapter, not a second execution engine or an isolation boundary.
package nodehost

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/NotRllyRn/proxykit/extension"
	"github.com/NotRllyRn/proxykit/worker"
)

type Message struct {
	Method string      `json:"method"`
	URL    string      `json:"url"`
	Header http.Header `json:"header"`
	Status int         `json:"status"`
	Body   *string     `json:"body,omitempty"`
}
type Identity struct {
	Root     string `json:"root"`
	Exchange string `json:"exchange"`
	Parent   string `json:"parent"`
	Attempt  int    `json:"attempt"`
	Depth    int    `json:"depth"`
}

func identity(i extension.Identity) Identity {
	return Identity{i.Root, i.Exchange, i.Parent, i.Attempt, i.Depth}
}

type Entry struct {
	Identity     Identity `json:"identity"`
	Hook         string   `json:"hook"`
	Stage        string   `json:"stage"`
	Outcome      string   `json:"outcome"`
	RequestedURL string   `json:"requestedUrl"`
	ServedURL    string   `json:"servedUrl"`
	Status       int      `json:"status"`
}
type Authentication struct {
	Subject string            `json:"subject"`
	Claims  map[string]string `json:"claims"`
}
type Delivery struct {
	Status     int    `json:"status"`
	Bytes      int64  `json:"bytes"`
	WriteError string `json:"writeError"`
	FlushError string `json:"flushError"`
	ReadError  string `json:"readError"`
	Returned   bool   `json:"returned"`
	Aborted    bool   `json:"aborted"`
	Hijacked   bool   `json:"hijacked"`
}
type Stream struct {
	StreamID string `json:"streamId"`
	Sequence uint64 `json:"sequence"`
	Data     string `json:"data"`
	End      bool   `json:"end"`
}
type Input struct {
	V          int               `json:"v"`
	Stage      string            `json:"stage"`
	Identity   Identity          `json:"identity"`
	Requested  Message           `json:"requested"`
	Request    Message           `json:"request"`
	Response   Message           `json:"response"`
	Metadata   map[string]string `json:"metadata"`
	Ledger     []Entry           `json:"ledger"`
	Truncated  bool              `json:"truncated"`
	Provenance []Identity        `json:"provenance"`
	Auth       Authentication    `json:"auth"`
	Error      string            `json:"error"`
	Delivery   *Delivery         `json:"delivery,omitempty"`
	Stream     *Stream           `json:"stream,omitempty"`
}

func errorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
func message(m extension.Message, body bool) Message {
	h := m.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	out := Message{Method: m.Method, URL: m.URL, Header: h, Status: m.Status}
	if body {
		s := base64.StdEncoding.EncodeToString(m.Body)
		out.Body = &s
	}
	return out
}
func snapshot(in extension.Input, h worker.HookDescriptor) Input {
	out := Input{V: 1, Stage: h.Stage, Identity: identity(in.Identity), Requested: message(in.Requested, false), Request: message(in.Request, h.BodyMode == "buffered" && stages[h.Stage] < extension.ResponseStart), Response: message(in.Response, h.BodyMode != "none" && stages[h.Stage] >= extension.ResponseStart), Metadata: in.Metadata, Ledger: []Entry{}, Provenance: []Identity{}, Truncated: in.Truncated, Auth: Authentication{in.Auth.Subject, in.Auth.Claims}, Error: errorText(in.Err)}
	for _, e := range in.Ledger {
		out.Ledger = append(out.Ledger, Entry{identity(e.Identity), e.Hook, stageNames[e.Stage], e.Outcome, e.RequestedURL, e.ServedURL, e.Status})
	}
	for _, i := range in.Provenance {
		out.Provenance = append(out.Provenance, identity(i))
	}
	if d := in.Delivery; d != nil {
		out.Delivery = &Delivery{d.Status, d.Bytes, errorText(d.WriteError), errorText(d.FlushError), errorText(d.ReadError), d.Returned, d.Aborted, d.Hijacked}
	}
	return out
}
func decodeBody(s string, limit int) ([]byte, error) {
	if len(s) > base64.StdEncoding.EncodedLen(limit) {
		return nil, extension.ErrLimit
	}
	b, err := base64.StdEncoding.Strict().DecodeString(s)
	if err != nil || base64.StdEncoding.EncodeToString(b) != s {
		return nil, errors.New("body must be canonical base64")
	}
	if len(b) > limit {
		return nil, extension.ErrLimit
	}
	return b, nil
}
func decodeMessage(raw json.RawMessage, body bool, limit int) (*extension.Message, error) {
	var m Message
	if err := worker.DecodeJSON(raw, &m); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, key := range []string{"method", "url", "header", "status"} {
		if len(fields[key]) == 0 || bytes.Equal(fields[key], []byte("null")) {
			return nil, fmt.Errorf("missing message %s", key)
		}
	}
	if m.Header == nil || (body && m.Body == nil) || (!body && m.Body != nil) {
		return nil, errors.New("invalid message body/header declaration")
	}
	if err := headerShape(fields["header"]); err != nil {
		return nil, err
	}
	out := &extension.Message{Method: m.Method, URL: m.URL, Header: m.Header, Status: m.Status}
	if body {
		b, err := decodeBody(*m.Body, limit)
		if err != nil {
			return nil, err
		}
		out.Body = b
	}
	return out, nil
}
func result(raw json.RawMessage, h worker.HookDescriptor, bodyLimit, unitLimit int) (extension.Result, error) {
	var w struct {
		Action      string            `json:"action"`
		Message     json.RawMessage   `json:"message"`
		Request     json.RawMessage   `json:"request"`
		Metadata    map[string]string `json:"metadata"`
		Credentials http.Header       `json:"credentials"`
	}
	if err := worker.DecodeJSON(raw, &w); err != nil {
		return extension.Result{}, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for key, value := range fields {
		if bytes.Equal(value, []byte("null")) {
			return extension.Result{}, fmt.Errorf("null result field %s", key)
		}
	}
	if value, ok := fields["credentials"]; ok {
		if err := headerShape(value); err != nil {
			return extension.Result{}, err
		}
	}
	if value, ok := fields["metadata"]; ok {
		var values map[string]json.RawMessage
		if json.Unmarshal(value, &values) != nil {
			return extension.Result{}, errors.New("invalid metadata")
		}
		for _, value := range values {
			if len(value) == 0 || value[0] != '"' {
				return extension.Result{}, errors.New("metadata values must be strings")
			}
		}
	}
	r := extension.Result{Metadata: w.Metadata, Credentials: w.Credentials}
	switch w.Action {
	case "pass":
		r.Action = extension.Pass
	case "replace":
		r.Action = extension.Replace
	case "respond":
		r.Action = extension.Respond
	case "child":
		r.Action = extension.Child
	default:
		return r, errors.New("explicit supported action required")
	}
	stage := stages[h.Stage]
	if (stage == extension.Observe || stage == extension.OriginalBody || stage == extension.Finish || stage == extension.Delivery) && (r.Action != extension.Pass || w.Metadata != nil || w.Credentials != nil) {
		return r, extension.ErrUnsupported
	}
	if len(w.Message) > 0 && r.Action != extension.Replace && r.Action != extension.Respond {
		return r, errors.New("unexpected result message")
	}
	if len(w.Request) > 0 && r.Action != extension.Child {
		return r, errors.New("unexpected child request")
	}
	if r.Action == extension.Replace || r.Action == extension.Respond {
		limit := bodyLimit
		if h.BodyMode == "stream" && r.Action == extension.Replace {
			limit = unitLimit
		}
		m, err := decodeMessage(w.Message, h.BodyMode != "none" || r.Action == extension.Respond, limit)
		if err != nil {
			return r, err
		}
		if r.Action == extension.Replace && stage < extension.ResponseStart && (m.Status != 0 || !validToken(m.Method)) {
			return r, errors.New("request replacement requires method token and status zero")
		}
		r.Message = m
	}
	if r.Action == extension.Child {
		if stage != extension.ResponseStart {
			return r, extension.ErrUnsupported
		}
		m, err := decodeMessage(w.Request, true, bodyLimit)
		if err != nil {
			return r, err
		}
		req, err := request(*m)
		if err != nil {
			return r, err
		}
		r.Request = req
	}
	return r, nil
}
func request(m extension.Message) (*http.Request, error) {
	if !validToken(m.Method) {
		return nil, errors.New("request method must be a nonempty token")
	}
	r, err := http.NewRequest(m.Method, m.URL, bytes.NewReader(m.Body))
	if err != nil {
		return nil, err
	}
	if r.URL.Host == "" || r.URL.User != nil || (r.URL.Scheme != "http" && r.URL.Scheme != "https") || r.URL.Fragment != "" || m.Status != 0 {
		return nil, errors.New("request must have absolute HTTP(S) URL and status zero")
	}
	r.Header = m.Header.Clone()
	return r, nil
}
