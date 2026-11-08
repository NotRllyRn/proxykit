// Package proxy constructs a streaming HTTP reverse proxy using Go's HTTP stack.
package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/NotRllyRn/proxykit/extension"
)

// Proxy forwards opaque HTTP bodies and upgraded connections. Its embedded
// ReverseProxy exposes the standard native composition points.
type Proxy struct {
	*httputil.ReverseProxy
	// SuppressInformationalResponses opts managed traffic out of upstream 1xx
	// forwarding (except 101 upgrades), before ResponseStart selects candidates.
	// Configure before serving. It has no effect on New's unmanaged path.
	// Final responses are unchanged; selected candidates do not replay their 1xx.
	SuppressInformationalResponses bool
	execution                      *extension.Engine
}

// ServeHTTP allows an upstream to respond while the client is still uploading.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.execution != nil {
		var err error
		r, err = p.execution.Bind(r)
		if err != nil {
			http.Error(w, "invalid managed request", http.StatusUnauthorized)
			return
		}
		var complete func(extension.DeliveryEvidence) error
		r, complete = p.execution.TrackDelivery(r)
		writer := &deliveryWriter{ResponseWriter: w, suppressInformational: p.SuppressInformationalResponses}
		w = writer
		if cn, ok := writer.ResponseWriter.(http.CloseNotifier); ok {
			w = &notifyingDeliveryWriter{writer, cn}
		}
		returned := false
		defer func() {
			if err := complete(writer.result(returned)); err != nil {
				if p.ErrorLog != nil {
					p.ErrorLog.Printf("extension delivery observation: %v", err)
				} else {
					log.Printf("extension delivery observation: %v", err)
				}
			}
		}()
		// Keep panic/ErrAbortHandler propagation intact; the deferred evidence
		// distinguishes unwinding from a normal ReverseProxy return.
		_ = http.NewResponseController(w).EnableFullDuplex()
		p.ReverseProxy.ServeHTTP(w, r)
		returned = true
		return
	}
	// HTTP/2 is already full duplex; it returns ErrNotSupported here.
	_ = http.NewResponseController(w).EnableFullDuplex()
	p.ReverseProxy.ServeHTTP(w, r)
}

// NewWithEngine opts into a frozen execution engine. Configure the embedded
// ReverseProxy before serving, just as with New. Shutdown the engine separately;
// it can be shared by multiple proxies. The original transport remains in use.
func NewWithEngine(upstream string, engine *extension.Engine) (*Proxy, error) {
	if engine == nil {
		return nil, fmt.Errorf("nil execution engine")
	}
	p, err := New(upstream)
	if err != nil {
		return nil, err
	}
	p.execution = engine
	p.Transport = engine.Transport(p.Transport)
	return p, nil
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
