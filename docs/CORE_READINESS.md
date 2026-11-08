# Core readiness and extension implementation gates

Current implementation status: [implementation tracker](IMPLEMENTATION_TRACKER.md). Native execution, installed Node activation and optional application examples are now implemented; combined acceptance remains in progress. The verdict and checks below are the historical pre-extension review, not a description of the latest checkout. [Extension plan and decision log](EXTENSION_PLAN.md) retains design decisions; the R01–R50 table below remains the acceptance rubric.

Reviewed 2026-11-07 against commit `71a60824f1e2a4c94db31b4ae886e2221a33dd03`, Go 1.27.1, Linux amd64. This is a source/test review and proposed implementation sequence, not an implemented extension specification.

Current design refinement (2026-11-08): [extension composition](EXTENSION_COMPOSITION.md) supersedes the review’s mandatory credential authorization, operator ordering and non-disableable budget prescriptions. Review/test evidence below is unchanged; gates are conditional on the configured extension contract.

## Verdict

**The existing Go proxy is a sound transport foundation for incremental extension work. No blocker requiring a transport rewrite was found within its declared opaque-forwarding scope. It is not yet an extension execution layer.** Preserve the standard-library transport and its regression suite; introduce the control contracts below before claiming the corresponding extension capabilities.

The [six architecture decisions](research/2026-11-extension-audit/extension-requirements.md) govern ownership. The [engineering reference](EXTENSION_ENGINEERING_REFERENCE.md) supplies router evidence for R01–R50. Those rows describe capabilities across transport, optional protocol runtimes and extension applications. They are not 50 core modules, nor 50 independent unit tests that certify the system.

## Implemented evidence

Reviewed [proxy.go](../proxy/proxy.go), [main.go](../cmd/proxykit/main.go), all five Go test files, the module, CI workflow and transport documentation. The core wraps `httputil.ReverseProxy`, enables HTTP/1 full duplex, validates the upstream URL, uses a reusable cloned transport without environment proxy selection or automatic decompression, preserves incoming trailer updates and flushes response chunks incrementally. Configuration is set before serving.

The executable configures HTTP/1, TLS HTTP/2 and inbound prior-knowledge h2c, header/idle timeouts, ordinary HTTP draining and cancellation of upgraded connections after draining. It is a configured-upstream reverse proxy, not a CONNECT forward proxy or an authenticated public gateway.

The completed checks passed:

```text
go test -race -count=1 -timeout 90s ./...
ok  github.com/NotRllyRn/proxykit/cmd/proxykit  2.614s
ok  github.com/NotRllyRn/proxykit/proxy         1.962s

go vet ./...
exit 0
```

These results were obtained during this review before the documentation updates; no Go code changed afterward. Tests use local fixtures, including real sockets and a compiled-process startup/drain test. They cover uploads, opaque payloads, SSE, known-length incremental responses, HTTP/1 and HTTP/2 duplex behavior, TLS, compression, trailers, reuse, cancellation, upstream errors, native transforms and opaque WebSocket frames/disconnect/shutdown. See [transport coverage](TRANSPORT_COVERAGE.md) for detailed limits.

This does not establish production load capacity, aggregate bounded memory under slow clients, forced-drain deadline behavior, all informational-response paths, hook failures, tenant routing isolation, durable replay, original usage capture or live provider/client compatibility. Those require tests when the relevant boundaries are introduced. Opaque WebSocket forwarding does not imply frame hooks.

## Integration prerequisites, not current forwarding defects

