# Transport decision: Go

Status: accepted for the initial HTTP reverse proxy. Extension runtime and API remain proposed.

## Decision matrix

| Criterion | Go | TypeScript on Node.js |
| --- | --- | --- |
| Transport efficiency | Native compilation, lightweight concurrent goroutines; good fit for concurrent streams | Efficient asynchronous I/O; CPU-heavy transformations share an event loop unless offloaded |
| Small, correct implementation | Standard library reverse proxy handles connection reuse, streaming, cancellation, trailers and hop-by-hop headers | HTTP primitives are built in; equivalent proxy policy needs more application code or a dependency |
| Maintenance | Static types, formatter, race detector and test tooling included | Familiar extension ecosystem and strong types; additional build/runtime tooling |
| TypeScript extensions | Needs a separate Node runtime and an explicit communication contract | Direct in-process calls and natural npm integration |
| Distribution | Standalone executable per operating system/architecture | Node required; straightforward npm packaging |
| Future npx launcher | npm package can select/download a native binary; packaging work required | Natural fit, though Node version support still matters |
| Main cost | Two runtimes if TypeScript extensions are added; IPC can dominate fine-grained hooks | CPU-heavy hooks can delay unrelated requests; workers add coordination costs |

Choose **Go** because the current deliverable is a small, efficient transport core. Go compiles to machine code; it is not transpiled into Rust. Rust also offers strong performance and memory safety, but its additional implementation complexity is unnecessary for this standard HTTP forwarding layer. No comparative benchmark here establishes that Go is universally faster than Node or Rust.

## Contract and boundaries

- One configured HTTP(S) upstream, with an optional base path and query. This is a reverse proxy, not an HTTP_PROXY/CONNECT forward proxy or a TLS interception service.
- Rewrite the destination and Host for that upstream. Preserve methods, escaped paths, valid query strings, end-to-end headers, status, bodies and trailers. Protocol framing and hop-by-hop headers are necessarily handled by the HTTP stack; this is not byte-identical wire replay.
- Drop client-supplied Forwarded and X-Forwarded-* identity headers recognized by Go's ReverseProxy. Do not add client identity headers. Do not use environment proxy variables for upstream routing.
- Stream with backpressure, propagate cancellation, reuse upstream connections, and disable automatic decompression so encoded bodies stay encoded. No whole-body buffering or application retries. Go's transport may retry eligible requests on stale reused connections.
- Return 502 for upstream transport errors. Never retry after response commitment. An interrupted upstream response is terminated rather than replaced with a successful-looking error body.
- The executable listens on loopback by default, uses a header-read timeout and idle timeout, and drains ordinary HTTP requests on termination with a bounded grace period. No whole-response timeout, so long-lived streams can continue. This is not a public authenticated gateway.
- Standard library upgrade handling is retained, but WebSocket compatibility and graceful shutdown of hijacked connections are not promised in this milestone. CONNECT tunneling is not implemented.

## Modularity without an extension framework

The proxy constructor returns Go's `httputil.ReverseProxy`. Before serving traffic, Go callers can compose its `Rewrite`, set `ModifyResponse`, or supply a `RoundTripper`. These allow replacing destinations, methods, headers and request/response bodies, including streaming readers. Body transformations must repair Content-Length/encoding metadata, close replaced bodies, and obey cancellation and commitment rules. Mutating configuration while serving is unsupported.

This is a native composition boundary, **not** a TypeScript extension API. Later, design a trusted Node worker boundary around concrete extension requirements. Avoid sending every token through IPC by default; opt into the transformations a route needs. Arbitrary full-response rewrites require buffering and cannot retract bytes already sent. Neither the language nor hooks remove that constraint.

## Implementation and acceptance plan

1. Use the standard library proxy rather than writing an HTTP parser, scheduler or buffer framework.
2. Separate reusable transport construction from executable configuration and lifecycle.
3. Test observable forwarding, streaming before completion, cancellation, connection reuse, compression, trailers, errors and native transformations over local sockets.
4. Run a compiled-process system test for CLI startup, forwarding and graceful termination; run race detection, vet and a repeatable benchmark.
5. Document measured results and limits; defer extensions, dashboard, npm packaging, storage and account logic.
