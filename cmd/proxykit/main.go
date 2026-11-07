package main

import (
	"context"
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

	"github.com/NotRllyRn/proxykit/proxy"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("proxykit", flag.ContinueOnError)
	flags.SetOutput(output)
	listen := flags.String("listen", "127.0.0.1:8080", "HTTP listen address")
	certificate := flags.String("tls-cert", "", "optional PEM certificate file (requires -tls-key)")
	privateKey := flags.String("tls-key", "", "optional PEM private key file (requires -tls-cert)")
	upstream := flags.String("upstream", "", "required upstream HTTP(S) URL")
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
	handler, err := proxy.New(*upstream)
	if err != nil {
		return err
	}
	defer handler.Transport.(*http.Transport).CloseIdleConnections()
	logger := log.New(output, "proxykit: ", log.LstdFlags)
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
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
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
