# CLIProxyAPI extension audit — 2026-11-07

**Target:** `router-for-me/CLIProxyAPI`, exact commit `91e0dee827abf476f9f86246f68c500a112f7155`, checkout `/tmp/proxy-research-0`. Public source links below pin that commit. Linux amd64, Go 1.27.1, native ABI 1 / JSON schema 10. This is a source and local execution audit, not a live-provider compatibility claim. The repository's license is MIT [S00].

**Conclusion:** CLIProxyAPI offers substantial real extension seams for a broker: exclusive inbound authentication, credential acquisition/refresh, account selection, custom executors, routing, usage, and management endpoints. It is a credible base for a **plugin-owned broker**, but does not supply all of Codex Broker's public-enrollment isolation, account/label/ACL transactions, or notification outbox. A recorder can independently receive real usage for ordinary requests, failures/failover, and nested host model calls on the tested route. General durable context/replay and mixed gateway/client tool orchestration remain plugin-owned systems, not host guarantees. Required-policy failure behavior, pristine payload observation, bounded extension work, unknown usage, and exact cross-event correlation need particular attention.

## Evidence and scope

Read ProxyKit's `docs/PROJECT_DIRECTION.md` and both repository guidance files. Applied the REA broader-investigation workflow with ordinary source and runtime tools; no REA binary-analysis session was needed for this complete Go source tree. No further delegation, PR, production dependency edits, or ProxyKit implementation. Only this project-prefixed report, logs, and fixtures are written in the audit directory; one additional test file exists in the disposable CLIProxyAPI checkout.

The inherited test session ID was unavailable in this execution context. Its process was observed running; no duplicate baseline run was started. Its log later showed all four requested packages passing.

Evidence levels used here:

- **Runtime:** built the actual `cmd/server`, loaded a C shared library through the native plugin loader, sent HTTP through the real server to a Python mock on loopback, retained client responses, upstream captures, plugin RPC, and server log.
- **Contract test:** executed upstream tests and two new temporary regression probes. Tests of adapters/fake providers establish those contracts, not real OAuth or provider behavior.
- **Source:** traced implementation and exported contracts; absence claims name their search boundary.
- **Not established:** no test or sufficiently strong contract proves the requested behavior. This does not mean trusted native plugin code cannot implement it.

All prompts, keys, labels and records in fixtures are synthetic. No live accounts or live inference/auth providers were used. The executable ran with `--local-model` and control-panel downloads disabled; this is not a claim that every unrelated background metadata updater in the server was network-sandboxed. Dependency downloads occurred in the temporary checkout/tool cache. The server identified its build as `dev, Commit: none`; the checked-out commit and SHA-256 of the produced binary are recorded separately in `cliproxyapi-manifest.json`.

## Executed tests