1. **Request-local execution and decisions.** Native `Rewrite` cannot return an error; shared callback/transport configuration must remain immutable during service. Introduce exchange context and explicit outcomes appropriate to each stage before dispatch; ordinary transforms continue/replace, while resolvers may respond. Blocking protection is opt-in. Carry a stable configuration snapshot and request/attempt/parent identity. Do not change the shared `Rewrite` or `Transport` to select each request's destination.
2. **Route-bound credentials.** Passthrough authentication suits the current one-upstream contract. Brokering extensions separate inbound identity from outgoing credentials and own destination authorization, including fallback and nested calls; the generic host does not police credential compatibility. Final signing follows mutation. Accidental credential forwarding to another provider is an extension defect, not a mandatory host prevention feature.
3. **Body ownership and replay.** Current bodies are one-shot streams. Select metadata-only, streaming or explicitly bounded capture/spool modes. A retry, classifier or full-body transform must obtain replayable input or reject that operation. Define who closes each body, updates framing and retains trailers. Do not silently buffer every request.
4. **Attempt observation versus final delivery.** `RoundTrip` returning means headers arrived, not body completion. `ModifyResponse` is not a universal final-output hook: local output, errors and coordinator paths need explicit coverage. Capture original outcomes/usage before mutation, then distinguish selected output and delivery completion. Required audit handoff needs a failure policy; optional metrics can be bounded best effort.
5. **Commitment and exceptional paths.** Go's proxy can forward informational 1xx responses through `Got1xxResponse` before `ModifyResponse`; 101 has distinct upgrade handling. Define whether early hints are withheld or allowed during selection, and explicitly handle upgrades. Final-body rerouting requires bounded precommit buffering/spooling. Never retry transparently after client-visible final output. Preserve `http.ErrAbortHandler` semantics for truncated committed bodies; generic panic recovery must not produce a successful-looking completion.
6. **Transparent wrappers.** ResponseWriter wrappers must retain the capabilities needed by `ResponseController` (for example through `Unwrap`) and flushing/hijacking/duplex behavior. Upgraded response bodies must retain `io.ReadWriteCloser`. Prove these properties through sockets after adding middleware.
7. **Role availability and resource lifetime.** Add startup validation, declared failure handling, configurable admission limits, cancellation and cleanup with the first roles. A timeout stops waiting, not arbitrary Go/JavaScript execution; keep capacity charged until work actually ends. Extension-owned processes may be supervised separately. Do not promise untrusted isolation without an enforced boundary.

The current native surface is useful for proving these contracts. It should not be frozen as the public extension API before those proofs. No speculative HTTP parser, storage framework, scheduler or general workflow engine is warranted.

## Staged implementation and exit gates

Each stage is future work requiring implementation authorization. Complete one observable slice before expanding the public API. Keep the no-extension socket suite passing throughout.

**A — Managed execution foundation.** Use a native Go test extension first. Add immutable request context, ordered roles, explicit denial/local response, declared startup/error behavior, adjustable invocation limits and cleanup. A mock broker selects between two local upstreams, and an independent recorder observes attributed attempts. The extension owns account fixtures and any enrollment service. Prove parallel identities do not cross routes/credentials, denied operations never dispatch, and local/error output follows its declared delivery hooks. Include cancellation, overload and missing required roles. This is the first deliverable, not a complete Broker application.

**B — Protocol, body and delivery slice.** Select one protocol/client and introduce only the adapter and body modes it needs. Exercise ordinary/streamed output, original usage, missing usage, trailers, early hints, upgrades, truncation and local responses. Compare what the upstream receives with what the client receives. Preserve opaque pass-through. Settle the extension authoring/worker contract from this slice; if Node IPC is added, repeat its cancellation, overload, failure and streaming tests across the actual worker boundary.

**C — Optional tool coordination.** Implement one internal search tool plus a client-owned tool. Test streaming and nonstreaming continuations, repeated same-name calls with distinct IDs, unresolved client handoff, trusted tool context, cancellation and bounded parallelism. Every managed internal model call must traverse the same applicable policy/recording boundaries. The coordinator may be an extension or a shared optional AI package.

**D — Durable context extension.** Add extension-owned archive/revision/replay storage. Test archive failure before reduction, restart with replay, concurrent updates, tenant separation, private tool history and unsupported signed/provider-held state. Use a real temporary database and process restart where persistence is claimed; mocks alone do not prove recovery.

**E — Dynamic routing composition.** Add a second provider adapter and explicit capability/settings translation. Prove response-driven reroute before commitment, bounded classifier/candidate calls, cancellation of losers, state affinity, per-destination credentials, requested/served provenance and cache partitions. Side-effecting speculative tools require explicit selection/authorization or idempotency. Test postcommit refusal to reroute and partial candidate failures. A wire-format translation is not semantic equivalence between models.

