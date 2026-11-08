# Claude Code Router: extension audit

Audited 2026-11-07. **Closest of the three to the requested TypeScript extension experience, but not a complete ProxyKit substitute.** The important distinction is between CCR's outer plugin service and its considerably broader packaged gateway.

## Identity and evidence boundary

- Repository: [musistudio/claude-code-router at 057abaa1](https://github.com/musistudio/claude-code-router/tree/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e), package version 3.1.3, MIT.
- Actual runtime dependency: [`@the-next-ai/ai-gateway@1.0.21`](https://www.npmjs.com/package/@the-next-ai/ai-gateway/v/1.0.21), MIT package metadata. [Exact tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).
- Its distributed `dist/index.js.map` includes 100 source modules. References prefixed **G/** below refer to these artifact-derived sources, not a separately verified source repository. [Manifest and SHA-256 hashes](ccr-manifest.json) identify the bundle, map, SDK, and recovered modules.
- [REA static evidence summary](ccr-rea-summary.json), evidence `ev_7fef79a03e4c39c4184a10094b08488bb0ebb63d4425a33a5d0ff0b9bcdd4bbe`. Static graph coverage is incomplete; dynamic paths and bounded alias/getter expansion remain unknown. Runtime conclusions below come from the separate mock probes, not from that graph.
- Read the relevant plugin contracts, loaders, request/route/auth/response paths, virtual tool loops, accounting, context archive, storage and lifecycle code. This is not a line-by-line review of all 863 tracked repository files, every adapter, or every dependency.

## Public extension surfaces

**Outer CCR plugin service.** [`packages/core/src/plugins/service.ts:155`](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L155) exposes configuration, logger, plugin data directory, `openSqliteStore`, `registerGatewayRoute`, `registerGatewayRequestTransform`, `registerHttpBackend`, `registerProxyRoute`, `registerProviderAccountConnector`, `registerApp`, and registration bridges to core provider plugins, gateway plugins and virtual model profiles.

Its request transform can change body, headers and selected model. **Exceptions are logged and skipped** (`service.ts:338–365`). This is not an appropriate seam for a mandatory deny policy. A provider account connector supplies quota/balance information; it is not itself the account selection/auth replacement hook. Core plugin registration is needed for that role.

**Packaged core.** `G/plugins/loader.ts:44–69`, `G/types.ts:1403–1610`, and the package's `docs/plugins.md` expose:

| Role | Actual public API | Practical capability |
|---|---|---|
| Inbound policy | `requestHooks.beforeAuth/beforeRouting/beforePrecheck/afterPrecheck` | Reject with explicit status; inspect request and routing context |
| Context/instructions/skills | `requestTransforms`, stages `beforeRouting` and `beforeUpstream` | Change raw body, standard request, headers, metadata, model |
| Broker selection | `routeResolvers.resolve` | Return a full dynamic provider config or ordered fallback providers |
| Broker authentication | `ProviderPlugin.authenticate`, `transformRequest` | Replace upstream URL, headers and body; refresh/select credentials in plugin code |
| Protocols | `sourceAdapters`, `targetAdapters` | Add inbound and outbound protocol implementations |
| Responses | `ProviderPlugin.transformResponse`, `responseHooks.transformResponse` | Upstream payload transforms and final ordinary nonstream response transforms |
| Streams | `streamHooks.transformResponse` | Wrap/replace a `Response` before ordinary relay; **not universal across virtual loops** |
| HTTP APIs | `httpRoutes` | Arbitrary method/path, pre/fallback priority, `auth: gateway | none` |
| Tool profiles | `virtualModelProfiles` | Internal/client ownership, MCP tools, buffered/optimistic loop, turn/call limits |
| Telemetry | `billingEventHooks`, `eventHooks`, publishers and outboxes | Event transformation, durable outbox adapters, external delivery |
| Delivery reliability | `deliveryStateStores` plus publisher/outbox lifecycle | Dedupe/dead letters, retry/concurrency policies, health/drain/close |

The wrapper runs the core in a child process and supplies managed configuration through a virtual file. Its bootstrap rejects writes to that managed configuration. A core plugin should keep its own live state or cooperate with CCR configuration management; it cannot assume the packaged core's standalone config-writing routes work unchanged through CCR. See [`gateway-bootstrap.ts`](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/core-runtime/gateway-bootstrap.ts).

## Broker as an extension

**Feasible in substantial part.** A plugin can own account pools, quota policy, renewal jobs and an encrypted credential store, select dynamic providers, and replace upstream credentials. The mock probe verified selection plus replacement of the original synthetic key with the chosen synthetic account key. CCR's own [`local-agent-auth-provider-hook.ts`](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/core-runtime/local-agent-auth-provider-hook.ts) is another concrete credential-hook example.

Public enrollment is **new plugin work**, not a ready-made host product: browser cookie handling, device-login start/poll, identity verification, duplicate detection, label binding, transactional activation, encrypted credential checkpoints, notifications and cleanup. `httpRoutes`/`registerGatewayRoute` can mount the APIs, and `registerHttpBackend` can own another service. Only `gateway` versus `none` route auth is directly represented: scoped enrollment-service auth, separate public/private listeners, per-client account allow/block rules and transactions spanning enrollment/outbox state must be designed by the plugin or supporting service. The probe's `/audit/enroll` proves route registration only; it does **not** implement or test OAuth/public enrollment.

No capability-scoped secret boundary is established: plugin factories receive configuration and trusted JavaScript can use process/filesystem/network APIs. Dynamic provider selection does not automatically attribute every Broker account lifecycle event or guarantee refresh singleflight.

## Recall, compaction and internal tools

**There is already a real host tool loop.** It is not merely a text transformer. `G/types.ts:850–930`, `G/gateway/handler.ts:2590–3335,4490–4715` implement virtual model profiles with internal/client tool ownership, MCP execution and repeated upstream calls. The mock verified both nonstream and optimistic SSE internal continuations: two upstream calls, one internal tool execution, valid final answer, and aggregate usage of 36 tokens from two 18-token responses.

However:

1. **Private transcript is not reconciled on client replay.** `foldInternalResults` defaults off; its own type comment explains the missing next-turn history (`G/types.ts:884–899`). Our next request used the same synthetic session ID and replayed the client's transcript; the hidden archive result was absent upstream. Folding makes results visible as assistant text, not a durable private continuation ledger.
2. **Mixed calls have a tradeoff, not complete private continuation support.** With default folding off, a mixed internal/client turn returns the client call, strips the internal call, and does not execute it. With folding on, the internal call runs and its result is exposed as assistant text before returning the client call. Both behaviors were reproduced over SSE. It does correctly stop model continuation while awaiting client results.
3. **Important hook coverage differs by path.** Ordinary final response hooks and stream hooks ran in our baseline probes. The virtual nonstream result did not receive the final response hook, and the optimistic virtual stream did not invoke the general stream hook. Source call sites corroborate this (`G/gateway/handler.ts:1035,1075,1402,2057,3197,4490`). A policy/telemetry plugin cannot assume these hooks see all final output. Provider payload hooks are a separate seam; they do not make the final stream hook universal.
4. **Tool execution lacks authenticated conversation context in this path.** `executeInternalVirtualToolCalls` constructs `sessionId: virtual-model:<profile>` with empty messages and does not pass the request abort signal (`G/gateway/handler.ts:3578–3650`). The MCP provider forwards args and optional multimodal metadata, not that synthetic session object (`G/agent/tools.ts:150–164`). A multitenant recall tool needs a trusted tenant/session capability supplied separately, not an LLM-selected archive ID alone. Tool timeout exists, but end-to-end cancellation of these virtual MCP executions is not established.
5. **Tool registration is MCP/profile-oriented.** The public core module result does not expose a generic `tools` implementation array or a durable conversation reconciliation service. Plugin-owned MCP servers can provide tools; the bridge and identity policy remain work.

There is also a separate `transparentToolExecution` path. Its gate explicitly requires **nonstreaming** (`G/gateway/handler.ts:4910–4914`); it is not an alternative streaming fix. Separate `/agent/...` session APIs exist, but an unchanged external coding client's proxy request does not automatically enter that agent session engine.

**Built-in context archive is useful but distinct.** [`context-archive.ts`](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive.ts) and its [`store.ts`](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive/store.ts) persist snapshots in SQLite, hash archive tokens, track lineage/status, intercept explicit/client-specific compaction signals and expose `/__ccr/context-archive/mcp`. Archive replay rejects external client-tool execution. These are built-in application paths, not evidence of a generic extension-owned revision/CAS reconciliation contract. Selected upstream archive tests passed; no live Codex/Claude compatibility claim is made.

## Telemetry, composition and failure

- Ordinary billing captured 18 real upstream tokens even when a final response plugin replaced visible usage with 1,998. Internal loops recorded aggregate 36. **This tested accounting path is sound for these cases.**
- Billing event hooks can mutate or drop events before publishers/outboxes (`G/billing/publisher.ts:198–260,453+`). Therefore the API does not guarantee an immutable, independently protected raw-usage ledger. Unknown usage, failed retries, and all protocol-specific accounting need separate verification.
- Core execution policies include `timeoutMs`, `concurrency`, `maxQueueSize`, failure threshold/cooldown and `fail_closed | fail_open` (`G/types.ts:1300–1309`, `G/plugins/execution.ts`). Default closed and explicit open behavior were tested. Limits are configurable/optional; a timeout does not forcibly terminate arbitrary JavaScript or roll back external effects.
- Registration order and named stages exist. No dependency-ordered transform DAG, exclusive compactor arbitration, or universal stable per-request plugin snapshot was established in the inspected loader/hooks. Package installation dependencies are a different concern.
- Outer plugin startup rolls back failed registrations and continues without that plugin. Stop hooks run in reverse order and backend/SQLite resources are closed (`service.ts:259–317`). Mandatory policy availability needs additional operator-level enforcement.
- Wrapper permission identifiers are `trusted-code`, `apps`, `gateway-routes`, `proxy-routes`, `http-backends`, `provider-account-connectors`, `gateway-request-transforms`, `core-gateway-config`, `core-gateway-plugins`, `core-provider-plugins`, `virtual-model-profiles`, `sqlite-store`, `system-launcher` ([contract](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/contracts/app.ts#L880)). These gate registration APIs, not OS access by trusted code.

## Reproduction and results

Run in an isolated checkout at the pinned revision:

```sh
npm ci --ignore-scripts --no-audit --no-fund
npm rebuild better-sqlite3
node build/test.mjs core
CCR_INTERNAL_HOME_DIR=$(mktemp -d) node --test \
  .test-dist/core/test/integration/plugins/*.test.js \
  .test-dist/core/test/unit/agents/plugin-*.test.js \
  .test-dist/core/test/unit/agents/provider-plugin-runtime-identity.test.js \
  .test-dist/core/test/unit/agents/context-archive.test.js \
  .test-dist/core/test/unit/gateway/local-agent-auth-provider-hook.test.js
node /path/to/this/report/ccr-probe.mjs "$PWD/node_modules/@the-next-ai/ai-gateway"
```

- [Selected upstream tests](ccr-upstream-tests.log): **51 passed, 1 failed, 0 skipped**. Failure: known bundled plugins without persisted permissions should receive scoped defaults; expected `claude-design` app, got an empty list. Root cause was not isolated; do not interpret as a general plugin failure. An earlier run had 9 skips because the isolated-home environment variable was absent; the final run enables them.
- [Our 12 local probes](ccr-probe-results.json): **12 passed**, including probes confirming limitations rather than desired behavior. [Harness](ccr-probe.mjs) starts the actual shipped bundle with temporary configuration, synthetic credentials, mock upstream and mock MCP; it cleans up its processes/data.
- No real account enrollment, provider OAuth, real coding client, distributed store, restart replay, disconnect-under-tool-load, backpressure stress or performance benchmark was tested. Core probes run standalone; wrapper registration/compilation is source-inspected and covered by the selected upstream tests, not a full desktop-to-provider test.

**Recommendation:** evaluate this underlying gateway before building common routing/hook infrastructure again. Reusing it still requires explicit fixes/contracts for tool identity, hook coverage, private replay, cancellation, policy availability and composition. Replacing the entire request handler inside a plugin would technically be possible, but would mean rebuilding the core responsibilities the user wants to reuse.