| Evidence | Command / result | What it establishes |
| --- | --- | --- |
| Baseline | `go test ./sdk/pluginapi ./sdk/translator ./internal/pluginhost ./sdk/cliproxy/auth -count=1` — all PASS | Existing contract suite, including native loader tests, host callbacks, auth adapters, and scheduling. Log: `cliproxyapi-baseline-tests.log`. |
| Binary | `go build -o /tmp/cpa-audit/cli-proxy-api ./cmd/server` — PASS | Actual server compiles at pinned source; no production source changes. `cliproxyapi-build.log`. |
| Focused + new probes | `go test ./internal/pluginhost -run '^TestAudit\|^TestHostPickAuth\|^TestCompleteRequest\|^TestHostAuth\|^TestHostModelExecute\|^TestHostHTTP.*Cancel' -count=1 -v` — PASS | Error vs termination, cancellation, concurrent calls, account callbacks, nested execution/callback scoping, scheduler failure behavior. `cliproxyapi-contract-tests.log`. |
| Execution/scheduling | `go test ./sdk/api/handlers ./internal/pluginhost ./sdk/cliproxy/auth -run 'TestExecuteModel\|TestIssue6332\|TestManagerPluginScheduler\|TestHostPickAuth\|Test.*Stream.*Drop' -count=1 -v` — all PASS | Host model execution, exact auth pinning, caller scope, cancellation, nested Responses delivery mock, chunk dropping, selection/rejection/fallback. `cliproxyapi-runtime-contract-tests.log`. |
| Management OAuth | `go test ./internal/api/handlers/management -run 'TestPluginLoginPollAuths\|TestSavePluginLoginRecordsRollsBackSavedAuthsOnFailure\|TestServePluginAuthURLPassesQueryParamsAsMetadata\|TestV8PluginOAuthUsesIndependentCallback\|TestV8PluginMultiPortalOAuthListLoginPollAndRelogin' -count=1 -v` — PASS | Mock plugin OAuth poll/save, compensating rollback, query metadata, v8 callback and multi-portal flows. `cliproxyapi-enrollment-contract-tests.log`. |
| Loaded-plugin HTTP | `gcc -shared -fPIC -pthread ... audit_plugin.c`; `python3 .../runtime_probe.py` — **17 assertions PASS** | Real C ABI registration, real HTTP handlers, mock upstream, two synthetic credentials. `cliproxyapi-runtime-results.jsonl`, `cliproxyapi-observations.json`, `cliproxyapi-plugin-rpc.log`, `cliproxyapi-server.log`. |

An initial fixture used the Go-facing stream capability name instead of the native JSON name and attempted to serialize management handler interfaces. Host diagnostics identified both mistakes. The saved C fixture uses `response_stream_interceptor`, `request_lifecycle_plugin`, and route descriptors without `Handler`; the saved runtime evidence is the corrected run. This distinction matters: `sdk/pluginapi` is the host Go representation; native authors must also follow `sdk/pluginabi` and `internal/pluginhost/rpc_schema.go` [S01–S03].

The 17 runtime checks include nine model-request cases, five route/auth cases, usage assertions, a stream-terminal assertion and a presentation-rewrite assertion:

| Case | Client and upstream observation |
| --- | --- |
| Ordinary request, no native API key | 200, one upstream request; exclusive plugin supplies principal `Example user`. |
| Frontend explicit deny | 403, no upstream request. |
| Request hook returns error envelope | **200, one upstream request**. Policy error is logged and ignored. |
| Request hook returns successful `Terminate` | 403 with `blocked`, no upstream request. |
| Response hook rewrites usage | Client sees 999 input + 999 output = 1,998; usage callback retains 7 + 3 = 10. |
| Drop all stream payload chunks | Client receives **only `data: [DONE]\n\n`**, 200. Usage still 10. |
| Before-auth hook calls `host.model.execute` | Two upstream calls; two usage records with distinct usage RequestIDs and the same TraceID. Originating interceptor is skipped for the nested call. |
| Upstream omits usage | Successful client response; usage callback reports all-zero counters with no presence bit. |
| First attempt returns 429 | Second credential succeeds; two upstream calls; separate failure (429) and success usage records, different AuthIDs. |
| Plugin management POST | 401 without management key, 200 with management key. |
| Plugin resource GET | 200 without management key. |
| Same resource path POST | 404. |
| Arbitrary Broker-style private POST path | 404. |

## 1. Broker as plugin

### Inbound authentication: supported public seam, with explicit composition semantics

`FrontendAuthProvider.Authenticate` receives method/path, headers, query, raw body and, on supported encoded routes, decoded body. It returns principal/metadata or an explicit denial. `FrontendAuthProviderExclusive` selects the highest-priority exclusive provider (plugin-ID tie break) and replaces the ordinary provider set [S01, S04]. The native HTTP probe proves acceptance without the configured native API key and explicit denial before upstream traffic.