**F — Demand-driven extras.** Add semantic WebSocket processing, convenience host routes/services or untrusted workers only for an actual use case. Separate extension services can already own HTTP/WebSocket listeners. A sandbox is a separate deployment/runtime feature, not a prerequisite for trusted extensions.

Final composition gate: run broker + an optional blocking-policy fixture + independent recorder + recall together, with fallback, one mixed tool response, restart/replay and precommit provider substitution. Verify observable output and the client's next replay request. Use unit tests for pure decisions/encoding, socket integration for transport and dispatch, and compiled-process/worker tests for startup, shutdown and recovery. Record tested versions, paths and unsupported modes. An isolated green test per row is insufficient when features interact.

## R01–R50 ownership and verification traceability

“Boundary” means proposed shared execution behavior; “extension” means application-owned behavior, not a mandatory host subsystem. Stages indicate when to prove the requirement, not a claim that it is implemented today. Router status labels remain in the engineering reference.

| Requirement | Ownership | Gate | Observable check |
|---|---|---|---|
| R01 Replace inbound authentication | Boundary + auth extension | A | Authenticate two concurrent identities; deny missing/invalid identity before protected dispatch. |
| R02 Choose account / upstream route | Boundary + route extension | A | Two upstreams receive only their selected request-local route. |
| R03 Replace credentials / implement refresh | Boundary + broker extension | A | Replace outgoing credentials; coordinate refresh without exposing another account’s secret. |
| R04 Own account catalog, labels and client ACLs | Extension + eligibility boundary | A | Account/label/ACL changes affect only committed eligible selections, including fallback. |
| R05 Serve public enrollment POST and private service auth | Extension | A | Own POST/service-auth server publishes only authorized, committed account eligibility. |
| R06 Run separate public enrollment listener/process | Extension/deployment | A | Expose enrollment separately; test process isolation only if claimed. |
| R07 Commit activation, labels and notification outbox | Extension | A | Crash/retry activation and notification handoff without exposing uncommitted eligibility. |
| R08 Quota snapshots / per-attempt outcomes | Extension + attempt observations | A/B | Keep quota snapshots distinct from attributed attempt outcomes and provider usage. |
| R09 Rewrite request / instructions / tool schemas | Transform boundary + adapter | B | Upstream sees intended instructions/tools; opaque fields and framing survive. |
| R10 Modify ordinary nonstream response | Delivery boundary + adapter | B | Mutate a complete response without changing the captured original evidence. |
| R11 Modify ordinary live stream | Delivery boundary + adapter | B | Incremental events and final objects agree under slow reads/cancellation. |
| R12 Same output hooks cover hidden tool loops | Dispatch/delivery boundaries | C | Hidden continuations cannot bypass applicable independent output hooks. |
| R13 Add new wire protocol through plugin | Protocol adapter | B/E | Round-trip supported fixtures; reject unsupported semantics instead of dropping data. |
| R14 Serve arbitrary HTTP endpoints and dashboard | Extension; optional host mount | A | Serve arbitrary routes on an owned listener; verify auth and shutdown. |
| R15 Own WebSocket service / modify gateway WebSockets | Extension; optional frame boundary | F | Own a WebSocket server; separately prove gateway-frame hooks if added. |
| R16 Own durable storage and tenant scoping | Extension | D | Persist across restart and prevent cross-tenant reads. |
| R17 Archive successfully before returning reduced context | Extension + awaited transform | D | Storage failure prevents removal of original context. |
| R18 Own context revisions, locks and concurrent state | Extension | D | Concurrent revisions conflict or serialize without lost updates. |
| R19 Reconcile private continuations with client replay | Extension + coordinator | D | Client replay retains private continuations without duplicates or history loss. |
| R20 Compaction with provider-held / signed state | Adapter + context extension | D | Preserve supported signed/provider-held state; reject unsupported compaction. |
| R21 Provide executable internal tools | Tool extension + optional coordinator | C | Execute a registered internal tool with explicit ownership. |
| R22 Implement nonstream model/tool continuation | Optional coordinator | C | Bound a nonstream loop and return a protocol-valid result. |
| R23 Implement streaming model/tool continuation | Optional coordinator + delivery | C | Stream valid continuations without bypassing final hooks. |
| R24 Preserve mixed client/internal calls and IDs | Optional coordinator + adapter | C | Pair duplicate-name calls by ID and stop for unresolved client results. |
| R25 Bind trusted caller/session to tool execution | Trusted context boundary | C | Tool arguments cannot impersonate the authenticated caller/session. |
| R26 Cancellation through every internal tool | Boundary + tool implementation | C | Cancel dependent work; stop new admission; distinguish wait cancellation from execution termination. |
| R27 Bound tool turns, spend and parallel work | Shared limits + coordinator | C/E | Bound rounds, elapsed time, applicable spend/output and actual concurrent work. |
| R28 Independent request/response/usage extension | Observation boundary + extension | A/B | Independent recorder sees originals even when another extension rewrites output. |
| R29 Actual usage retained before presentation edits | Capture boundary + adapter | B | Presentation edits do not change recorded actual provider usage. |
| R30 Account for internal model calls / attempts | Managed dispatch + recorder | C/E | Attribute internal calls, candidates, fallback and retries separately. |
| R31 Reliable lifecycle / attempt correlation | Execution context boundary | A | Correlate request/attempt/parent IDs through success, failure and cancellation. |
| R32 Distinguish missing usage from measured zero | Adapter + recorder | B | Absent usage stays unknown; measured zero remains distinguishable. |
| R33 Persist telemetry after capture / avoid pre-capture loss | Capture boundary + extension sink | B | Required durable handoff fails explicitly; optional queue saturation reports loss. |
| R34 Explicit policy denial before upstream | Decision boundary + policy | A | Explicit denial sends no protected upstream request. |
| R35 Fail-closed mandatory policy on unexpected error | Required-role boundary | A | Missing, failed, timed-out or invalid mandatory check cannot fail open. |
| R36 Bound extension-owned workers / host hook execution | Host admission + extension workers | A | Overload is bounded and capacity remains charged while underlying work runs. |
| R37 Deterministic extension order | Composition boundary | A | Developer-declared stage/order is repeatable; cycles, missing prerequisites and duplicate/exclusive conflicts reject activation. |
| R38 Coordinate cooperating extensions / compactor choice | Composition + extension coordinator | D | One selected compaction coordinator owns a revision; cooperative strategies are explicit. |
| R39 Keep configuration stable for active work | Lifetime boundary | A | An active stream and its nested calls keep a stable configuration snapshot. |
| R40 Own background jobs and lifecycle | Extension + lifecycle | A | Stop admission, drain/cancel work and close resources in declared order. |
| R41 Sandbox untrusted extension code | Optional enforced runtime/deployment | F | If offered, demonstrate actual filesystem/network/resource restrictions; manifests alone fail this claim. |
| R42 Short-circuit cache / local response | Local-response boundary + cache extension | A/B | Local hits traverse final delivery; misses continue ordinary dispatch explicitly. |
| R43 Run a full extension application plus gateway adapter | Runtime/adapter + extension | A | Own listener/store/workers can exchange trusted context with gateway hooks and clean up. |
| R44 Negotiate target capabilities and translate model-specific settings | Route extension + adapters | E | Translate supported reasoning/tools/settings; explicitly reject unsupported target features. |
| R45 Reroute after inspecting an upstream outcome before client commitment | Coordinator + commit boundary | E | Inspect an outcome and reroute before delivery; refuse unsafe postcommit replacement. |
| R46 Coordinate classifier calls, candidate fan-out, hedging and result selection | Coordinator + shared dispatch limits | E | Bound candidates/classifiers, attribute all calls and cancel losers without unselected side effects. |
| R47 Preserve the client protocol contract across provider/model substitution | Adapters + delivery | E | Client parses substituted provider events/tools/errors in its requested wire protocol. |
| R48 Preserve or migrate conversation affinity and provider-held state when rerouting | State extension + adapters | D/E | Preserve affinity, reconstruct supported portable state or reject migration. |
| R49 Bind each route change to destination-specific credentials and authorization | Dispatch authorization/credentials | A/E | When a credential/protection extension claims this capability, verify it authorizes nested/fallback targets and binds credentials; generic forwarding does not enforce it. |
| R50 Separate requested/served route provenance and partition caches accordingly | Provenance boundary + cache extension | E | Keep requested/served identity distinct and partition caches by effective scope/settings/state. |
