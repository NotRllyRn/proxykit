# ProxyKit

A small, streaming HTTP reverse proxy written in Go. The core has no third-party dependencies. Extensions, a dashboard and npm/npx packaging are future work.

Go was selected for its compiled transport, standard-library reverse proxy and standalone deployment. TypeScript on Node is easier for in-process TypeScript extensions, but Go better fits this initial transport-only deliverable. See the [decision matrix and transport contract](docs/TRANSPORT_DECISION.md) for tradeoffs and the future Node boundary. Go does not compile through Rust.

## Run

Requires Go 1.26 or later.

```sh
go run ./cmd/proxykit -upstream https://example.com
# Or build a standalone executable:
go build -o /tmp/proxykit ./cmd/proxykit
/tmp/proxykit -listen 127.0.0.1:8080 -upstream https://example.com
curl -i http://127.0.0.1:8080/
```

Clients use ProxyKit as their base URL. An upstream such as `https://example.com/api?version=1` prefixes incoming paths and appends incoming query parameters. This is a **reverse proxy**, not a CONNECT forward proxy for `HTTP_PROXY`/`HTTPS_PROXY` settings.

The listener defaults to loopback. There is no client authentication or inbound TLS configuration; public deployment needs those controls outside this core. Upstream HTTPS certificates are verified normally. Upstream URLs cannot contain credentials or fragments. Request Authorization headers are forwarded.

## Behavior

- Streams request and response bodies, including SSE, with backpressure and cancellation; no whole-body buffering.
- Reuses upstream connections and preserves compressed representations, end-to-end headers, status codes and trailers.
- Rewrites destination and Host; strips hop-by-hop headers and Go's recognized Forwarded/X-Forwarded identity headers. It does not add forwarding identity headers or follow environment proxy settings.
- Returns 502 on upstream connection/TLS errors. Interrupted responses terminate. No application retry policy is added.
- Uses a 10-second request-header timeout and 90-second idle timeout, with no total stream deadline. SIGINT/SIGTERM allows ordinary active requests up to 10 seconds to finish before forced closure.

Standard-library upgrade handling is retained, but WebSocket compatibility and draining hijacked connections are not tested or guaranteed. There is no TLS interception, protocol-specific AI logic, extension host, storage or dashboard.

## Native composition

`proxy.New(upstream)` returns a standard `*httputil.ReverseProxy`. Configure its `Rewrite`, `ModifyResponse` or `Transport` before serving to modify requests and responses, including body readers. Transformations must keep length/encoding headers correct and close replaced bodies. Configuration must remain unchanged while requests are running. Close the transport's idle connections when finished.

This is a Go composition surface, not an implemented TypeScript extension API. A future Node worker host can provide TypeScript/npm authoring; its IPC, lifecycle and streaming contracts remain to be designed.

## Verify

```sh
go test -race -timeout 90s ./...
go vet ./...
go test ./proxy -run '^$' -bench BenchmarkProxy -benchmem -benchtime=2s -cpu=4
```

Tests cover configuration validation; forwarding, escaping, headers, compression, trailers, HEAD and no-content regressions; socket integration for streaming uploads/downloads, SSE, cancellation, HTTPS, connection reuse, failures and native body transformations; and a compiled-process system test for startup and graceful shutdown. The signal-based system test runs on POSIX systems and skips Windows. CI runs vet, race-enabled tests and a build on Linux. Research fixtures are isolated from the application module.

Local baseline: Go 1.27.1, Linux amd64, Intel i9-13900H, four benchmark CPUs, parallel 32 KiB responses over loopback: **261,582 ns/op, 125.27 MB/s, 47,919 B/op, 160 allocations/op**. These are whole-harness measurements including client, proxy and upstream, not isolated proxy latency, production capacity, bounded-memory proof, or a Go-versus-Node comparison. No claim of maximum achievable speed is made; this is a repeatable starting point without speculative optimization.

## Project context

[Project direction and architecture](docs/PROJECT_DIRECTION.md) records the founding goals and proposed future boundaries; its initial no-implementation status is historical. The accepted transport decision above describes this implemented milestone.

The project takes inspiration from Codex Broker, Pi's extension authoring experience, and context gateways. It is not a Pi compatibility layer or a claim of universal client support.