A plugin can own client keys and labels/ACL lookup. It must actually request exclusivity if other providers must not bypass its checks. Auth adapter errors and recovered panics return `NotHandled`; in an ordinary multi-provider chain that allows another provider to authenticate. With the exclusive provider active, the auth manager exhausts that one provider and returns no credentials rather than accepting anonymously. Plugin disabling/unregistration may restore built-in configuration; there is no independently declared mandatory-auth-plugin deployment requirement established here. Management authentication is a separate host layer and is not replaced by frontend auth [S04, S05, S11].

### Upstream selection: supported, within host eligibility and retry rules

`Scheduler` can choose an AuthID, delegate to round-robin/fill-first, or explicitly reject; `ModelRouter` chooses a built-in provider, another plugin executor, or itself [S01, S06, S07]. A scheduler sees metadata/attributes and request headers, enabling a broker to apply label policy or its own quota state. It cannot select a credential the host did not offer.

The host filters unavailable/model-ineligible/disabled/cooling credentials and normally offers only the highest available priority tier. `SchedulerAcrossPriorities` expands tiers but does not bypass eligibility. Host tests cover across-priority selection, cooldown filters, fallback, exact pinning, and selection IDs. Home mode changes this behavior; tests explicitly cover scheduler bypass in Home mode [S08].

Only the highest-priority scheduler is called for a selection. If it says unhandled, the host uses its built-in scheduler; it does not try the next plugin scheduler on that call. Invalid/unknown candidate output also falls back. An explicit handled rejection is terminal; a returned scheduler error stops the pick. A recovered scheduler panic fuses the plugin and falls back. These distinctions are implemented and tested, not just documentation [S06, S08].

The host still owns retries/failover. The HTTP probe demonstrates a failed 429 attempt followed by a different account, with separate usage. A broker must reapply any required account policy on every selection, and cannot equate “plugin panic” with a guaranteed fail-closed request. Exact `AuthID` pinning is available in `host.model.execute`; upstream tests verify it does not silently fall back across priorities [S09].

### Credential acquisition and refresh: supported; no live OAuth claim

`AuthProvider` exposes parse, login start, poll and refresh; schema 10 supports multiple auth providers/portals. `AuthData` carries label, disabled state, storage JSON, attributes, metadata and `NextRefreshAfter`. Host auth-provider adapters and native-compatible refresh wrappers connect these to managed credentials [S01, S10]. Existing tests run in the baseline cover passing host HTTP/context/state into start/poll, refresh metadata/priority/disabled-state preservation, and compatibility executors.

This allows a plugin to implement device-code/OAuth details and token renewal while the host invokes it. It does **not** prove Broker's exact Codex credential/checkpoint algorithm has been ported, nor validate real token refresh, identity verification or upstream service policy. Such a plugin remains work to implement/test.

### Account CRUD, labels and ACLs: partial host primitives, plugin-owned domain

Public callbacks include `host.auth.list/get/get_runtime/save/models`. `save` validates a `.json` name, parses auth data, writes a physical file, and updates/registers runtime auths; the executed host tests exercise writes, preserving disabled state, and identity handling [S02, S12]. This is more than an inert file export: saved records are integrated with runtime routing.

There is **no `host.auth.delete` callback** in the enumerated ABI. Native management auth-file delete/update/upload routes are built-in functionality, not equivalent to a complete plugin-owned account service. A trusted plugin can call management HTTP with suitable credentials or own files/state, but that introduces its own integration and privileges. Labels in `AuthData` are a display label; metadata can carry custom data. Neither is a relational label catalog, client-key ACL engine, referential deletion rule, nor transaction coupling those rules to account activation.

`saveAuthFile` uses `os.WriteFile` followed by runtime upsert [S12]. No shared plugin transaction/outbox boundary is offered. The host's management OAuth helper has serialized multi-record saving plus compensating rollback [S13]; that is useful behavior, but it is not crash-atomic commitment with a plugin label database and notification outbox. Avoid describing it as Broker's atomic enrollment contract.

### Public enrollment start/poll: OAuth substrate exists; required boundary does not

