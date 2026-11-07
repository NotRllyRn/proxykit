// Package proxy constructs a streaming HTTP reverse proxy using Go's HTTP stack.
package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Proxy forwards opaque HTTP bodies and upgraded connections. Its embedded
// ReverseProxy exposes the standard native composition points.
type Proxy struct {
	*httputil.ReverseProxy
}

// ServeHTTP allows an upstream to respond while the client is still uploading.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// HTTP/2 is already full duplex; it returns ErrNotSupported here.
	_ = http.NewResponseController(w).EnableFullDuplex()
	p.ReverseProxy.ServeHTTP(w, r)
}

// New routes requests to upstream, appending the incoming path and query.
// Configure the returned proxy before serving; do not mutate it during requests.
// Its transport owns reusable connections; call CloseIdleConnections when done.
func New(upstream string) (*Proxy, error) {
	target, err := url.Parse(upstream)
	if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.Fragment != "" {
		return nil, fmt.Errorf("upstream must be an absolute HTTP(S) URL without credentials or fragment")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	return &Proxy{ReverseProxy: &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			// Trailer values arrive at body EOF. Keep the inbound map rather
			// than the snapshot made by Request.Clone so they reach upstream.
			request.Out.Trailer = request.In.Trailer
		},
		Transport:     transport,
		FlushInterval: -1, // Flush even known-length audio/binary responses incrementally.
	}}, nil
}
