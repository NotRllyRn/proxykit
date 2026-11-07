# Transport coverage and evidence

This core is model- and schema-independent. It forwards to one configured upstream and does not parse AI request bodies, SSE events or WebSocket messages. Compatibility means the transport carries a client's existing protocol; it does not mean one provider understands another provider's schema or credentials.

## Implemented and tested

- HTTP/1.1; HTTP/2 with TLS upstream/downstream; prior-knowledge cleartext HTTP/2 on the executable listener. The default upstream transport uses HTTP/1.1 for cleartext HTTP.
- Arbitrary endpoint paths, query strings, methods and opaque HTTP payloads. Representative fixtures include Responses, standalone compaction, Chat Completions, Messages, Gemini REST, SDP signaling, multipart audio and binary event-stream bytes. These fixtures assert exact bytes and headers, not valid model output.
- Incremental SSE and binary responses, including small chunks with Content-Length; streaming uploads and simultaneous uploads/downloads. The core does not impose a whole-body size limit or parse/normalize JSON. HTTP header limits and protocol validation still apply.
- Request and response trailers; binary gRPC-shaped frames with HTTP/2 full duplex and status trailers. This does not implement gRPC service semantics, gRPC-Web conversion or h2c upstream selection.
- HTTP/1.1 WebSocket upgrades to HTTP or HTTPS upstreams. The `-upstream` URL stays `http://` or `https://`, even when clients connect with `ws://` or `wss://`. Optional listener TLS enables direct WSS. Tests verify handshake metadata, subprotocols, server-initiated traffic, multiple turns, fragmented text, binary audio-sized frames, ping/pong, close frames, rejected upgrades and disconnect propagation.
- TLS certificates are verified upstream. Executable TLS uses operator-provided PEM certificate/key files; tests trust a local fixture certificate without disabling verification.
- Ordinary HTTP requests drain on shutdown for up to ten seconds. Cancellation then closes upgraded sockets on both sides, rather than leaving them outside the server lifecycle. Active WebSocket sessions may be interrupted and must reconnect; no application close handshake or session replay is manufactured.

`proxy/protocol_test.go` and `proxy/websocket_test.go` contain transport fixtures. `cmd/proxykit/transport_test.go` verifies listener protocols and socket cleanup without process exit concealing leaks. `cmd/proxykit/main_test.go` also builds and launches the executable and verifies signal-driven HTTP draining. Existing forwarding, cancellation, compression, TLS validation and connection-reuse tests remain.

Validation: `go test -race -timeout 90s ./...` passed on Go 1.26.0 and Go 1.27.1 (Linux amd64); `go vet ./...` passed on Go 1.27.1.

## Deliberate limits

- No universal claim for every client/provider/version. No live Codex, Claude, Gemini, Grok, Kiro or Muse sessions have been exercised. No provider credentials are required by the test suite. Models and unknown fields are not allowlisted.
- Clients must direct their requests to the proxy. Hardcoded endpoints, subscription authentication, OAuth refresh, AWS signing and provider selection are not supplied by the transport. Host is rewritten to the configured upstream; signatures must match that upstream Host/path and other signed fields.
- End-to-end headers and bodies are preserved subject to HTTP framing rules. Connection-specific headers and recognized forwarding-identity headers are removed as documented. This is not byte-identical HTTP wire replay.
- WebRTC session creation/SDP is HTTP and can be forwarded. Negotiated WebRTC media and data channels are not HTTP streams and do not traverse this proxy. SIP, UDP media relay, HTTP/3, HTTP/2 extended CONNECT WebSockets and HTTP CONNECT forward proxying are outside this implementation.
- CORS is passed through, not granted by the proxy. Absolute Location headers and URLs inside bodies are not rewritten; clients following them may connect directly to another host. The proxy does not rewrite cookies or issue browser credentials.
- WebSocket tunnels are opaque after upgrade. HTTP `ModifyResponse` cannot transform individual frames; future streaming extensions need an explicit framing contract. No reconnect, replay, state recovery or schema conversion is added.
- Embedded callers must configure listener protocols/TLS and cancellation themselves. The executable demonstrates these settings. Configuration must remain immutable while serving.

## Official documentation reviewed

These sources establish API transport shapes, not results of live compatibility testing:

- [OpenAI Responses WebSocket mode](https://developers.openai.com/api/docs/guides/websocket-mode): persistent `/v1/responses`, repeated `response.create` events and continuation IDs.
- [OpenAI compaction](https://developers.openai.com/api/docs/guides/compaction): both server-side compaction and the still-documented standalone `/v1/responses/compact` endpoint. Encrypted items and returned context are opaque; ProxyKit does not prune them.
- [OpenAI Realtime](https://developers.openai.com/api/docs/guides/realtime): WebSocket and WebRTC connections, with current HTTP session endpoints.
- [OpenAI voice WebSockets](https://developers.openai.com/api/docs/guides/voice-websockets) and [WebRTC](https://developers.openai.com/api/docs/guides/voice-webrtc): voice events/audio over WebSocket versus negotiated media/data channels. The fetched guides also describe `/v1/live/sessions`; the core needs no special route for it.
- [Codex configuration](https://developers.openai.com/codex/config-reference): provider base URL, Responses wire API, environment credentials and WebSocket capability settings.
- [Google Gemini Live](https://ai.google.dev/gemini-api/docs/live): stateful WSS transport.
- [AWS InvokeModelWithResponseStream](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_InvokeModelWithResponseStream.html): binary `application/vnd.amazon.eventstream` HTTP responses. Forwarding bytes does not supply AWS request signing.

The Anthropic documentation fetch returned HTTP 403 in this environment. The Messages fixture verifies generic HTTP forwarding only; it is not presented as a fresh audit of the Claude API or Claude Code configuration. “Muse” is not resolved to a specific API contract here.