Broker reference inspected locally at `60c40a26d1ac9c94ddc99a00865880da12aa4309`: `docs/public-enrollment.md` and `internal/httpserver/server.go` (private route registrations at lines 155–157). Its intended boundary is an isolated public process, narrow service key, secure HttpOnly enrollment cookie, availability/start/status private endpoints, label ACL rules, and activation plus notification outbox. No private transcript or credential material is copied here.

CLIProxyAPI can invoke a plugin's login start/poll through management OAuth handling, save returned auths, and expose management routes. Management endpoints remain protected by the host's management key. Plugin paths are normalized beneath `/v0/management`; no path parameters/wildcards and no override of reserved host routes. Public resources are GET-only under `/v0/resource/plugins/<pluginID>/...`, disabled in Home mode. These paths are directly verified over HTTP [S11, S14].

Consequences:

- A management page/start/poll plugin is feasible, with plugin-owned cookie/flow logic if needed.
- A GET resource is not a general public POST router or an isolated listener. Do not grant a browser the host management key to emulate enrollment.
- Broker's separate public process can remain separate; a native plugin could also open its own listener as unrestricted code. Those are plugin/service-owned network infrastructure, not a host route-registration guarantee.
- Preserving narrow private-route authorization, host-mounted public POST routes, and a host-managed isolated enrollment listener requires new host integration if those must be provided by the host.
- Activation/labels/outbox must be implemented in a plugin-owned authoritative transaction with explicit reconciliation, or require stronger host transaction support. No restart/crash proof was performed for this port.

### Telemetry and quota: supported seams, with accounting limits below

`QuotaProvider` exposes describe/fetch/reset for management UI. `UsagePlugin`, lifecycle, request/response hooks and WebSocket observation are real separate capabilities. A broker can consume them independently of the request rewriter. Quota snapshots are not request token counts [S01].

## 2. Independent request/response/attempt recorder

**Supported on tested paths:** A plugin can subscribe only to usage, and optionally request/response/lifecycle interfaces. The native audit plugin received nine usage records: seven with total 10, one successful missing-usage record with total 0, and one failed 429 record with total 0. This includes the hidden host-model call and the failed attempt. Usage records include AuthID/index/type, model, base URL, provider/executor, failure status/body, selected response headers, latency, TTFT, session fields, TraceID and optional scheduler SelectionID [S15].

**Raw usage survives presentation rewriting in this tested executor.** `OpenAICompatExecutor` parses usage from its retained upstream body, not the downstream `ResponseInterceptor` result. Streaming usage is parsed before translated chunks are handed downstream. Runtime tests verify this even when every stream payload is suppressed [S16]. This does not prove all providers/translators preserve raw usage under every before-translation mutation, nor that a custom executor telling the host fabricated usage is independently audited.

**No universal pristine transcript observer:** Request/response interceptors are an ordered mutation chain. Later observers see earlier plugins' edited bodies. The temporary regression test shows that a compactor's body edit survives a later policy error and is what the next observer receives. Response interception is for successful responses; failure usage contains failure text but is not a complete wire transcript for all protocols. WebSocket response observation is a distinct raw event capability, not interchangeable with HTTP stream rewriting. A highest-priority recorder can capture an earlier view of these stages; no independent guaranteed pre-all-mutations/post-all-delivery record was established [S17].

**Correlation trap, observed:** `RequestInterceptRequest.RequestID`/`RequestCompletion.RequestID` and `UsageRecord.RequestID` are separately generated UUIDs. Even the ordinary one-attempt request has different values. `TraceID` groups them, but nested executions share that TraceID; the usage contract exposes no universal parent-execution/attempt linkage to the interceptor ID [S30]. The mock recorded distinct usage RequestIDs on failover and nested calls, while the outer lifecycle has its own ID. SelectionID can join scheduler picks to usage when a plugin scheduler is used; it was empty in the runtime probe using the built-in selector [S18]. Do not join the two RequestID columns as if they were the same key.

