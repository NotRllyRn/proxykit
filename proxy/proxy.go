// Package proxy constructs a streaming HTTP reverse proxy using Go's HTTP stack.
package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// New routes requests to upstream, appending the incoming path and query.
// Configure the returned proxy before serving; do not mutate it during requests.
// Its transport owns reusable connections; call CloseIdleConnections when done.
func New(upstream string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(upstream)
	if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.Fragment != "" {
		return nil, fmt.Errorf("upstream must be an absolute HTTP(S) URL without credentials or fragment")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	return &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
		},
		Transport: transport,
	}, nil
}
