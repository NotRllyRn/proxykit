# ProxyKit

A small, streaming HTTP reverse proxy written in Go, with native Go extensions and trusted Node ESM workers. The Go core has no third-party dependencies. Installed extensions activate on server restart; a dashboard and published npm/npx packaging remain future work.

Go owns transport and extension execution; Node supplies an optional TypeScript/JavaScript authoring boundary. See the [transport decision](docs/TRANSPORT_DECISION.md), [extension SDK](sdk/README.md), and [implementation tracker](docs/IMPLEMENTATION_TRACKER.md). Local installed-worker and combined-application acceptance tests pass; the [R01–R50 audit](docs/implementation/acceptance-audit.md) distinguishes tested boundaries from optional, unsupported or uncertified capabilities.

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

The listener defaults to loopback. Without an authentication extension, there is no client authentication. For direct HTTPS/WSS, add `-tls-cert cert.pem -tls-key key.pem`; otherwise terminate TLS in front of ProxyKit. Public deployment still needs access controls. Upstream HTTPS certificates are verified normally. Upstream URLs cannot contain credentials or fragments. Request Authorization headers are forwarded.

## Install extensions

Requires Node on PATH when activating Node extensions (tested with Node 24). Packages must contain a root `proxykit-extension.json` manifest and prebuilt ESM; installation does not run npm, build commands or package scripts.

```sh
# Replace this placeholder with a repository you have reviewed and trust:
/tmp/proxykit extension install https://github.com/OWNER/REPOSITORY --trust
/tmp/proxykit extension status
# Start with the same store (new packages remain disabled):
/tmp/proxykit -listen 127.0.0.1:8080 -upstream https://example.com
# In another terminal, activate without restarting the listener:
/tmp/proxykit extension enable example-extension
/tmp/proxykit extension status example-extension
/tmp/proxykit extension disable example-extension
# Wait until status shows no active/draining/cleanup owner, then:
/tmp/proxykit extension remove example-extension
```

Set `PROXYKIT_EXTENSIONS_DIR` to select the store; otherwise it lives under your OS user configuration directory at `proxykit/extensions`. Installation records the resolved GitHub commit and defaults to disabled. `install URL --trust --activate` installs and then enables on a running server; offline it installs disabled and returns an online-required error. Offline enable/disable persists intent only. Invalid packages or hook configurations fail activation rather than silently removing security hooks. Unchanged workers retain their ports, store locks and jobs across activation. Replacement requires disable, drain/reap, remove, reinstall and enable—never overlapping stateful versions. See [hot activation, legacy policy and bounds](docs/HOT_ACTIVATION.md).

**Trusted extensions execute arbitrary server-side code with the server's privileges.** `--trust` is an acknowledgment, not a sandbox or security review. Same-UID trusted code can access the local management socket; it is not a boundary against installed code. Review the repository before installing. See the [SDK and authoring guide](sdk/README.md), [example extension](examples/node-personal-usage/), and [installation/runtime contracts](docs/implementation/distribution.md).

## Develop an extension

Start with the **[TypeScript extension developer guide](docs/EXTENSION_AUTHORING.md)**: a complete first extension, GitHub repository layout and manifest, prebuilt packaging, all HTTP hook stages, API recipes, ordering, credentials, lifecycle, testing and troubleshooting. Use the [SDK reference](sdk/README.md) and [typed API](sdk/http.d.mts) for exact contracts; see the [minimal runnable example](examples/node-personal-usage/) for an existing package.

## Connection support

**No-extension transport support, verified with local fixtures—not live-provider certification.** Configure the client's base URL and send the credentials its upstream expects. Without extensions, one instance forwards to one configured upstream and does not translate formats or route among providers. Optional native application examples add narrower routing and AI protocol capabilities.

| Connection / request | Support |
| --- | --- |
| OpenAI/Codex Responses, `/v1/responses/compact`, Chat Completions | Yes: opaque HTTP bodies and SSE; compaction/state fields preserved |
| Claude/Anthropic Messages, Gemini REST, other HTTP APIs | Yes: paths, provider headers and bodies pass through; no adapters |
| SSE, binary/event streams, audio/images/files, multipart uploads | Yes: incremental request/response streaming, including known-length bodies |
| Responses, Realtime/voice, Gemini Live WebSockets | Yes: HTTP/1.1 upgrade, WS/WSS, text/binary, fragmentation, ping/pong, close |
| HTTP/1.1 and HTTP/2, duplex streams and trailers | Yes: HTTP/2 over TLS on both sides; prior-knowledge h2c inbound only |
| gRPC over HTTP/2 | Opaque frames/trailers tested; no service discovery or protocol translation |
| WebRTC voice | HTTP session/SDP signaling only; media/data channels bypass this proxy |
| HTTP/3, WebSocket over HTTP/2 extended CONNECT, CONNECT tunnels, SIP | Not supported |
| OAuth/login, signing, model access, API-format conversion | Client/upstream responsibility; not implemented here |

OpenAI-compatible services (including compatible Grok endpoints) use the same generic transport. Names such as Kiro or Muse do not establish a wire protocol or a tested integration. Clients must allow endpoint configuration; signed requests must be valid for the actual upstream Host/path. Browser CORS policy and absolute redirects/upload URLs remain upstream-owned and are not rewritten. See [coverage and evidence](docs/TRANSPORT_COVERAGE.md).

### Codex with an API-key upstream

Run `go run ./cmd/proxykit -upstream https://api.openai.com`, then configure a Codex provider in `~/.codex/config.toml`:

```toml
model_provider = "proxykit"

[model_providers.proxykit]
name = "ProxyKit"
base_url = "http://127.0.0.1:8080/v1"
wire_api = "responses"
env_key = "OPENAI_API_KEY"
supports_websockets = true
```

Keep your chosen upstream-supported model and supply the key through your environment. This follows the [documented Codex configuration](https://developers.openai.com/codex/config-reference); it is not a tested ChatGPT subscription-login or hosted Codex Cloud integration. A cloud client needs a reachable proxy URL and support for overriding its endpoint.

## No-extension behavior

- Streams request and response bodies, including SSE, with backpressure and cancellation; no whole-body buffering.
- Reuses upstream connections and preserves compressed representations, end-to-end headers, status codes and trailers.
- Rewrites destination and Host; strips hop-by-hop headers and Go's recognized Forwarded/X-Forwarded identity headers. It does not add forwarding identity headers or follow environment proxy settings.
- Returns 502 on upstream connection/TLS errors. Interrupted responses terminate. No application retry policy is added.
- Uses a 10-second request-header timeout and 90-second idle timeout, with no total stream deadline. SIGINT/SIGTERM allows ordinary active requests up to 10 seconds to finish, then cancels remaining connections, including WebSocket tunnels. Shutdown closes tunnels without synthesizing WebSocket close frames.

WebSocket traffic uses standard-library upgrade handling; there is no gateway WebSocket message parser or TLS interception. Optional AI adapters and durable application examples are separate from the transport. There is no built-in dashboard.

## Native composition

`proxy.New(upstream)` returns a `*proxy.Proxy` embedding the standard `*httputil.ReverseProxy`, with HTTP/1 full duplex enabled. Configure its `Rewrite`, `ModifyResponse` or `Transport` before serving to modify requests and responses, including body readers. Transformations must keep length/encoding headers correct and close replaced bodies. Configuration must remain unchanged while requests are running. Close the transport's idle connections when finished. Embedded servers should supply a cancellable `http.Server.BaseContext` and cancel it after HTTP draining to close upgraded connections, as the executable does.

For managed native extensions, use `extension.New` and `proxy.NewWithEngine`. The engine owns ordered hooks, bounded body recovery, scoped internal calls, original observations and delivery evidence. See [execution contracts](docs/implementation/execution.md). `nodehost.Open` adapts installed Node hooks to that same engine; the CLI uses it automatically when extensions are installed. TypeScript authors compile to ESM before packaging.

Optional examples demonstrate [account brokering](examples/brokerapp/), [managed AI coordination](examples/aihost/), [durable recall](examples/recall/) and their [combined composition](examples/system/) with an independent original recorder. Local socket/process tests cover two tenants, routing, tools, restart continuation, buffered SSE and shutdown. They are application building blocks, not enabled-by-default services or universal protocol adapters.

## Verify

```sh
go test -race -count=1 -timeout 180s ./...
go vet ./...
go test ./proxy -run '^$' -bench BenchmarkProxy -benchmem -benchtime=2s -cpu=4
```

Tests cover forwarding and opaque API payloads; SSE and duplex streaming; compression and trailers; TLS, HTTP/2 and h2c; WebSocket frames, disconnects and shutdown; errors and connection reuse; and a compiled-process startup/draining test. Tests use local fixtures, with no provider credentials. The POSIX signal test skips Windows. CI runs vet, race-enabled tests and a build on Linux. Research fixtures are isolated from the application module.

Local baseline: Go 1.27.1, Linux amd64, Intel i9-13900H, four benchmark CPUs, parallel 32 KiB responses over loopback: **221,310 ns/op, 148.06 MB/s, 48,050 B/op, 161 allocations/op**. These are whole-harness measurements including client, proxy and upstream, not isolated proxy latency, production capacity, bounded-memory proof, or a Go-versus-Node comparison. No claim of maximum achievable speed is made; this is a repeatable starting point without speculative optimization.

## Requirement status

See the [simple 50-requirement status table](docs/REQUIREMENT_STATUS.md) for what is implemented, working as an optional example, partially implemented or not implemented, plus the remaining work. No unfinished item is currently labeled as actively in progress.

## Project context

Start with the [implementation tracker](docs/IMPLEMENTATION_TRACKER.md) for current integration status and evidence. The [consolidated extension plan](docs/EXTENSION_PLAN.md) retains the design decisions, recovery/re-entry recommendations and open cases.

The [extension composition note](docs/EXTENSION_COMPOSITION.md) records current priorities, proposed hook stages/ledger, streaming rules, and source findings from Pi and Codex Broker.

[Project direction and architecture](docs/PROJECT_DIRECTION.md) records the goals and current ownership model. The [six architecture decisions](docs/research/2026-11-extension-audit/extension-requirements.md) define proposed extension contracts; the [core readiness review](docs/CORE_READINESS.md) maps all 50 requirements to staged implementation and test gates. The accepted transport decision above describes the implemented milestone.

The [50-requirement extension engineering reference](docs/EXTENSION_ENGINEERING_REFERENCE.md) compares Claude Code Router, CLIProxyAPI, and Bifrost using pinned implementation evidence. It explains supported mechanisms, extension ownership, integration work, host gaps, and minimal proposed ProxyKit contracts.

The project takes inspiration from Codex Broker, Pi's extension authoring experience, and context gateways. It is not a Pi compatibility layer or a claim of universal client support.