**Missing usage is not encoded as unknown:** `UsageDetail` has integer counters with no presence/known flag. The missing-usage runtime response resulted in zero counters and `Failed:false`. A downstream recorder would need payload evidence or another convention to distinguish unknown from genuine zero; the public normalized usage record alone cannot [S15].

**Delivery is not durable or uniformly bounded:** The global usage manager appends to an in-memory slice; `NewManager(buffer int)` does not enforce that parameter. One dispatcher invokes plugins sequentially. A slow sink can stall other sinks while producers continue accumulating. Plugin replacement has a separate bounded pending queue (default 4,096) and backpressure, which must not be confused with a bound on the global queue [S29]. Lifecycle events launch goroutines using `context.WithoutCancel` and log callback errors; no persistent delivery/retry acknowledgment contract exists [S19, S17]. A recorder must persist its own records and define crash/loss handling. Whole-process durable exactly-once auditing is not established.

**Nested accounting boundary:** Calls through `host.model.execute` use normal executor accounting, as observed. Generic `host.http.do` records request-log data when configured but contains no model usage publication/parsing. Direct plugin network requests are outside automatic token accounting. A custom tool loop using host HTTP or its own socket cannot assume every internal provider request appears in `UsagePlugin` [S20].

## 3. Durable archive, compaction and replay after restart

**Possible as plugin-owned implementation; not supplied or proven by the host.** Request hooks can read/replace payloads, perform external I/O, and synchronously invoke host model execution. Native plugins can use files/SQLite/other databases. CallerScope and metadata can be carried into nested executions. These are usable building blocks [S01, S09].

The enumerated `Capabilities` and ABI host callbacks contain no general scoped durable store, compare-and-swap context revision, archive-before-remove transaction, transcript/session repository, or recovery job API. The built-in bbolt configuration is not itself an exported plugin storage contract. The runtime probe saw `derived:ctx:v1:...` session identifiers for ordinary synthetic requests; this audit does not establish those derived identifiers as authoritative conversation identity. CallerScope identifies a caller, not a unique conversation.

To implement the requested archive, a plugin would have to own identity binding, archive writes, mappings/revisions, concurrent updates, replay reconciliation, signed/opaque content handling, provider-held state (`previous_response_id`), retention/redaction, and restart loading. Archive failure must be caught by that plugin and converted to a valid termination response before returning a reduced body. Merely returning an error does not fail closed; an earlier plugin's reduction is not rolled back. No restart/replay, simultaneous-session, history-resend or crash-injection archive test was run because there is no supplied generic archive implementation to validate. This is an explicit **unresolved product behavior**, not a claim that filesystem persistence is impossible.

## 4. Gateway-owned tools, mixed client tools and streaming

The host offers the ingredients to write a custom orchestrator: model routing/executors, `host.model.execute` and streaming open/read/close, `host.http` bridges, stream emit/close and request/response rewriting [S02, S09, S20, S21]. The caller plugin is skipped for nested model routing/interception/completion, reducing direct self-recursion. This is not a generic recursion-depth or total-continuation budget; no such limit was found in the inspected callback/model-execution path. The runtime probe confirms a nested model call succeeds and is accounted; existing `TestIssue6332PluginNestedDelivery` exercises a mocked Codex Responses continuation/cancellation path through host callbacks [S31]. This is positive evidence for the mechanism, not proof of arbitrary agent behavior.

There is **no generic gateway tool registrar/dispatcher contract** in `sdk/pluginapi`, `sdk/pluginabi` or `internal/pluginhost`: no declared tool ownership, tool result submission API, shared continuation budget, or mixed-client-tool handoff state machine. The `claude-web-search-router` example implements a specialized route/executor, including host model forwarding and a web-search-only classifier. It is evidence that plugin-owned server behavior is feasible, not that the core provides a composable mixed-tool loop [S22]. Built-in provider web-search behavior elsewhere must likewise not be relabeled a public extension tool seam.

