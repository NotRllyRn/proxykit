// Package control implements private local management, never a public proxy route.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/NotRllyRn/proxykit/nodehost"
)

type Request struct {
	Op     string `json:"op"`
	ID     string `json:"id,omitempty"`
	Source string `json:"source,omitempty"`
	Trust  bool   `json:"trust,omitempty"`
}
type Response struct {
	Error   string                  `json:"error,omitempty"`
	Message string                  `json:"message,omitempty"`
	Status  *nodehost.RuntimeStatus `json:"status,omitempty"`
}

func socket(store string) string { return filepath.Join(store, ".control", "runtime.sock") }

// Listen requires exclusive store ownership before stale socket removal.
func Listen(store string, runtime *nodehost.Runtime) (func(context.Context) error, error) {
	dir := filepath.Dir(socket(store))
	if e := os.Mkdir(dir, 0700); e != nil && !errors.Is(e, os.ErrExist) {
		return nil, e
	}
	st, e := os.Lstat(dir)
	if e != nil {
		return nil, e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm() != 0700 {
		return nil, errors.New("control directory must be real and private (0700)")
	}
	if e = checkOwner(st); e != nil {
		return nil, e
	}
	if st, e = os.Lstat(socket(store)); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refuse non-socket control path")
		}
		if e = os.Remove(socket(store)); e != nil {
			return nil, e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	listener, e := net.Listen("unix", socket(store))
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(socket(store), 0600); e != nil {
		listener.Close()
		return nil, e
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := Response{}
		defer func() { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(response) }()
		if r.Method != "POST" || r.URL.Path != "/" {
			response.Error = "only POST / supported"
			return
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 8193))
		if e != nil || len(b) > 8192 {
			response.Error = "management request exceeds bound"
			return
		}
		var request Request
		if e = decodeRequest(b, &request); e != nil {
			response.Error = e.Error()
			return
		}
		// Canonical field vocabulary also rejects irrelevant arguments.
		if request.Op != "install" && (request.Source != "" || request.Trust) {
			response.Error = "unexpected install arguments"
			return
		}
		if request.Op == "status" && request.ID != "" {
			response.Error = "unexpected status argument"
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
		defer cancel()
		switch request.Op {
		case "enable", "disable":
			e = runtime.Change(ctx, request.ID, request.Op == "enable")
		case "remove":
			e = runtime.Remove(request.ID)
		case "data-dir":
			response.Message, e = runtime.DataDir(ctx, request.ID)
		case "install":
			if request.ID != "" {
				e = errors.New("unexpected installation ID")
				break
			}
			var id string
			record, err := runtime.Install(ctx, request.Source, request.Trust)
			e = err
			id = record.Manifest.ID
			if e == nil {
				response.Message = id
			}
		case "status":
			s, err := runtime.Status()
			e = err
			response.Status = &s
		default:
			e = errors.New("unknown management command")
		}
		if e != nil {
			response.Error = e.Error()
		}
	})
	go func() { _ = server.Serve(checkedListener{listener}) }()
	return func(ctx context.Context) error {
		e := server.Shutdown(ctx)
		if e != nil {
			_ = server.Close()
		}
		_ = os.Remove(socket(store))
		return e
	}, nil
}

// Commands are flat objects. Reject duplicate keys (including escaped aliases)
// as well as unknown fields/types and trailing JSON, not last-key-wins decoding.
func decodeRequest(b []byte, r *Request) error {
	d := json.NewDecoder(bytes.NewReader(b))
	token, e := d.Token()
	if e != nil {
		return e
	}
	if token != json.Delim('{') {
		return errors.New("management command must be an object")
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		if e != nil {
			return e
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return errors.New("duplicate management field")
		}
		seen[name] = true
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return e
		}
	}
	if _, e = d.Token(); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(r)
}

type checkedListener struct{ net.Listener }

func (l checkedListener) Accept() (net.Conn, error) {
	for {
		c, e := l.Listener.Accept()
		if e != nil {
			return nil, e
		}
		if peerAllowed(c) {
			return c, nil
		}
		c.Close()
	}
}

func Call(ctx context.Context, store string, request Request) (Response, error) {
	var response Response
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", socket(store))
	}}
	defer transport.CloseIdleConnections()
	b, _ := json.Marshal(request)
	req, e := http.NewRequestWithContext(ctx, "POST", "http://local/", bytes.NewReader(b))
	if e != nil {
		return response, e
	}
	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}
	r, e := client.Do(req)
	if e != nil {
		return response, e
	}
	defer r.Body.Close()
	b, e = io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if e != nil {
		return response, e
	}
	if e = json.Unmarshal(b, &response); e != nil {
		return response, e
	}
	if response.Error != "" {
		return response, fmt.Errorf("management: %s", response.Error)
	}
	return response, nil
}
