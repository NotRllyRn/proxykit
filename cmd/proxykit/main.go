package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/nodehost"
	"github.com/NotRllyRn/proxykit/proxy"
	"github.com/NotRllyRn/proxykit/worker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) (runErr error) {
	if len(args) > 0 && args[0] == "extension" {
		return runExtension(ctx, args[1:], output)
	}
	flags := flag.NewFlagSet("proxykit", flag.ContinueOnError)
	flags.SetOutput(output)
	listen := flags.String("listen", "127.0.0.1:8080", "HTTP listen address")
	certificate := flags.String("tls-cert", "", "optional PEM certificate file (requires -tls-key)")
	privateKey := flags.String("tls-key", "", "optional PEM private key file (requires -tls-cert)")
	upstream := flags.String("upstream", "", "required upstream HTTP(S) URL")
	node := flags.String("node", "node", "trusted extension Node executable")
	gateway := flags.String("gateway-origin", "", "exact gateway HTTP(S) origin for managed extension calls (defaults to bound listener)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if (*certificate == "") != (*privateKey == "") {
		return fmt.Errorf("-tls-cert and -tls-key must be supplied together")
	}
	store, err := extensionStore()
	if err != nil {
		return err
	}
	installed, err := (extensions.Installer{Store: store}).List()
	if err != nil {
		return fmt.Errorf("inspect installed extensions: %w", err)
	}
	handler, err := proxy.New(*upstream)
	if err != nil {
		return err
	}
	// Retain the underlying transport explicitly: the engine wrapper does not
	// own idle-connection cleanup or expose a concrete *http.Transport.
	transport := handler.Transport
	if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
		defer closer.CloseIdleConnections()
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	logger := log.New(output, "proxykit: ", log.LstdFlags)
	if len(installed) > 0 {
		origin := *gateway
		if origin == "" {
			scheme := "http"
			if *certificate != "" {
				scheme = "https"
			}
			origin = scheme + "://" + listener.Addr().String()
		}
		config, err := json.Marshal(map[string]string{"gatewayOrigin": origin})
		if err != nil {
			return err
		}
		host, err := nodehost.Open(ctx, nodehost.Options{Store: store, Gateway: origin, Worker: worker.Options{NodeBinary: *node, Stderr: output, Config: config}})
		if err != nil {
			return fmt.Errorf("activate installed extensions: %w", err)
		}
		defer func() {
			drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			runErr = errors.Join(runErr, host.Close(drain))
		}()
		handler, err = proxy.NewWithEngine(*upstream, host.Engine)
		if err != nil {
			return err
		}
		// NewWithEngine's pristine transport has not opened any connections.
		// Reuse the explicitly owned transport so its resources remain closable.
		handler.Transport = host.Engine.Transport(transport)
	}
	handler.ErrorLog = logger
	// Signal cancellation starts draining; this separate context closes upgraded
	// connections when draining finishes, since Server.Shutdown does not own them.
	connections, closeConnections := context.WithCancel(context.Background())
	defer closeConnections()
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		BaseContext:       func(net.Listener) context.Context { return connections },
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		ErrorLog:          logger,
	}
	logger.Printf("listening on %s", listener.Addr())
	result := make(chan error, 1)
	go func() {
		if *certificate != "" {
			result <- server.ServeTLS(listener, *certificate, *privateKey)
		} else {
			result <- server.Serve(listener)
		}
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		err := <-result
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