A plugin can construct its own private loop, advertise tools, perform tool calls, append results and call the model again. It must own: complete tool argument buffering, tool/result ID pairing, resolving only gateway-owned calls, returning unresolved client calls, subsequent client replay reconciliation, parallel tool semantics, budgets, cancellation, side-effect idempotency and durable continuation state. No end-to-end mixed gateway/client tools + streaming + next-client-replay test exists in this audit. Core support is needed if ProxyKit requires these invariants to be shared guarantees rather than duplicated plugin logic.

Streaming is incremental and cancellable through host bridges, with backpressure on unbuffered stream bridge channels. Interceptor history is bounded at 64 chunks / 1 MiB, and newer schema versions omit request bodies/history on payload chunks. That is not a full transcript archive. Dropping a payload stops subsequent chunk interceptors from seeing it; late response headers cannot retract committed HTTP bytes [S01, S17, S21]. The actual chat HTTP probe returned `[DONE]` after all payload chunks were dropped because the native handler supplies its own completion marker [S23]. Complete semantic terminal ownership, delta/final agreement and retracting delivered content are not promised by `DropChunk`.

## 5. Failure, ordering, cancellation and resources

- **Request/response/stream policies:** returned errors log-and-continue; recovered Go panics fuse the plugin. Request termination is a successful return value, not an error. No public per-role required/fail-closed setting was found in capability/config contracts. A policy whose own code catches an error can deny, but its unexpected crash/unload is not governed by a mandatory-policy host invariant [S17].
- **Schedulers:** handled reject and returned error stop selection; invalid/unhandled/panic fall back as described above. Routers likewise log failures and continue. This is role-specific behavior, not uniformly fail-open or uniformly fail-closed [S06–S08].
- **Ordering:** descending numeric priority, then plugin ID lexical order. No dependency graph/cycle validation. One authoritative scheduler is selected; transform chains are sequential. An observer sees mutations according to its position [S24].
- **Cancellation:** host HTTP operation IDs and callback context scoping support cancellation; existing tests verify cancellation before headers, while reading body, and on plugin/callback closure. The native call itself is a synchronous C function. `guardedPluginClient` returns when `ctx.Done()` fires but cannot forcibly end that function [S20, S25]. The added probe admitted 24 simultaneous calls, canceled every caller, and observed all inner calls still blocked until explicitly released. This test uses a blocking inner client, not an intentional hung native library; loader source establishes the C boundary.
- **Concurrency and deadlines:** no general worker pool/semaphore/call budget in the generic guarded-call path. Each call spawns work; 24 concurrent calls entered in the probe. Auth body decoding has a real separate two-slot/decoded-size bound; stream bridges also have local flow control. Neither establishes bounded total extension work. Lifecycle goroutines and the global usage queue are additional unbounded admission points [S04, S19, S25].
- **Storage/jobs:** no general host storage or job registration methods in the complete ABI enumeration. Native plugins can own durable state, goroutines and queues, but must provide retries, leases/idempotency, queue limits and shutdown behavior themselves [S02].
- **Reload:** snapshots are atomic, stale identities are filtered, guarded clients detach and wait/defer cleanup, and usage replacement has dedicated handling. Each hook fetches current active records; a stable plugin/config set spanning the whole request is not established. Native Go shared-library images remain resident because unloading their runtime is unsafe; native code is not an isolation sandbox [S24–S26].
- **Credential isolation:** auth callbacks expose physical credential JSON/runtime data to trusted native plugins. Plugins run in process and can use the OS directly. Metadata/schema declarations are not a security boundary [S02, S12, S26].

## 6. Arbitrary HTTP routes and protocols

Custom format identifiers are valid (`translator.FromString` accepts an arbitrary string). Request/response normalizers/translators and executor input/output formats permit plugin-defined protocol conversion/backend execution [S01, S27]. This is meaningful protocol extensibility at the model pipeline layer.

