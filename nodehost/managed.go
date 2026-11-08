package nodehost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/NotRllyRn/proxykit/extension"
)

// managed implements only buffered ordinary HTTP. No redirects, streaming,
// capability values, alternative origins, or transport options cross the wire.
func (h *Host) managed(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	m, err := decodeMessage(raw, true, h.bodyLimit)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(m.URL)
	if err != nil {
		return nil, err
	}
	origin, _ := url.Parse(h.gateway)
	if u.Scheme != origin.Scheme || u.Host != origin.Host {
		return nil, errors.New("managed HTTP requires configured gateway origin")
	}
	for k := range m.Header {
		if strings.HasPrefix(strings.ToLower(k), "x-proxykit-") {
			return nil, errors.New("reserved managed HTTP header")
		}
	}
	r, err := request(*m)
	if err != nil {
		return nil, err
	}
	r = r.WithContext(ctx)
	client, err := h.Engine.Client(ctx, h.gateway)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, errors.New("managed HTTP redirects unsupported")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, int64(h.bodyLimit)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > h.bodyLimit {
		return nil, extension.ErrLimit
	}
	return json.Marshal(message(extension.Message{Status: resp.StatusCode, Header: resp.Header, Body: b}, true))
}