It does not create a generic listener/router. Data-plane paths are registered by the host; the dynamic plugin management/resource registrars have the restrictions verified above. `RequestInterceptResponse.Path` overrides the target of an already-handled execution; it does not register a new inbound protocol endpoint. `ProviderExecutor.HTTPRequest` and `HostHTTPClient` provide outbound HTTP work, not inbound route ownership. The repository has real built-in Responses/WebSocket/realtime paths; that is not a generic plugin WebSocket registration API. `WebSocketResponseObserver` observes events and does not itself implement an upgrade handler or arbitrary frame transform. For new inbound protocols/routes, embed/modify the Go server or let the plugin/service own a listener; qualify which responsibility is being assumed [S11, S14, S28].

## Work required to preserve the desired ProxyKit contracts

**Plugin work using existing seams:** client key/ACL lookup with exclusive frontend auth; account-choice policy over allowed candidates; custom auth acquisition/refresh; quota/usage presentation; durable recorder storage; archive/reconciliation database; any custom gateway tool continuation logic and budgets.

**Host changes or deliberate external infrastructure when required as host guarantees:** fail-closed required policies on error/panic/unload; narrowly authorized public POST routes or isolated enrollment listener; transactional activation + plugin state/outbox; exact parent-execution/attempt correlation; usage presence semantics; guaranteed raw observation independent of mutator ordering; bounded generic plugin execution and durable event/job delivery; generic gateway tool registration/mixed-tool continuation and replay ownership; arbitrary inbound routes/protocol adapters.

**Still not established:** a complete Codex Broker plugin; live OAuth/refresh/provider correctness; multi-tenant label/ACL enforcement under concurrent edits; archive/replay after process restart; mixed gateway/client tool correctness and client replay; all-provider immutable usage under translation mutation; safe cancellation of arbitrary native code; distributed storage consistency. The existing tested host primitives should not be mistaken for those completed products.

## Original-question completion ledger

| Requested question | Audit status |
| --- | --- |
| Inbound auth replacement and upstream selection | **Answered:** public seams traced, native frontend HTTP and scheduler contracts executed; eligibility/failure limits identified. |
| Credential refresh and account lifecycle | **Partially answered:** mock auth/refresh/save/OAuth tests pass; no live refresh or full Broker lifecycle port. |
| Public enrollment start/poll, account CRUD, label ACLs | **Answered for extension boundary:** management vs public GET vs separate listener established. Full enrollment, label transactions and outbox **not implemented/validated**. |
| Independent request/response/attempt usage recorder | **Partially answered:** real usage, failover, nested calls and presentation mutation tested; pristine all-protocol payload capture, missing-usage encoding and durable delivery not guaranteed. |
| Durable context archive, compaction, restart/replay | **Unresolved as product behavior:** primitives and absence of host archive/transaction seam traced; no supplied implementation to restart-test. |
| Gateway tools with mixed client tools and streaming | **Partially answered:** nested host calls/stream delivery tested; no generic ownership/continuation seam or full mixed-tool/replay test. |
| Policy failure, ordering, cancellation, bounded concurrency | **Answered for inspected boundaries:** fail-open behavior and 24 concurrent cancellation probe executed; arbitrary native execution cannot be forcibly canceled by the observed wrapper. |
| Storage/jobs and arbitrary routes/protocols | **Answered for API surface:** no general storage/job/route registration callbacks; protocol/executor and restricted HTTP route seams traced/tested. |
| Other extension needs | **Answered as gaps:** required-policy activation, exact accounting correlation, immutable observation, known/unknown usage, budgets and transaction ownership identified. |

## Pinned source index

All links below are exact-commit GitHub permalinks. Line anchors locate the relevant implementation, with the surrounding file providing context.

- [S00] — License.
- [S01] — Public capability enumeration; auth, scheduler, executor, interception, tools-related building blocks and usage types.
- [S02] — ABI/schema versions and complete host method enumeration.
- [S03] — Native JSON capability names and callback wrappers.
- [S04] — Frontend provider registration, exclusivity and error behavior (Authenticate at line 217); decoding bounds near file start.
- [S05] — Frontend provider-chain authentication and exhausted-provider behavior.
- [S06] — Single selected scheduler, error/panic/invalid-result behavior.
- [S07] — Router selection and errors continuing to other routes.
- [S08] — Eligibility/priority filtering; plugin selection/rejection starts at line 995.
- [S09] — Nested host model execution, forced provider/auth and caller-plugin skip IDs.
- [S10] — Plugin login start/poll; refresh implementation begins at line 961.
- [S11] — Plugin management auth vs public resource dispatch.
- [S12] — Auth save validation, disk write and runtime upsert.
- [S13] — Management OAuth polling and commit; compensating multi-record save begins at line 1803.
- [S14] — Management/resource route normalization and HTTP serving.
- [S15] — UsageRecord/UsageDetail, metadata and integer-only token counters.
- [S16] — Nonstream raw-body usage accounting; streaming observation at line 563.
- [S17] — Fail-open interceptors; async completion at 295; ordered response/chunk mutation at 329/360.
- [S18] — Independent per-usage UUID; contrast lifecycle UUID in handlers_interceptors.go line 119.
- [S19] — Ignored buffer parameter, in-memory append at 770 and sequential plugin dispatch at 793.
- [S20] — Generic host HTTP logging/read path, not model usage accounting.
- [S21] — Unbuffered plugin stream bridge and emit/close control.
- [S22] — Specialized web-search routing classifier; implementation is example-plugin-owned.
- [S23] — Native handler terminal marker after stream closes without delivered data.
- [S24] — Current active records; priority descending / ID ascending ordering at line 158.
- [S25] — Goroutine-per-call, context return without terminating inner call, asynchronous shutdown.
- [S26] — Native C call and shared-library lifecycle; accompanying loader_unix_test.go records resident Go-runtime semantics.
- [S27] — Arbitrary string format identifiers.
- [S28] — Built-in route registration, including realtime/WebSocket routes.
- [S29] — Usage plugin replacement pending queue and backpressure, distinct from global usage queue.
- [S30] — Separate lifecycle UUID generation.
- [S31] — Executed mock nested delivery / continuation / cancellation regression.
- [S32] — Executed scheduler error, panic, invalid return and rejection tests.
- [S33] — Executed physical-file auth saving regression.
- [S34] — Executed host HTTP cancellation regressions.
- [S35] — Executed exact-auth pinning across priorities without fallback.

[S00]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/LICENSE
[S01]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L74
[S02]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginabi/types.go#L5
[S03]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/rpc_schema.go#L20
[S04]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_auth.go#L93
[S05]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/access/manager.go#L35
[S06]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/scheduler.go#L11
[S07]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/model_router.go#L34
[S08]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/auth/conductor_selection.go#L569
[S09]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L403
[S10]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/auth_provider.go#L748
[S11]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/api/server_management.go#L254
[S12]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/auth_callbacks.go#L345
[S13]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/api/handlers/management/auth_files_provider_oauth.go#L1320
[S14]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/management.go#L118
[S15]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1644
[S16]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/openai_compat_executor.go#L247
[S17]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L21
[S18]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/helps/usage_helpers.go#L127
[S19]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/usage/manager.go#L648
[S20]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/http_bridge.go#L61
[S21]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/stream_bridge.go#L91
[S22]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/examples/plugin/claude-web-search-router/go/detect.go
[S23]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/openai/openai_handlers.go#L550
[S24]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/snapshot.go#L55
[S25]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/client_guard.go#L86
[S26]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L248
[S27]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/translator/format.go#L3
[S28]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/api/server_routes.go
[S29]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_usage_translation.go#L454
[S30]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_interceptors.go#L119
[S31]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/issue6332_delivery_test.go#L40
[S32]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/scheduler_test.go#L12
[S33]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/auth_callbacks_test.go#L369
[S34]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks_test.go#L326
[S35]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/model_execution_test.go#L864
