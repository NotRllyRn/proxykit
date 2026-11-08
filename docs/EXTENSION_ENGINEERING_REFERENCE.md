# ProxyKit extension engineering reference

Current planning entry point: [Extension plan and decision log](EXTENSION_PLAN.md). Its latest decisions supersede conflicting earlier proposals; implementation and source evidence below retain their original scope.

Date: 2026-11-07. **Research and proposed extension design.** The original audit predates the accepted [Go transport milestone](TRANSPORT_DECISION.md); the extension runtime/API remains proposed. Statements below about unsettled language/runtime refer to that original design stage, not a reversal of the transport decision.

This reference covers all 50 requirements in the [current extension checklist](research/2026-11-extension-audit/capabilities-current.json). It records how audited routers expose useful capabilities, what integration remains, and the smallest ProxyKit contract worth adopting. The [current six architecture decisions](research/2026-11-extension-audit/extension-requirements.md) define ownership; the [core readiness review](CORE_READINESS.md) defines staged implementation and verification gates. Host databases, enrollment endpoints and job services are optional conveniences.

Use the [searchable engineering view](research/2026-11-extension-audit/engineering-reference.html) to filter requirements by router/status and read their evidence. Fifteen focused research groups covered two or three requirements each using GPT-6.1 Terra, with one rate-limited group completed by GPT-6.1 Sol; the findings were then reconciled into this reference. Three additional Terra groups audited R44–R50; parent review added target-specific reasoning translation evidence.

The recommendation is to borrow CCR's authoring and protected-hook patterns, CPA's explicit broker/executor boundaries, and Bifrost's typed response and tracing patterns. Keep enrollment, databases, dashboards, and jobs in extensions. None establishes the complete combination of mandatory policy availability, universal hook coverage, streaming mixed-tool orchestration, durable replay, and independent accounting.

This review also tightens four earlier interpretations: Bifrost already has allowlisted per-request remote MCP headers (R25); CCR's MCP abort wrapper can stop waiting without stopping remote work (R26); CCR's outer lifecycle and core role-specific cleanup differ (R40); and CCR's local-response capability does not establish transparent cache-miss fall-through (R42). The existing status labels remain, with these qualifications made explicit.

Latest direction: [extension composition](EXTENSION_COMPOSITION.md) supersedes older proposed operator-controlled ordering and mandatory credential policing below. Router evidence and R01–R50 status labels remain historical research; protection capabilities are optional, not universal host restrictions.

## Dynamic routing: what is already covered and what is new

**Basic model/provider/endpoint replacement is already R02**, with R03 for credentials, R09 for settings, and R10–R13 for response and protocol adaptation. The router may ignore the requested model. It still needs a compatible target adapter and a client-compatible result; changing the model label alone does not provide either.

**R44–R50 make the combined guarantees explicit:** target capability negotiation; response-driven precommit rerouting; classifier/candidate orchestration; client-protocol fidelity; provider-state migration; destination/credential authorization; and truthful route provenance/cache separation. All three routers currently score **Integration needed** on these complete compositions. This does not downgrade their supported component APIs or imply they cannot perform a particular model switch.

For the requested example, preserve original client intent, choose the target model/provider and a supported reasoning setting, bind the new credential/endpoint, translate the request, dispatch, and encode the answer in the original client's protocol. Keep the actual served route and usage in accounting. No live test establishes that the example model labels, reasoning options, or particular client/provider pair work. If routing examines an actual upstream answer, the first call has already happened; it can be replaced transparently only before commitment and subject to side-effect and budget rules.

See [scenario coverage and requirement definitions](research/2026-11-extension-audit/dynamic-routing-scope.md). These seven additions constrain shared dispatch/adapters and extension-owned coordination; they do not call for seven core frameworks.

## Read the statuses as contract boundaries, not effort estimates

| Status | What it establishes | What it does not establish |
|---|---|---|
| **Supported** | An applicable gateway/runtime mechanism exists at the audited interface. Each entry explains its scope. | A complete Broker, recall extension, or production integration already exists; every protocol/path works. |
| **Extension-owned** | Trusted extension code can provide the feature using its own runtime, services, or storage. No specialized host service is necessary. | The router implements the feature for you, or a second listener fixes missing interception hooks. |
| **Integration needed** | Useful primitives exist, but wiring, coverage, ownership, or end-to-end behavior remains unresolved. | A small change, a configuration-only fix, or a guarantee that no core change is required. |
| **Host gap** | A specific required guarantee is missing or contradicted at the audited host boundary. | The feature is impossible, the whole router must be rewritten, or the fix must be large. |
| **Unknown** | Evidence is insufficient to establish the capability. | Support or a demonstrated absence. |
| **Optional** | Outside the present trusted-extension requirement; retain as a future decision. | Implemented sandboxing or isolation. |

The earlier **Integration needed** category was deliberately broad. To make it actionable, entries identify these paths where evidence permits:

- **CONFIG:** configure or register existing host functionality.
- **EXTENSION:** implement application logic or a coordinator using existing interfaces.
- **SDK-HOST:** wire a custom server/SDK host; this changes packaging and deployment even if upstream library code is unchanged.
- **CORE/UNVERIFIED:** an audited path may need a core adaptation, or interface coverage has not been established. This is not a proven plugin-only solution.

For example, Bifrost's custom `Account` implementation is an **SDK-HOST** integration: the interface exists, but the stock native plugin's `Init(config)` does not receive an account-provider registrar. CLIProxyAPI's ignored interceptor errors are a **host gap** for mandatory fail-closed policy: an extension can deliberately return a denial, but cannot guarantee denial when the host ignores an unexpected callback failure. CCR's default-closed core hook runner makes that policy closer, yet startup can omit a failed plugin; required-role availability still needs integration. These distinctions describe control, not development cost.

## What belongs to an extension application

“Public enrollment POST plus private service auth” means an enrollment application can accept a browser operation such as `POST /enroll/start`, while separately authenticating calls from that application to a private broker service. Those are example application endpoints, not mandatory ProxyKit endpoints. The extension can own the HTTP server, cookies, CSRF/state checks, OAuth flow, dashboard, database, labels, outbox, and background workers. The gateway only needs to honor the resulting account eligibility and credentials through its routing/authentication contracts.

A second port separates routing and deployment concerns. Separate process credentials and deployment permissions are needed if the public enrollment service must not access private broker secrets. Neither arrangement requires a universal host enrollment API. Likewise, “atomic activation, labels, and notification outbox” can be one transaction in the extension's database; it does not make a provider network call or a second host credential store part of that transaction.

All three audited trusted runtimes allow an extension application plus an adapter. Their adapters differ: CCR loads JavaScript modules and offers backend/lifecycle helpers; CLIProxyAPI loads a native C ABI plugin; Bifrost loads native Go symbols. A separate service can use another language, but communication with it still needs a compatible adapter, identity propagation, deadlines, and cleanup. Merely running on the same server does not supply missing request hooks.

## Recommended shared contracts (proposed)

The 50 requirements should constrain a few composable boundaries, not create 50 core frameworks:

1. **Transport and delivery.** The generic HTTP core owns connections, backpressure, cancellation, bounded buffers, commitment, and one final delivery path. Raw response transforms run there regardless of whether the response came from upstream, a cache, or an optional coordinator. Semantic transforms belong to a protocol adapter. WebSocket mutation is a separate transport contract.
2. **Identity, routing, and dispatch.** A selected authentication provider returns a trusted principal. A selected route provider returns an eligible upstream decision. One dispatch primitive carries identity, request/execution/attempt/parent IDs, deadline, cancellation, and retry limits through all gateway-managed upstream calls. Refresh policy and account state remain in the broker extension. Required checks run on every applicable attempt; recursive calls cannot simply bypass them.
3. **Ordered transforms and explicit decisions.** Use developer-declared stages and stable ordering with narrow dependencies; users do not manually order hooks. Return a discriminated continue/deny/respond result rather than relying on exceptions for ordinary decisions. Required-role errors, timeouts, and absence block the protected operation. Validate duplicate exclusive roles at startup. Avoid a dependency graph engine until a real case needs it.
4. **Capture and observation.** Capture original provider usage/outcome before presentation mutation, with an explicit unknown state. Optional observers may use bounded best-effort delivery. A required audit sink needs an awaited durable handoff and a documented failure policy. The sink's database remains extension-owned; no exactly-once claim spans an external provider and a local crash.
5. **Extension lifetime.** Setup returns registrations and cleanup. Allow ordinary runtime libraries and separately supervised applications. Start with startup configuration and drain/restart updates; do not build hot reload merely to satisfy a hypothetical deployment.

Provide model/tool continuation as an **optional shared AI runtime** above this core. It can own protocol state machines and call-ID pairing while feature extensions supply tools, archive/replay persistence, policies, and budgets. An extension may also own its coordinator, provided it uses the dispatch/delivery boundaries for behavior the gateway promises. The host cannot observe arbitrary network calls made privately by trusted native code.

Choose one initial protocol/client and prove Broker plus recall coexist. Add adapters from actual compatibility needs. Approachable TypeScript authoring remains an ergonomic preference, not a language decision. Short functions and ordinary data structures are preferable; streaming commitment, durable replay, and policy availability cannot safely be reduced to an event callback with unspecified semantics.

## Evidence and scope

The requirement sections use source inspection and the earlier local audit tests. Acceptance examples are **proposed future checks, not newly executed tests**. This work does not claim every repository line was read, every provider was exercised, or any full extension application was deployed.

| Router | Audited source | Important boundary |
|---|---|---|
| Claude Code Router (CCR) | [`057abaa1f95519ab8faf999e1ccef2b80ccdeb9e`](https://github.com/musistudio/claude-code-router/tree/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e), wrapper 3.1.3 | Outer plugin service and packaged core have different APIs and failure behavior. |
| CCR packaged core | [`@the-next-ai/ai-gateway@1.0.21` tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz) | `G/path:line` refers to recovered source-map modules identified by the [manifest and hashes](research/2026-11-extension-audit/ccr-manifest.json), not an independently verified GitHub checkout. |
| CLIProxyAPI (CPA) | [`91e0dee827abf476f9f86246f68c500a112f7155`](https://github.com/router-for-me/CLIProxyAPI/tree/91e0dee827abf476f9f86246f68c500a112f7155) | Native plugin callbacks, SDK host APIs, and management endpoints are distinct surfaces. |
| Bifrost | [`6e640279b36337820f2088fb85f6e78f339063c3`](https://github.com/maximhq/bifrost/tree/6e640279b36337820f2088fb85f6e78f339063c3) | SDK account/tool injection exceeds what stock dynamic `Init(config)` receives. |

Prior test evidence: [CCR](research/2026-11-extension-audit/ccr.md) has 12 local probes passing, plus 51 selected upstream tests passing and one failing permission-default test; [CPA](research/2026-11-extension-audit/cliproxyapi.md) has selected suites and 17 loaded-plugin HTTP assertions passing; [Bifrost](research/2026-11-extension-audit/bifrost-test-summary.md) has 54 selected tests passing, including eight added probes. Some probes pass by demonstrating a limitation. Bifrost's native loader was source-inspected, not exercised by those probes. No live OAuth, provider/client compatibility, distributed-state, or restart-replay success is implied.

The root licenses were MIT for CCR/CPA and Apache-2.0 for Bifrost; CCR's packaged core declares MIT. These are architectural recommendations, not a source-code port. Review exact file/dependency terms if implementation code is later copied.

## Checklist

The matrix preserves R01–R43 from the original self-hosted classification and adds R44–R50 for dynamic-routing composition. The detailed entries qualify its scope and identify any source-backed corrections; supported cells are not whole-feature certifications.

| Requirement | CCR | CLIProxyAPI | Bifrost |
|---|---|---|---|
| [R01 — Replace inbound authentication](#r01) | Integration needed | Supported | Integration needed |
| [R02 — Choose account / upstream route](#r02) | Supported | Supported | Integration needed |
| [R03 — Replace credentials / implement refresh](#r03) | Supported | Supported | Integration needed |
| [R04 — Own account catalog, labels and client ACLs](#r04) | Extension-owned | Integration needed | Integration needed |
| [R05 — Serve public enrollment POST and private service auth](#r05) | Extension-owned | Extension-owned | Extension-owned |
| [R06 — Run separate public enrollment listener/process](#r06) | Extension-owned | Extension-owned | Extension-owned |
| [R07 — Commit activation, labels and notification outbox](#r07) | Extension-owned | Integration needed | Integration needed |
| [R08 — Quota snapshots / per-attempt outcomes](#r08) | Supported | Supported | Integration needed |
| [R09 — Rewrite request / instructions / tool schemas](#r09) | Supported | Supported | Supported |
| [R10 — Modify ordinary nonstream response](#r10) | Supported | Supported | Supported |
| [R11 — Modify ordinary live stream](#r11) | Supported | Supported | Supported |
| [R12 — Same output hooks cover hidden tool loops](#r12) | Host gap | Integration needed | Integration needed |
| [R13 — Add new wire protocol through plugin](#r13) | Supported | Integration needed | Integration needed |
| [R14 — Serve arbitrary HTTP endpoints and dashboard](#r14) | Extension-owned | Extension-owned | Extension-owned |
| [R15 — Own WebSocket service / modify gateway WebSockets](#r15) | Integration needed | Integration needed | Integration needed |
| [R16 — Own durable storage and tenant scoping](#r16) | Extension-owned | Extension-owned | Extension-owned |
| [R17 — Archive successfully before returning reduced context](#r17) | Extension-owned | Extension-owned | Extension-owned |
| [R18 — Own context revisions, locks and concurrent state](#r18) | Extension-owned | Extension-owned | Extension-owned |
| [R19 — Reconcile private continuations with client replay](#r19) | Integration needed | Integration needed | Integration needed |
| [R20 — Compaction with provider-held / signed state](#r20) | Unknown | Unknown | Unknown |
| [R21 — Provide executable internal tools](#r21) | Supported | Integration needed | Supported |
| [R22 — Implement nonstream model/tool continuation](#r22) | Supported | Integration needed | Supported |
| [R23 — Implement streaming model/tool continuation](#r23) | Integration needed | Integration needed | Integration needed |
| [R24 — Preserve mixed client/internal calls and IDs](#r24) | Integration needed | Integration needed | Integration needed |
| [R25 — Bind trusted caller/session to tool execution](#r25) | Integration needed | Integration needed | Integration needed |
| [R26 — Cancellation through every internal tool](#r26) | Integration needed | Integration needed | Integration needed |
| [R27 — Bound tool turns, spend and parallel work](#r27) | Extension-owned | Extension-owned | Extension-owned |
| [R28 — Independent request/response/usage extension](#r28) | Supported | Supported | Supported |
| [R29 — Actual usage retained before presentation edits](#r29) | Integration needed | Integration needed | Integration needed |
| [R30 — Account for internal model calls / attempts](#r30) | Integration needed | Supported | Supported |
| [R31 — Reliable lifecycle / attempt correlation](#r31) | Integration needed | Integration needed | Integration needed |
| [R32 — Distinguish missing usage from measured zero](#r32) | Unknown | Integration needed | Unknown |
| [R33 — Persist telemetry after capture / avoid pre-capture loss](#r33) | Integration needed | Integration needed | Integration needed |
| [R34 — Explicit policy denial before upstream](#r34) | Supported | Supported | Supported |
| [R35 — Fail-closed mandatory policy on unexpected error](#r35) | Integration needed | Host gap | Host gap |
| [R36 — Bound extension-owned workers / host hook execution](#r36) | Supported | Integration needed | Integration needed |
| [R37 — Deterministic extension order](#r37) | Supported | Supported | Supported |
| [R38 — Coordinate cooperating extensions / compactor choice](#r38) | Extension-owned | Extension-owned | Extension-owned |
| [R39 — Keep configuration stable for active work](#r39) | Integration needed | Integration needed | Integration needed |
| [R40 — Own background jobs and lifecycle](#r40) | Extension-owned | Extension-owned | Extension-owned |
| [R41 — Sandbox untrusted extension code](#r41) | Optional | Optional | Optional |
| [R42 — Short-circuit cache / local response](#r42) | Supported | Supported | Supported |
| [R43 — Run a full extension application plus gateway adapter](#r43) | Supported | Supported | Supported |
| [R44 — Negotiate target capabilities and translate model-specific settings](#r44) | Integration needed | Integration needed | Integration needed |
| [R45 — Reroute after inspecting an upstream outcome before client commitment](#r45) | Integration needed | Integration needed | Integration needed |
| [R46 — Coordinate classifier calls, candidate fan-out, hedging and result selection](#r46) | Integration needed | Integration needed | Integration needed |
| [R47 — Preserve the client protocol contract across provider/model substitution](#r47) | Integration needed | Integration needed | Integration needed |
| [R48 — Preserve or migrate conversation affinity and provider-held state when rerouting](#r48) | Integration needed | Integration needed | Integration needed |
| [R49 — Bind each route change to destination-specific credentials and authorization](#r49) | Integration needed | Integration needed | Integration needed |
| [R50 — Separate requested/served route provenance and partition caches accordingly](#r50) | Integration needed | Integration needed | Integration needed |

## Requirement-by-requirement engineering notes


<a id="r01"></a>

## R01 — Replace inbound authentication

**Status:** CCR: Integration needed; CLIProxyAPI: Supported; Bifrost: Integration needed.

**Implementation evidence.**

- **CLIProxyAPI:** `FrontendAuthProvider.Authenticate(ctx, FrontendAuthRequest)` receives method, path, headers, query and raw/available decoded body; it returns an authenticated principal and metadata, or a denial. Registration selects the highest-priority `FrontendAuthProviderExclusive`, breaking ties by plugin ID, as the sole ordinary frontend provider ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L524), [registration](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_auth.go#L93)). The adapter maps explicit denial to an auth error and principal/metadata to the host result; provider errors and recovered panics become not-handled ([adapter](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_auth.go#L217)). A prior native HTTP probe accepted a request without the configured native API key and denied another before upstream traffic; this is scoped evidence, not complete deployment coverage.
- **CCR:** `beforeAuth` can reject but built-in authentication remains separate (G/types.ts:1532). **Bifrost:** pre-auth hooks can translate credentials or deny; stock governance remains authoritative ([middleware](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/handlers/middlewares.go#L1778)).

**Integration / gaps.** **CONFIG:** CPA requires exclusivity to prevent another provider accepting requests; disabling the plugin can restore other configuration. Management authentication is separate ([management](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/api/server_management.go#L254)). **CORE/UNVERIFIED:** CCR and stock Bifrost need a verified replacement boundary, not just an extra gate. G/ citations identify recovered npm source-map modules, not GitHub source: [artifact manifest](research/2026-11-extension-audit/ccr-manifest.json), [exact tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).

**ProxyKit recommendation (proposed).** Transport core selects one configured required authentication provider for a protected route and passes its verified principal to downstream roles. A feature extension owns identity policy; provider failure or absence rejects. Keep administrative auth separately configurable. This borrows CPA's explicit exclusive selection while making mandatory availability an operator contract.

**Pattern choice.** Borrow CPA's explicit authentication-provider role. Select the provider by configuration rather than copying a priority contest; add operator-required availability so unloading or startup failure cannot restore an unintended fallback.

**Acceptance example (not executed).** With a required custom provider, a valid Example user credential reaches the upstream with the correct principal; an invalid credential and an unavailable provider both prevent forwarding.

<a id="r02"></a>

## R02 — Choose account / upstream route

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Integration needed.

**Routing scope clarification.** This includes ignoring the client-requested model and choosing another provider/model/endpoint with appropriate credentials and settings; it is not limited to choosing accounts for the same model. CPA explicitly exposes `ModelRouteResponse.TargetModel` on provider routes ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L735)). Arbitrary endpoints still require a compatible provider configuration, executor or adapter. The [dynamic-routing expansion](research/2026-11-extension-audit/dynamic-routing-scope.md) distinguishes this existing capability from the additional audited end-to-end guarantees in R44–R50.

**Implementation evidence.**

- **CCR:** `GatewayPluginRouteResolver.resolve(input)` can return `targetProvider`, `targetProviderName`, `targetProviderConfig`, or ordered `targetProviders` containing provider configs (G/types.ts:1493–1521). The handler invokes registered resolvers after the before-routing hook, checks failure, applies returned model/body/headers and resolves targets (G/gateway/handler.ts:416–484); resolver execution is in G/plugins/hooks.ts:242. A prior mock probe selected a dynamic provider and replaced a synthetic key. G/ denotes recovered source-map modules: [manifest](research/2026-11-extension-audit/ccr-manifest.json), [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).
- **CLIProxyAPI:** `ModelRouter.RouteModel` selects a built-in provider, self, or plugin executor; the host validates availability and continues on invalid result or router error ([router](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/model_router.go#L33)). `Scheduler.Pick` receives model, headers, metadata and host-offered candidates, returning an AuthID, built-in delegation, or handled rejection ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L570), [selection](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/auth/conductor_selection.go#L995)). The highest-priority scheduler alone is tried; invalid/unhandled results fall back, errors and explicit rejection stop selection ([host](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/scheduler.go#L11)). Prior tests cover filtering, selection and failure behavior.
- **Bifrost:** stock plugins can pin eligible keys; full `Account` / `KeySelector` wiring is an SDK host surface ([SDK config](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/bifrost.go#L20)).

**Integration / gaps.** **EXTENSION:** CCR's dynamic route and CPA's candidate selection are mechanisms, not finished broker policy. **CONFIG/CORE:** CPA filters eligibility and usually offers the highest available priority tier; across-priority mode still respects eligibility, and Home mode can bypass the scheduler. **SDK-HOST:** Bifrost needs custom host wiring for account replacement.

**ProxyKit recommendation (proposed).** Transport core owns attempts, cancellation and retry safety; a feature extension supplies a route provider returning an ordered list of eligible account/endpoint references or explicit rejection. Revalidate eligibility on each attempt. This combines CCR's dynamic routes with CPA's explicit rejection without surrendering retry control.

**Pattern choice.** Use CCR's ability to return a dynamic route and CPA's validation of eligible account choices. One authoritative route provider is simpler than combining competing resolvers and schedulers; retry safety remains a shared transport concern.

**Acceptance example (not executed).** A request from Work laptop selects an allowed available account; an empty eligible set returns an explicit failure without contacting an unapproved account.

<a id="r03"></a>

## R03 — Replace credentials / implement refresh

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** `ProviderPlugin.authenticate(input)` returns `Result<UpstreamRequest>` and can replace URL, headers and body (G/types.ts:1403–1428). The ordinary handler matches each provider plugin, invokes its protected authentication operation and uses the returned upstream request; a returned failure aborts that stage (G/gateway/handler.ts:8220–8317). The previous mock verified replacing a synthetic credential; the plugin must implement refresh and storage itself. G/ denotes recovered source-map modules: [manifest](research/2026-11-extension-audit/ccr-manifest.json), [exact npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).
- **CLIProxyAPI:** `AuthProvider` exposes `Identifier`, `ParseAuth`, `StartLogin`, `PollLogin` and `RefreshAuth`; `AuthData` includes provider-owned `StorageJSON`, metadata, attributes and `NextRefreshAfter` ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L262)). Host `RefreshAuth` finds the matching provider, passes auth data and a host HTTP client, then merges returned data while preserving missing identity, metadata, priority and disabled state ([implementation](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/auth_provider.go#L961)). Prior tests cover refresh metadata preservation and login callbacks; live OAuth was not tested.
- **Bifrost:** a custom SDK `Account` can return refreshed provider keys; the stock plugin loader does not expose general account-provider registration ([Account contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/account.go#L1403), [stock server setup](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/server/server.go#L3622)).

**Integration / gaps.** **EXTENSION:** CCR refresh lifecycle and CPA's specific login/refresh implementation remain application work; neither status establishes live provider compatibility or coordinated token rotation. **SDK-HOST:** Bifrost requires custom account wiring or another verified publication path. Built-in OAuth grant support is narrower than a general user-account refresh provider.

**ProxyKit recommendation (proposed).** A feature extension owns credential acquisition, durable storage and refresh; transport core requests credentials for the selected account immediately before an attempt and injects them into its upstream request. Define cancellation, expiry and refresh failure explicitly; coordinate concurrent refreshes per account and persist rotated credentials before use. Expose only the needed credential reference to unrelated roles.

**Pattern choice.** Start with CCR's small just-in-time credential boundary. Keep CPA-style start/poll/refresh operations inside the broker application until more than one extension needs a shared login API; a mandatory host OAuth framework is unnecessary.

**Acceptance example (not executed).** Two concurrent requests encounter an expired account credential: one refresh succeeds, both forward with the new credential, and a restart can recover the committed rotated credential.

<a id="r04"></a>

## R04 — Own account catalog, labels and client ACLs

**Status:** CCR: Extension-owned; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Its trusted JS loader imports the configured module and invokes `setup(context)` or `activate(context)` ([service.ts:526](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L526)). In the shipped gateway, `routeResolvers[].resolve(input)` returns a provider config or ordered `targetProviders`; `ProviderPlugin.authenticate(input)` returns an `UpstreamRequest` ([G/types.ts:1403, 1495](research/2026-11-extension-audit/ccr-manifest.json); [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)). The resolver loop invokes matching resolvers; the handler invokes matching provider authentication before forwarding (G/plugins/hooks.ts:242; G/gateway/handler.ts:8238). G paths and line numbers refer to the recovered source-map entries recorded in the manifest, not GitHub source. These are extension mechanisms, not a supplied catalog or proven ACL product.
- **CLIProxyAPI:** Frontend auth and scheduler can bind a principal and pick among host candidates. `PickAuth` rejects unknown candidate IDs; an unhandled/invalid choice or recovered panic permits fallback ([scheduler.go:11](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/scheduler.go#L11)). Host auth save exists, but `host.auth.delete` is absent from the audited callbacks ([auth_callbacks.go:345](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/auth_callbacks.go#L345)).
- **Bifrost:** Built-in virtual-key governance manages key restrictions ([governance.go:1820](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/handlers/governance.go#L1820)); the SDK `Account` exposes provider/key/config lookup ([account.go:1403](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/account.go#L1403)). Stock `.so` initialization does not inject that SDK interface ([soloader.go:70](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L70)).

**Integration / gaps.** EXTENSION: implement authoritative catalog, labels, ACLs and transactional changes. CCR must apply eligibility to routing and credential selection, including fallbacks; its owned label is feasibility, not established complete coverage. CLIProxyAPI needs host-record reconciliation and eligibility on every pick, with no guaranteed fail-closed scheduler on panic (CORE/UNVERIFIED if mandatory). Bifrost needs governance mapping or a custom SDK host (SDK-HOST). No complete implementation of this requirement was established.

**ProxyKit recommendation (proposed).** Feature extension owns catalog and ACL; transport core invokes a required route provider with authenticated principal and attempt context, accepting an eligible credential reference or explicit denial. Recheck every fallback; fail the request when required selection is unavailable. Language and runtime remain undecided.

**Acceptance example (not executed).** After revoking Example user's access to a labeled account, a request and its fallback attempts never use that account and return an explicit failure if no eligible account remains.

<a id="r05"></a>

## R05 — Serve public enrollment POST and private service auth

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** `setup(context)` executes trusted imported JavaScript; its context also offers `registerGatewayRoute` and `registerHttpBackend` ([service.ts:526](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L526), [service.ts:765](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L765)). Core `httpRoutes` permits a method/path and `auth: gateway | none`, not scoped service authentication (G/types.ts:1586; [manifest](research/2026-11-extension-audit/ccr-manifest.json), [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)).
- **CLIProxyAPI:** The native loader opens a shared library and calls `cliproxy_plugin_init(hostAPI, pluginAPI)` ([loader_unix.go:179](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179)); trusted plugin code can run its own HTTP service. Host plugin management routes are separately authenticated; public plugin resources are GET-only ([management.go:118](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/management.go#L118)).
- **Bifrost:** `plugin.Open` loads trusted Go code and calls optional `Init(config any) error`; required `Cleanup` provides a shutdown hook ([soloader.go:59](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59)). Plugin code can start its own HTTP listener; the loader does not provide a generic route registrar.

**Integration / gaps.** EXTENSION: implement `POST /start`, cookie/session handling, private service authentication and enrollment itself, on an extension-owned server or supervised companion. Publishing usable credentials to the gateway is separate integration work. Prior CCR route and CPA management/resource probes did not establish public OAuth enrollment; no new test was run. Owned denotes a viable runtime, not a shipped or tested feature.

**ProxyKit recommendation (proposed).** Feature extension owns both HTTP interfaces and authentication, using ordinary application libraries or a companion service. Keep enrollment domain and its service credentials out of transport core; expose only the credential/route integration required by the chosen broker.

**Acceptance example (not executed).** A public `POST /start` initiates an enrollment session, while the private enrollment operation rejects a request without its service credential.

<a id="r06"></a>

## R06 — Run separate public enrollment listener/process

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** Trusted module import followed by `setup`/`activate` executes application code, and `registerHttpBackend` offers a managed backend registration seam ([service.ts:526](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L526), [service.ts:775](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L775)). An application can instead own a listener or contact a separately supervised service.
- **CLIProxyAPI:** `dlopen` and `cliproxy_plugin_init` execute trusted native code in the host process ([loader_unix.go:179](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179)). It can start a listener or connect to a companion; neither is a host-provided isolation boundary.
- **Bifrost:** Go `plugin.Open`, optional `Init(config any) error`, and required `Cleanup() error` run in process ([soloader.go:59](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59)). The plugin can run a listener or adapter to a separately supervised process.

**Integration / gaps.** EXTENSION and deployment: implement and supervise the listener, lifecycle, networking and any separate credential stores. A port alone does not isolate process memory or credentials. The earlier “gap” labels concerned absence of a host-managed isolated listener; the current owned labels correctly describe the broader trusted self-hosted deployment, not evidence that isolation is supplied or tested. No new tests were run.

**ProxyKit recommendation (proposed).** Keep enrollment as a feature extension or independently supervised application with a narrow gateway adapter. If isolation is required, use separate processes and appropriate OS identities, network policy and stores; no enrollment-specific listener manager is needed in the transport core.

**Acceptance example (not executed).** With enrollment and gateway in separate processes, restarting the public enrollment process leaves gateway request forwarding available and the private service rejects unauthorized calls.

<a id="r07"></a>

## R07 — Commit activation, labels and notification outbox

**Status:** CCR: Extension-owned; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Trusted plugins receive `openSqliteStore` and may register core gateway/provider plugins via [service.ts](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L730). Core `routeResolvers.resolve` and `ProviderPlugin.authenticate` can consult plugin-owned committed state (G/types.ts:1424, 1509). This is a feasible mechanism, not an implemented activation transaction.
- **CLIProxyAPI:** A native shared library can own a database and invoke network services; its [loader](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L248) loads trusted native code. Host credential publication separately writes a file and updates runtime state ([auth_callbacks.go](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/auth_callbacks.go#L345)).
- **Bifrost:** A trusted Go `.so` can own a database and workers; the [loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L67) calls `Init(config)` and loads hook symbols, without injecting an account registry. A custom host can supply an SDK account implementation; it is a distinct integration.

**Integration / gaps.** **EXTENSION:** Commit activation, labels and a uniquely identified outbox row in one extension-owned database transaction; dispatch notifications idempotently after commit. **SDK-HOST / CORE/UNVERIFIED:** CCR route/auth must actually enforce committed eligibility on each applicable selection; CPA and Bifrost publication and routing must honor it, including fallback. Reconciliation across separate stores is not a cross-store transaction. No crash/restart or full enrollment behavior was established; the original labels describe ownership or integration, not a delivered feature. G citations identify recovered @the-next-ai/ai-gateway 1.0.21 source ([manifest](research/2026-11-extension-audit/ccr-manifest.json); [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)).

**ProxyKit recommendation (proposed).** Feature extension owns the database, activation policy and notification worker. An account-selection provider reads only committed eligible accounts; transport core accepts its decision and fails explicitly if none is eligible. If publication is needed, reconcile idempotently and recheck eligibility at selection. No mature complete implementation was established.

**Acceptance example (not executed).** A transaction failure leaves an account ineligible and creates no notification; after successful commit, the outbox worker retries with the same event ID and a receiver supporting that ID deduplicates delivery. Without receiver idempotency, duplicate external notifications remain possible.

<a id="r08"></a>

## R08 — Quota snapshots / per-attempt outcomes

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** `registerProviderAccountConnector` accepts `{id, resolve(request)}`, returning meters or a snapshot; account service looks up and invokes it ([contract](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L68), [dispatch](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/providers/account-service.ts#L808)). Core billing hooks can transform or drop events before delivery (G/billing/publisher.ts:198, 453). Prior probes observed actual upstream usage on ordinary and internal-loop cases; they did not establish all failed retries or immutable per-attempt records. G means recovered package source ([manifest](research/2026-11-extension-audit/ccr-manifest.json); [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)).
- **CLIProxyAPI:** `QuotaProvider` exposes `Identifier`, `DescribeQuota`, `FetchQuota`, `ResetQuota`; fetch receives credential IDs, metadata and host HTTP client ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1737), [dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/quota_provider.go#L311)). `UsagePlugin.HandleUsage` receives records with `AuthID`, `SelectionID`, failure and token fields ([types](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1644)); prior mock probes observed ordinary, failover and nested usage, not universal correlation or coverage.
- **Bifrost:** Core retries/rotates keys and emits retry spans ([executor](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L7271)); `ObservabilityPlugin.Inject` receives traces, but saturated exporters can drop them ([tracer](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/tracing/tracer.go#L1380)).

**Integration / gaps.** The Supported labels are accurate for exposed primitives but overstate complete per-attempt outcomes if read literally: coverage, correlation, durable delivery and failed-attempt usage remain unestablished. **EXTENSION:** quota fetching, snapshots and policy. **CORE/UNVERIFIED:** guarantee an attributed record for each actual attempt; missing provider usage is unknown, never zero.

**ProxyKit recommendation (proposed).** Feature extension owns quota refresh/storage; transport core emits attempt start/end with request and attempt IDs, selected account, outcome and optional raw provider usage. Keep quota snapshots distinct from request usage. Do not claim an existing exemplar guarantees complete accounting.

**Pattern choice.** Use CPA's explicit account-attributed quota/usage concepts and CCR's pluggable snapshot fetcher. Keep quota fetching extension-owned and expose generic attempt outcomes from dispatch, avoiding a quota-specific transport subsystem.

**Acceptance example (not executed).** One failed account attempt followed by a successful second attempt produces two correlated outcomes, with absent usage marked unknown and a separately timestamped quota snapshot.

<a id="r09"></a>

## R09 — Rewrite request / instructions / tool schemas

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** Outer `registerGatewayRequestTransform` processes cloned body/headers in order and applies returned changes; thrown errors are logged and skipped ([service.ts](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L338)). Core `GatewayPluginRequestTransform.transform` receives staged raw body or `standardRequest` with `instructions` and `tools`; provider `transformRequest` modifies the upstream request (G/types.ts:1192, 1403, 1478; G/gateway/handler.ts:8316). The handler applies before-routing transformations before routing (G/gateway/handler.ts:360). G is recovered 1.0.21 package source ([manifest](research/2026-11-extension-audit/ccr-manifest.json); [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)).
- **CLIProxyAPI:** `InterceptRequestBeforeAuth` / `InterceptRequestAfterAuth` return request changes; `PatchRequestBody` supplies localized edits, and `TranslateRequest` translates payloads ([API](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1172), [patcher](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1307)). Host logs and skips interceptor errors ([dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L21)).
- **Bifrost:** `HTTPTransportPreHook` mutates the HTTP request after authentication; `PreRequestHook` mutates typed input, params and tools once, before `PreLLMHook`, which runs again on fallbacks ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L328), [typed hooks](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L411)). LLM hook errors are logged and ignored; explicit short-circuits differ.

**Integration / gaps.** **EXTENSION:** supply instructions and schemas; adapter and route determine protocol validity. Supported mutation does not establish preservation of all unknown/signed fields or universal client compatibility. Required checks need enforced failure policy, not assumed hook behavior.

**ProxyKit recommendation (proposed).** Optional AI runtime exposes one ordered, protocol-adapted request transform with explicit instructions/tools and preservation rules; transport core exposes bounded raw HTTP access. Feature extensions own content. Define raw/semantic ordering and reject unsupported combinations; choose operator-controlled failure policy.

**Pattern choice.** Use CCR's few named stages and Bifrost's typed AI payload where an adapter is installed. CPA's byte-range patch ABI is useful for demonstrated IPC costs, but defer it until a selected runtime and measurements justify the complexity.

**Acceptance example (not executed).** A configured extension adds one instruction and tool schema to a supported request; the captured upstream payload contains both and retains an unrelated opaque field.

<a id="r10"></a>

## R10 — Modify ordinary nonstream response

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** The shipped core exposes provider `transformResponse` and gateway `responseHooks[].transformResponse(input)`, whose result may replace `responsePayload`, status and headers. The ordinary handler applies final hooks after conversion and sends their result; matching hooks chain through `applyGatewayPluginResponseHooks` (G/types.ts:1426, 1557–1584; G/plugins/hooks.ts:319–350; G/gateway/handler.ts:1035–1064, 2057–2085). Here and below, **G** means recovered source-map sources from [the exact npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), mapped by [the audit manifest](research/2026-11-extension-audit/ccr-manifest.json), not GitHub source.
- **CLIProxyAPI:** `ResponseInterceptor.InterceptResponse(ctx, ResponseInterceptRequest) (ResponseInterceptResponse, error)` is invoked on successful execution before delivery; the host chains cloned body/header results, applying nonempty body replacements and header mutations ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1203), [handler](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_execution.go#L122), [chain](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L329)). Errors in interceptors are ignored; this is not a required-policy guarantee.
- **Bifrost:** `PostLLMHook(ctx, resp, bifrostErr)` can return modified typed response/error; core calls it after an attempt, in reverse entered-plugin order. HTTP plugins can additionally mutate `HTTPTransportPostHook(ctx, req, resp)` in place; it only applies to HTTP transport, and large streamed bodies may be absent ([contracts](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L251), [HTTP contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L344), [core call](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L5628)). LLM hook errors are logged and do not themselves deny ([pipeline](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9934)).

**Integration / gaps.** Supported describes ordinary paths, not universal errors, hidden loops, or protocol compatibility; see R12. Prior audit probes exercised ordinary response hooks, not every adapter.

**ProxyKit recommendation (proposed).** Transport core supplies a bounded pre-commit response transform with explicit body/status/header result and failure policy; optional AI runtime supplies an adapter-aware typed transform where needed. Feature extensions supply edits. Preserve original accounting separately and validate framing after mutation.

**Pattern choice.** Use CCR's explicit returned response edits at one final boundary, plus Bifrost's distinction between typed model output and HTTP output. Avoid CPA's nonempty-body sentinel: an explicit replacement must also support an intentionally empty body.

**Acceptance example (not executed).** A configured response extension replaces a JSON field; the client receives valid JSON and correct framing, while recorded upstream usage remains unchanged.

<a id="r11"></a>

## R11 — Modify ordinary live stream

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** `streamHooks[].transformResponse({upstreamRequest, upstreamResponse, standardRequest, ...})` returns a replacement `Response` or hook result. Ordinary passthrough and translated stream branches invoke the chain before relay (G/types.ts:1543–1556; G/gateway/handler.ts:1075, 1402). The [G source-map manifest](research/2026-11-extension-audit/ccr-manifest.json) and [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz) identify this shipped code.
- **CLIProxyAPI:** `StreamChunkInterceptor.InterceptStreamChunk(ctx, request) (response, error)` runs on successful chunks; host chains body/header edits or `DropChunk`, with a separate header initialization index. Its handler applies results before forwarding ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1208), [chain](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L359), [handler](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_stream.go#L263)). OpenAI handler separately emits its terminal marker ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/openai/openai_handlers.go#L567)); interceptor failure is ignored.
- **Bifrost:** typed `PostLLMHook` runs on streaming responses and `HTTPTransportStreamChunkHook(ctx, req, chunk)` can replace, drop, or terminate a transport chunk. Transport chains hooks in reverse order ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L377), [implementation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/lib/config.go#L8538), [typed pipeline](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9950)). Prior tests exercised drop and error semantics at the hook pipeline; full network payload editing and terminal-object reconciliation remain untested.

**Integration / gaps.** These are stream seams, not proof arbitrary chunks are complete semantic events or that all loop output is covered. Prior tests are documented in [Bifrost's test summary](research/2026-11-extension-audit/bifrost-test-summary.md); no new tests were run.

**ProxyKit recommendation (proposed).** Transport core offers an ordered backpressure-aware byte stream transform, cancellation and bounded buffering; optional AI runtime parses supported protocol events and owns consistent deltas/final objects. Extensions select the level needed. Define failure before and after commitment, including explicit abort; do not silently retry visible output.

**Pattern choice.** Combine CCR's stream-wrapper ergonomics with Bifrost's separate precommit header hook. CPA's stream bridge demonstrates a bounded 16-chunk queue and omits full request/history from modern per-chunk ABI payloads; borrow bounded buffering and avoid repeated history copies, not the native ABI itself.

**Acceptance example (not executed).** A semantic transform changes a streamed text item and its completed object consistently; a slow client bounds buffering and cancellation stops upstream work.

<a id="r12"></a>

## R12 — Same output hooks cover hidden tool loops

**Status:** CCR: Host gap; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Virtual execution partitions internal/client calls and returns through `sendVirtualModelResponse`, which sends the nonstream payload directly (G/gateway/handler.ts:3190–3225, 4820–4871); optimistic virtual streaming likewise bypasses the ordinary stream hook. Prior mock probes observed both omissions, while ordinary hooks ran. G references are recovered from [the npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), per [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** A plugin's nested host model call sets `SkipInterceptorPluginID` for its own interceptor ([callback](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L435)); normal handler output hooks respect that exclusion. The audited API does not establish a generic host tool loop.
- **Bifrost:** Its agent loop calls tools, constructs a new request and calls `adapter.makeLLMCall`; nonstream continuation reenters core processing ([loop](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L375), [adapter](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agentadaptors.go#L182)). Per-attempt typed hooks therefore have a useful seam, but final HTTP and per-attempt hooks are distinct; the reviewed agent loop has no streaming equivalent. This does not establish same-hook coverage of all hidden and final output.

**Integration / gaps.** No mature universal implementation was established. **CORE/UNVERIFIED:** CCR needs virtual final/optimistic paths wired through an appropriate output boundary; provider transforms are a separate seam. **EXTENSION / CORE/UNVERIFIED:** CPA can own the loop through existing callbacks; composing independent hooks still needs explicit recursion and coverage rules. **EXTENSION / SDK-HOST / CORE/UNVERIFIED:** Bifrost needs coverage defined across its typed and HTTP roles and a streaming loop if required. These are boundary gaps, not claims a rewrite is necessary.

**ProxyKit recommendation (proposed).** Optional AI runtime owns continuation and routes each model attempt through a named per-attempt hook, then runs a distinct final-delivery hook exactly once on all exit paths, including mixed client tools. Feature extensions implement policy; transport core handles commitment. Give hooks phase/attempt identity and bounded recursion, preserving tool IDs and accounting. Do not claim per-attempt hooks alone see final synthesized output.

**Acceptance example (not executed).** With one internal tool continuation, a recorder observes two attempt outputs and exactly one client-visible final output; a required final policy also runs when the loop stops for a client-owned tool.

<a id="r13"></a>

## R13 — Add new wire protocol through plugin

**Status:** CCR: Supported; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** The shipped gateway's plugin result accepts `sourceAdapters` and `targetAdapters`; its loader registers both in runtime registries (G/plugins/loader.ts:44,193). A source adapter declares `key`, `provider`, optional method/path `routes`, `toStandardRequest`, `fromStandardResponse`, `isStreamingRequest`, and `buildPassthroughRequest`; a target declares provider matching, `buildRequestFromStandard`, and `toStandardResponse` (G/types.ts:1226,1258). The router matches a source route, and the handler selects adapters, converts requests and responses around upstream execution (G/gateway/routes.ts:454; G/gateway/handler.ts:311,654,696,783,1549,2025). This establishes a real adapter mechanism, not preservation or full streaming compatibility for every new protocol. G denotes recovered sources from [the exact npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), mapped by [the source-map manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** `FromString` permits custom format identifiers, but translation/executor registration alone does not register an inbound endpoint ([format.go](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/translator/format.go#L7)).
- **Bifrost:** Custom provider configuration requires a supported base provider, so it reuses existing wire semantics ([config.go](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/lib/config.go#L10452)).

**Integration / gaps.** CCR's adapter must implement and validate its own conversion, streaming and preservation; the audited source requires a JSON object body (G/gateway/handler.ts:320). CLIProxyAPI requires **SDK-HOST/EXTENSION** inbound routing or a separate listener; Bifrost requires **SDK-HOST/CORE** wiring for a genuinely new protocol, or an external adapter. These labels describe the audited integration boundary, not an effort estimate.

**ProxyKit recommendation (proposed).** Keep ordinary HTTP transport in the core; add a small optional AI runtime adapter contract only when a concrete protocol needs it. An adapter identifies its routes, decodes and encodes that protocol, and explicitly declares streaming and opaque-field handling. An extension-owned server can bridge to the gateway where appropriate. Validate round trips and client/provider versions; do not imply universal compatibility.

**Pattern choice.** Borrow CCR's registerable source/target adapters without making its JSON-object assumption a property of generic HTTP. Start with one adapter and preservation tests; do not build a universal intermediate model before the second protocol requires it.

**Acceptance example (not executed).** A test adapter accepts its registered request path, converts a request to a supported upstream format, and returns a response preserving a designated opaque field.

<a id="r14"></a>

## R14 — Serve arbitrary HTTP endpoints and dashboard

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** Trusted JavaScript plugins are dynamically imported ([service.ts](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L986)) and can run a server or companion. For same-port integration, `registerGatewayRoute` and `registerHttpBackend` are exposed with surface/permission checks; routes carry path or prefix, methods and auth ([service.ts](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L158), [registration](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L674)). The gateway also loads `httpRoutes` and invokes matching handlers with request/reply and gateway-or-no auth (G/plugins/loader.ts:235; G/types.ts:1586; G/gateway/routes.ts:340,421; [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json)).
- **CLIProxyAPI:** Native shared libraries execute in process via `dlopen` and plugin initialization ([loader_unix.go](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L44)); trusted native code can run a listener or use a companion. Prior HTTP probes found management-prefix routes and public GET resources, but an arbitrary public POST returned 404; this does not prevent a separate service.
- **Bifrost:** `plugin.Open` loads a trusted Go shared object and invokes optional `Init(config)` ([soloader.go](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59)); plugin code can run a listener or communicate with a companion. No route registrar is injected by this `Init` contract.

**Integration / gaps.** **EXTENSION** owns the application, HTTP server, authentication and dashboard; listener access is a feasibility inference from trusted code, not a prior end-to-end test. A shared gateway port is optional **SDK-HOST/CORE** integration where existing registration is insufficient. The earlier gateway-route-only labels (CCR Available, CLIProxyAPI Partial, Bifrost Gap) measure a narrower boundary; they do not contradict extension ownership of a separate service.

**ProxyKit recommendation (proposed).** Keep dashboard and service logic in the feature extension, with its own listener or separately supervised application and gateway adapter. Do not require core endpoints, storage, or jobs solely to host it. Add same-port mounting only for a concrete deployment need.

**Acceptance example (not executed).** An extension's separate dashboard serves a configured POST endpoint and a browser page while the gateway continues forwarding ordinary requests.

<a id="r15"></a>

## R15 — Own WebSocket service / modify gateway WebSockets

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** The gateway explicitly registers built-in Responses and MCP WebSocket routes (G/index.ts:314,321; [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json)). Trusted imported JavaScript can instead own a listener ([service.ts](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L986)); a generic hook for existing socket upgrades and both frame directions was not established.
- **CLIProxyAPI:** The server has `AttachWebsocketRoute(path, handler)` on its Gin engine, subject to Home-mode and auth behavior ([server_routes.go](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/api/server_routes.go#L567)); this is a server API, not evidence it is exposed to ordinary native plugins. The plugin observer receives upstream response event payloads and returns no replacement ([handlers_interceptors.go](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_interceptors.go#L577)). Native code can own a separate listener.
- **Bifrost:** Its built-in realtime handler registers specific GET routes and performs upgrades ([wsrealtime.go](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/handlers/wsrealtime.go#L63)); trusted Go plugins can own a separate listener ([soloader.go](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59)). Dynamic `Init(config)` does not itself supply a generic socket registrar.

**Integration / gaps.** **EXTENSION** can own an independent WebSocket service; no gateway hook is needed for that part. For existing gateway sockets, **CORE/UNVERIFIED**: upgrade ownership and bidirectional mutation/coverage are not established; CLIProxyAPI's observer is insufficient and Bifrost's route-specific hook coverage needs evaluation. CCR's original Unverified and the other routers' Partial labels concerned the narrower generic gateway socket feature. No mature generic implementation or new wire test was established here.

**ProxyKit recommendation (proposed).** Let an extension own its socket server where appropriate. If a concrete gateway use case requires interception, add a transport-core upgrade boundary with explicit bidirectional frame ownership, bounded buffering, close/cancellation and backpressure; keep protocol-specific behavior in an extension or optional adapter.

**Acceptance example (not executed).** A client connects to an extension-owned WebSocket service, sends a message, receives its response, and observes a clean close; a gateway-interception claim would require separate bidirectional tests.

<a id="r16"></a>

## R16 — Own durable storage and tenant scoping

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** The shipped gateway dynamically imports a configured Node module and awaits `createGatewayPlugin({config, plugin})` or its default export; its returned transforms are registered (G/plugins/loader.ts:570–598, 215–221). This trusted code can open its own database or contact a separately supervised service. CCR's distinct built-in archive opens SQLite and creates a snapshot table; this is a concrete storage example, not a generic tenant-scoped plugin store ([store.ts](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive/store.ts#L43-L80)). G denotes recovered source from the [1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), identified by the [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** Its Unix loader opens native code and calls `cliproxy_plugin_init(host, plugin)` in process ([loader](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179-L225)). That trusted native adapter can access a database directly or call a companion; its request API exposes `CallerScope` when available ([types](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1256-L1281)).
- **Bifrost:** The native `.so` loader uses `plugin.Open`, invokes optional `Init(config any) error`, and requires `Cleanup() error` ([loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59-L106)). Trusted Go code can own storage or connect to a companion; post-auth HTTP hooks can see resolved caller identity ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L328-L342)).

**Integration / gaps.** **EXTENSION:** Supply migrations, tenant authorization, retention, backup and database credentials. **SDK-HOST / UNVERIFIED:** Identity availability and authority depend on the chosen request path; caller scope is not a conversation ID. No restart or tenant-isolation validation was established. A host database API is optional.

**ProxyKit recommendation (proposed).** A feature extension owns an ordinary durable database with every record keyed by authenticated tenant, including indexes and access checks; use a companion where appropriate. The transport core supplies authenticated request context only when that integration exists. No mature general tenant-scoped extension storage implementation was established.

**Acceptance example (not executed).** After a restart, Example user can retrieve their archived record; a different tenant cannot retrieve it even with its record ID.

<a id="r17"></a>

## R17 — Archive successfully before returning reduced context

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** The Node factory and registered transforms described in R16 permit owned database I/O. `GatewayPluginRequestTransform.transform(input)` returns a value or Promise containing `requestBody` or `standardRequest` (G/types.ts:1448–1492); the handler awaits `beforeRouting` transforms before accepting the resulting body (G/gateway/handler.ts:320–365). Source-map provenance: [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json). The separate built-in archive stores a pending snapshot in a SQLite transaction ([store](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive/store.ts#L84-L126)); this does not prove a generic archive-before-removal contract.
- **CLIProxyAPI:** Trusted native adapter or companion may write the archive. `InterceptRequestBeforeAuth/AfterAuth` receives a body and returns replacement `Body`, edits or `Terminate` ([API](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1192-L1195), [payload](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1256-L1304)); the host synchronously invokes and applies the result ([chain](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L198-L225)).
- **Bifrost:** Native plugin or companion may write the archive; its post-auth `HTTPTransportPreHook(ctx, req)` can mutate the body or return a local response/error ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L328-L342)); `PreLLMHook` is called synchronously before provider execution ([pipeline](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9817-L9845)).

**Integration / gaps.** **EXTENSION / CONFIG:** One coordinator must own both archive and reduction; earlier independent reductions cannot be rolled back. Catch commit failure and preserve the original input or explicitly deny. CPA ignores interceptor errors and Bifrost LLM hooks log errors and continue ([CPA](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L22-L38), [Bifrost](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9843-L9852)); unexpected failure policy and route coverage remain **CORE / UNVERIFIED**. No complete generic implementation or crash test was established.

**ProxyKit recommendation (proposed).** Feature extension builds a candidate reduction, durably commits originals and mapping, then returns it; on failure return unchanged history or an explicit denial. Transport or optional AI runtime needs an awaited pre-forward boundary and an enforceable required-role failure policy where required.

**Acceptance example (not executed).** Inject an archive commit failure and verify that upstream receives the complete original history or no request, never reduced history.

<a id="r18"></a>

## R18 — Own context revisions, locks and concurrent state

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** Its trusted Node module factory and request transforms can use a database or companion (G/plugins/loader.ts:570–598, G/types.ts:1478–1492; [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json)). Separately, the built-in archive's SQLite transaction reads the latest generation and inserts a pending successor, with unique `(session_id, generation)` ([store](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive/store.ts#L57-L75), [transaction](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive/store.ts#L84-L119)). This is lineage, not a general revision/CAS replay contract.
- **CLIProxyAPI:** In-process native `cliproxy_plugin_init` can create or contact an extension-owned state service ([loader](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179-L225)); interceptors synchronously receive request metadata and an optional caller scope ([request](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1256-L1281)).
- **Bifrost:** Go `.so` `Init(config any)` can initialize state or connect to a supervised service, and `Cleanup` closes resources ([loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L67-L106)); post-auth transport hooks expose request and caller context ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L328-L342)). Injected SDK `KVStore` has TTL/NX operations, but is not a durable session-revision service offered to each stock plugin ([interface](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/kvstore.go#L5)).

**Integration / gaps.** **EXTENSION:** Implement revision checks, conflict handling and restart recovery; use database transactions or leases with fencing for multi-process ownership. **SDK-HOST / UNVERIFIED:** Require a trusted tenant plus stable explicit conversation identity; do not infer identity from caller scope or similarity. No concurrent revision or restart-replay validation was established, and no mature generic implementation was established.

**ProxyKit recommendation (proposed).** The feature extension stores `(tenant, conversation, revision)` with archive mappings in one transactional store; update only when the expected revision matches, then retry/reconcile on conflict. Use per-session serialization only where necessary, with database-enforced correctness across processes. Keep revision policy outside the transport core.

**Acceptance example (not executed).** Two requests read revision 4 concurrently: only one commits revision 5; the other detects the conflict and cannot overwrite the first mapping.

<a id="r19"></a>

## R19 — Reconcile private continuations with client replay

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Virtual-profile configuration explicitly says internal tool call/result pairs are missing from the next client replay; `foldInternalResults` instead exposes results as assistant text and defaults off (G/types.ts:884–901, recovered from the [1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz); source-map hashes and paths in [manifest](research/2026-11-extension-audit/ccr-manifest.json)). The previous synthetic replay probe confirmed the hidden result was absent; ordinary final hooks also miss virtual paths in that audit.
- **CLIProxyAPI:** Request interception and nested `host.model.execute` offer controllable request bodies and model calls; the callback resolves the request context, invokes the executor and returns status, headers and body ([callback](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407)). These are primitives, not a replay ledger.
- **Bifrost:** `executeAgent` creates conversation history and executed-call/result collections as local variables ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L140)). Its mixed-result synthesis is keyed by tool name ([adapter](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agentadaptors.go#L348)); a prior duplicate-name probe observed one result lost.

**Integration / gaps.** **EXTENSION / CORE/UNVERIFIED:** An extension needs authenticated conversation identity, a durable per-call ledger with visible-history revision mapping, concurrency control and replay reconstruction. Capture must occur before hidden data is discarded: CCR may need a controlled tool path or its own orchestrator; Bifrost needs capture before lossy synthesis or its own loop; CPA can own the loop. Hook coverage and restart-safe reconciliation are not established; a shared host guarantee may require a host patch. No mature complete implementation was established.

**ProxyKit recommendation (proposed).** An optional AI runtime coordinates the tool loop and exposes ordered, ID-paired private continuation events to a feature extension owning durable storage. Scope records by authenticated identity and explicit conversation ID; atomically commit a revision before relying on it, and reinsert matching private items on replay. If identity or revision cannot be verified, decline reconciliation rather than merge histories. Keep this coordination out of generic HTTP transport.

**Acceptance example (not executed).** After an internal tool result and restart, a client resending its visible transcript causes the next upstream request to include the matching private call/result exactly once; a different conversation never receives it.

<a id="r20"></a>

## R20 — Compaction with provider-held / signed state

**Status:** CCR: Unknown; CLIProxyAPI: Unknown; Bifrost: Unknown.

**Implementation evidence.**

- **CCR:** `prepareContextArchiveRequest` recognizes supported protocol and explicit or Codex compaction signals, snapshots the original request, and appends an archive handoff task ([source](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/gateway/context-archive.ts#L461)). This specific archive flow does not establish general opaque/signed state preservation.
- **CLIProxyAPI:** Its WebSocket turn error explicitly marks continuations referring to a lost socket and requests full input without `previous_response_id` ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/executor/websocket.go#L33)). This recovery signal does not reconstruct provider-held history or validate a compactor.
- **Bifrost:** `BifrostCompactionRequest` has `previous_response_id`, input and raw request fields; its OpenAI conversion copies the ID and normalizes input ([schema](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/responses.go#L165), [conversion](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/providers/openai/responses.go#L1644)). This is provider compaction support, not proof of arbitrary extension rewrite fidelity.

**Integration / gaps.** **EXTENSION / CORE/UNVERIFIED:** No router establishes the complete requirement for an extension-owned compactor across provider-held references, signed reasoning and replay. Protocol-specific resolution, original-to-replacement mapping and archive-before-removal are needed; ability to store a snapshot or forward an opaque item alone is insufficient. The prior audit did not test full state preservation. Unknown labels remain warranted; do not infer absence of provider compaction.

**ProxyKit recommendation (proposed).** A feature extension owns compaction and its archive; an optional AI runtime adapter declares the state forms it can safely preserve or resolve. Keep a provider reference and associated signed/opaque fields intact unless a protocol-specific path reconstructs valid full context or performs a verified provider compaction; otherwise bypass compaction or return an explicit unsupported-state error. Do not silently remove a reference or rewrite signed content. No mature general implementation was established.

**Acceptance example (not executed).** A request containing `previous_response_id` and signed reasoning is either forwarded intact on a supported path or rejected for compaction with the originals preserved; it is never forwarded as a reduced history with the reference silently removed.

<a id="r21"></a>

## R21 — Provide executable internal tools

**Status:** CCR: Supported; CLIProxyAPI: Integration needed; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** `registerCoreGatewayVirtualModelProfile(profile)` is exposed by the wrapper ([contract](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L158)). Profiles specify tool name, schema, `visibility: internal|client`, `tool_loop`, turn/call limits and streaming mode; `executeInternalVirtualToolCalls` selects internal owners, parses arguments, invokes the MCP tool provider and creates results paired with `call_id`. The provider resolves the configured server and calls its remote tool (G/types.ts:873–903; G/gateway/handler.ts:3578–3653; G/agent/tools.ts:115–164; recovered from the [1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), with source-map hashes in [manifest](research/2026-11-extension-audit/ccr-manifest.json)). Previous mock probes observed nonstream and optimistic SSE continuations.
- **Bifrost:** An operator can configure an extension-owned MCP server. SDK hosts can alternatively call `RegisterMCPTool(name, description, func(args any) (string, error), schemas.ChatTool)`; it requires an MCP manager and delegates to `RegisterTool`, which creates a local MCP handler ([SDK](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L4612), [registration](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/clientmanager.go#L2988)). Nonstream Chat calls the MCP agent after the initial response ([entry](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L1063)); the loop classifies executable tools and continues within its depth limit ([loop](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L176)).
- **CLIProxyAPI:** **EXTENSION:** An executor can dispatch its own tools and use `host.model.execute` for continuation ([callback](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407)); no shared generic tool registrar was established.

**Integration / gaps.** **CONFIG / SDK-HOST / EXTENSION:** Supported means a mechanism exists, not complete integration. CCR's virtual execution uses synthetic profile session context and does not pass request cancellation to the tool provider (G/gateway/handler.ts:3600–3643); trusted tenant context needs separate wiring. Bifrost's stock dynamic plugin is not handed an SDK instance, its agent loop is nonstreaming, and mixed summaries can lose duplicate tool results. Neither mechanism proves universal client compatibility or replay correctness.

**ProxyKit recommendation (proposed).** Let feature extensions register a schema and executable handler; an optional AI runtime supplies trusted request context, call IDs, cancellation and bounded continuation, while transport remains generic. Use MCP as an optional adapter for separately supervised tools, not a mandatory additional server. Stop for unresolved client-owned calls and retain their IDs. CCR's profile separation and Bifrost's SDK callback are useful precedents, not complete mixed-tool solutions.

**Pattern choice.** Combine CCR's explicit internal/client ownership with a direct handler resembling Bifrost's SDK registration, adding trusted context and cancellation. Direct local registration avoids requiring every simple tool to deploy an MCP server; MCP remains an adapter.

**Acceptance example (not executed).** A model calls an extension-owned lookup tool; the runtime executes it once with trusted request context, pairs its result to the call ID, and sends a continuation to the model.

<a id="r22"></a>

## R22 — Implement nonstream model/tool continuation

**Status:** CCR: Supported; CLIProxyAPI: Integration needed; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** `VirtualModelProfileConfig` exposes `tools` with `internal`/`client` visibility and `execution` with `tool_loop`, `maxTurns`, `maxToolCalls`, and client-tool policy. The loop builds an upstream request each turn, partitions returned calls by owner, executes internal calls, appends call/results to the working request, and aggregates usage (G/types.ts:873–923; G/gateway/handler.ts:2863–2869,3189–3195,3284–3316). Separately, enabled `transparentToolExecution` resolves configured MCP tools and runs only on nonstream requests (G/gateway/handler.ts:1990–1992,2169–2221,4910–4914,5235–5266). G is recovered shipped core source from [@the-next-ai/ai-gateway 1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), indexed by [source map manifest](research/2026-11-extension-audit/ccr-manifest.json); these are not CCR GitHub source paths. Prior mock tests observed two upstream calls, one internal execution, and summed usage; they do not establish replay correctness.
- **Bifrost:** `ChatCompletionRequest` and `ResponsesRequest` invoke MCP agent checks after the first complete response ([entry points](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L1063)). `executeAgent` classifies auto-executable tools using client execution configuration, runs them, appends results, invokes the adapter again, and merges usage under `maxAgentDepth` ([loop](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L140), [classification](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L274), [continuation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L364)). Prior mock usage probe observed 12 + 23 = 35, with paired IDs.
- **CLIProxyAPI:** Its [`host.model.execute`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407) bridge enables a plugin-owned loop; prior nested-call probes establish the primitive, not a general tool coordinator.

**Integration / gaps.** **EXTENSION:** CPA needs dispatch, ownership, pairing, budgets and replay. CCR and Bifrost Supported establish a nonstream mechanism, not lossless mixed handoff or durable replay; see R24. Bifrost's prior cancellation probe found an already-canceled context still dispatched mock callbacks; downstream cancellation behavior differs.

**ProxyKit recommendation (proposed).** Put a small coordinator in an optional AI runtime, with tools supplied by feature extensions and HTTP lifecycle in transport core. After each completed response, classify calls, execute owned calls, append ID-paired results, and continue within explicit round, time and output budgets; attribute every model attempt. Stop for client calls. Neither exemplar establishes the complete mixed/replay contract.

**Pattern choice.** Borrow the classify → execute → append → continue structure shared by CCR and Bifrost. Retain explicit per-call records and shared dispatch; avoid copying separate duplicated loops for each protocol or Bifrost's name-keyed mixed-result summary.

**Acceptance example (not executed).** An internal lookup triggers one continuation with the original call ID and its result, and the final answer records usage from both model calls.

<a id="r23"></a>

## R23 — Implement streaming model/tool continuation

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Optimistic virtual-model streaming relays a turn, collects and parses its completed payload, partitions calls, executes internal tools, appends results, and builds the next request (G/gateway/handler.ts:4497–4549,4631–4668; G source and manifest/tarball provenance as in R22). Prior mock tests observed a two-call SSE continuation. The path still has ordinary-hook, identity, and replay gaps; its existing optimistic mechanism does not establish complete integration.
- **CLIProxyAPI:** The [ABI](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginabi/types.go#L135) exposes `host.model.execute_stream`, `host.model.stream_read`, `host.model.stream_close`, `host.stream.emit`, and `host.stream.close`. The [model stream callback](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_model_stream_callbacks.go#L11) returns a stream ID and reads chunks; callback cleanup manages stream lifetime. The output bridge has a bounded internal queue of 16 chunks despite unbuffered channels ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/stream_bridge.go#L94)). Prior nested delivery tests are not end-to-end mixed-tool coverage.
- **Bifrost:** Nonstream Chat/Responses enter the agent loop; streaming siblings are separate entry points ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L1063)); documented Agent Mode excludes `chat_stream` and `responses_stream` ([documentation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/docs/mcp/agent-mode.mdx#L17)).

**Integration / gaps.** No complete streaming exemplar was established. **EXTENSION / CORE/UNVERIFIED:** a coordinator or separately supervised service can own client output and consume model streams, but must implement framing, cancellation, bounded buffering, usage and terminal consistency. CPA's stock callbacks do not establish full protocol and terminal control; Bifrost's stock agent hook is not a composable streaming loop. CCR's status appropriately reflects its remaining gaps, not absence of a loop.

**ProxyKit recommendation (proposed).** Optional AI runtime coordinates the same ownership state machine as R22; transport core supplies bounded backpressure, cancellation and commitment semantics. Buffer each tool item until its ID and arguments are complete; emit only protocol-valid client-visible events, reconcile final objects with emitted deltas, and never retry after visible output without a safe continuation contract. Feature extensions implement tools.

**Acceptance example (not executed).** A streamed internal call completes, executes once, and continues; client-visible deltas and final object agree, while client cancellation stops pending work.

<a id="r24"></a>

## R24 — Preserve mixed client/internal calls and IDs

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** For mixed turns, the virtual loop stops for client or unknown calls; by default it strips and skips internal calls. With `foldInternalResults`, it executes eligible internal calls and exposes results as assistant text (G/gateway/handler.ts:3225–3276; G/types.ts:891–901, provenance as in R22). Prior SSE probes confirmed both behaviors and missing hidden results on next client replay.
- **CLIProxyAPI:** Nested model calls and stream callbacks are available ([ABI](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginabi/types.go#L135)), but no shared mixed-tool ownership or replay coordinator was established in the audited public seams.
- **Bifrost:** `executeAgent` executes auto tools then stops when non-auto tools remain ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L293), [handoff](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L378)). The Chat and Responses adapters synthesize a text summary keyed by tool name ([Chat](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agentadaptors.go#L371), [Responses](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agentadaptors.go#L588)). Prior probes retained the pending client ID but lost one of two same-name results in both formats. This is a concrete limit on the partial mixed-handoff claim, not a lossless implementation.

**Integration / gaps.** No mature lossless mixed handoff was established. **EXTENSION / SDK-HOST / CORE/UNVERIFIED:** an independent coordinator can retain per-call records and own output; an integration using an existing hook must intervene before lossy synthesis. Replay identity, storage and supported client/protocol shapes need explicit validation; a host guarantee would require a shared contract, not merely access to model calls.

**ProxyKit recommendation (proposed).** Optional AI runtime owns ordered call-ID/result records, executes only gateway-owned calls, returns unresolved client calls in a protocol-valid handoff, and resumes only after matching client results. A feature extension may provide durable state; scope it to authenticated conversation identity and reconcile subsequent replay before reconstructing private history. Unknown identity must not merge sessions.

**Acceptance example (not executed).** Two internal calls with the same name and distinct IDs plus one client call retain both internal results; the next client request supplies the pending ID and the resumed model input pairs all three correctly.

<a id="r25"></a>

## R25 — Bind trusted caller/session to tool execution

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** `executeInternalVirtualToolCalls` passes `args`, multimodal metadata and a synthetic `sessionId: virtual-model:<profile>` to `toolProvider.execute`; it supplies neither the authenticated caller nor a real conversation identity (`G/gateway/handler.ts:3578–3643`). `McpAgentToolProvider.execute` forwards arguments/metadata, not that session object, to `client.callTool` (`G/agent/tools.ts:132–164`). Thus a profile shared by callers is not a tenant boundary.
- **CLIProxyAPI:** `requestCallerScope` hashes the authenticated `userApiKey`; interceptors expose it as `RequestInterceptRequest.CallerScope`. `HostModelExecutionRequest.CallerScope` explicitly carries that scope into nested execution, and `modelExecutionRequestFromPlugin` copies it into host execution ([scope extraction][cpa-scope], [API][cpa-caller-api], [forwarding][cpa-model]). This identifies a caller scope, not a conversation. `session.Enrich` also supports derived identity; derived history/affinity must not become authorization ([identity][cpa-identity]). There is no corresponding general tool-invocation context contract in the inspected API.
- **Bifrost:** SDK `RegisterMCPTool(..., handler func(args any) (string,error), ...)` reaches a wrapper that discards its incoming context before calling `toolFunction(args)` ([SDK][bf-register], [wrapper][bf-wrapper]). However, remote MCP already supports per-request `BifrostContextKeyMCPExtraHeaders`, filtered by `AllowedExtraHeaders` and forwarded by HTTP `headerFunc` ([transport][bf-headers], [filter][bf-filter]); per-user credential resolution uses `MCPAuthMode`/`MCPIdentity` ([resolver][bf-user]). **Evidence qualification:** identity propagation is not universally absent; trusted conversation binding remains unestablished. Retain the original Integration-needed label.

**Integration / gaps.** **EXTENSION:** own the identity/session ledger and authorize every archive/tool resource. **CONFIG + EXTENSION:** Bifrost remote MCP can carry a gateway-authenticated invocation handle through allowlisted headers; strip/replace caller-supplied values. **SDK-HOST:** custom Bifrost hosting can arrange registration, but stock `.so Init(config)` receives no Bifrost instance. **CORE/UNVERIFIED:** CCR's built-in virtual path needs identity plumbing or replacement orchestration; CPA needs explicit tool dispatch binding. A separate listener alone supplies none of these guarantees.

**ProxyKit recommendation (proposed).** Transport core supplies authenticated request identity; optional AI runtime passes immutable `{principal, conversation, request, toolCall}` context separately from model arguments. Feature extensions own resource ACLs. For a companion service, mint a short-lived opaque invocation handle bound to those fields and allowed operations. Validate conversation ownership; reject unknown identity instead of guessing from prompt similarity. No mature complete cross-router implementation was established.

**Acceptance example (not executed).** Two callers use the same profile and conversation string; a tool invocation from one cannot read the other's archive, even with a copied model-visible document ID.

<a id="r26"></a>

## R26 — Cancellation through every internal tool

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** `createClientDisconnectSignal` aborts on request abort or premature response close (`G/gateway/client-disconnect.ts:5–59`); virtual streaming checks this signal between turns (`G/gateway/handler.ts:4498–4516`). But `executeInternalVirtualToolCalls` and `streamInternalVirtualToolCalls` omit it (`G/gateway/handler.ts:3578–3643,5467–5488`). Even when another caller supplies `AgentToolExecutionInput.signal`, canonical MCP execution merely races `client.callTool(...)` against abort: `callTool` has no signal parameter, and `raceWithAbort` rejects the waiting promise without canceling remote work (`G/agent/tools.ts:100–103,161–164,3114–3139`). Passing the missing signal alone is therefore insufficient.
- **CLIProxyAPI:** `host.model.execute` resolves `HostCallbackID` to the originating context before model execution ([callback][cpa-model]); host APIs expose `host.http.cancel`, stream close and model stream close ([ABI][cpa-host-api]). `guardedPluginClient.Call` returns on `ctx.Done()` while its goroutine continues `inner.Call`; the native loader checks cancellation before entering synchronous `C.cliproxy_call_plugin`, without preempting that function ([guard][cpa-guard], [native call][cpa-native]). Prior audit tests observed canceled callers with still-blocked inner calls; this is prior evidence, not a new test.
- **Bifrost:** `executeToolInternal` creates a caller-derived timeout context, using per-client timeout or global default; `CallToolWithin` bounds waiting and drops late results but cannot stop an ignoring in-process handler ([timeout][bf-timeout], [wait wrapper][bf-wait]). Local registered callbacks lack context ([wrapper][bf-wrapper]). `executeAgent` dispatches tool goroutines and calls the continuation without its own cancellation admission check ([dispatch][bf-fanout], [continuation][bf-continuation]). Prior canceled-context probes established that loop boundary; downstream executors may independently reject canceled work.

**Integration / gaps.** **EXTENSION:** propagate cancellation into owned HTTP/DB/tool operations and check before each queued dispatch/continuation. **CORE/UNVERIFIED:** expose disconnect/deadline through every retained orchestration path; fix omitted or args-only context where reused. **SDK-HOST:** a custom host can supply contextual execution instead of Bifrost's args-only callback. Timed-out waiting is not stopped execution, and completed external side effects cannot be rolled back. No complete end-to-end exemplar was established; existing labels remain accurate.

**ProxyKit recommendation (proposed).** Transport core owns disconnect/deadline; optional AI runtime derives one cancellation tree for upstream calls, tools and continuations. Tool execution receives context/signal, checks cancellation before admission and side effects, and forwards it to dependencies. Retain concurrency slots until actual execution exits; discard late results and prevent subsequent rounds. Feature extensions own cooperative cleanup; independently killable processes are an optional deployment choice for noncooperative work, not an in-process guarantee.

**Acceptance example (not executed).** Disconnect while one cooperative tool runs and another waits: the active tool observes cancellation, the queued tool never starts, and no model continuation is sent.

<a id="r27"></a>

## R27 — Bound tool turns, spend and parallel work

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** the loader dynamically imports a Node module and invokes `createGatewayPlugin({config,plugin})`; trusted extension code can own counters, semaphores, workers or companion-service admission (`G/plugins/loader.ts:570–598`). The built-in virtual profile exposes `execution.maxTurns`/`maxToolCalls`, defaulting to 6/8 (`G/types.ts:884–887; G/config.ts:4911–4929`). Its loop counts calls before execution and stops above the limit; `executeInternalVirtualToolCalls` awaits calls serially (`G/gateway/handler.ts:2863,3284–3305,3589–3600`). Optimistic streaming has corresponding checks (`G/gateway/handler.ts:4498,4634–4652`). These are per-loop controls, not a global tool-concurrency or spend budget.
- **CLIProxyAPI:** the loader invokes native `cliproxy_plugin_init` with host callbacks and retains the trusted library in-process ([loader][cpa-loader]). That runtime permits extension-owned worker pools or an authenticated companion. `host.model.execute`/stream open/read/close enable an owned loop ([ABI][cpa-host-api], [execution][cpa-model]). The generic guarded call starts a goroutine per invocation without admission limits ([guard][cpa-guard]); no shared rounds/spend budget is established there.
- **Bifrost:** `plugin.Open` loads trusted Go code; optional `Init(config any)` and required `Cleanup()` allow an owned service/worker lifecycle ([loader][bf-loader]). This does not inject SDK `RegisterMCPTool` or a Bifrost instance. Built-in `executeAgent` uses `maxAgentDepth` but starts one goroutine per auto-executable call and waits for all ([depth][bf-depth], [fan-out][bf-fanout]); prior audit probes observed 64 simultaneous mock callbacks. Depth and tool timeouts do not establish bounded parallel admission or spend.

**Integration / gaps.** **EXTENSION:** implement budgets in the owning orchestrator/service; no host database, scheduler or dashboard is required. **CONFIG:** CCR's existing caps help when reusing its loop. Bifrost admission must occur before fan-out, or use an owned loop; a semaphore inside each callback bounds active work but leaves goroutine/queue admission unbounded. R26 still governs cancellation. All original Extension-owned labels remain feasibility claims; no mature implementation covering every requested budget was established.

**ProxyKit recommendation (proposed).** Feature extension owns limits/policy; optional AI runtime offers one small budget object shared across nested calls and retries: remaining rounds/calls, deadline, token/output allowance, spending reservation and bounded queue/semaphore. Acquire before spawning; reject full queues; settle reservations from authentic usage and tool charges. Missing usage remains unknown. A strict monetary ceiling requires reliable pricing and enforceable worst-case reservations; otherwise report a stop threshold, not a billing guarantee. Keep slots until actual work exits. Generic transport core supplies cancellation and transport limits only.

**Acceptance example (not executed).** With concurrency 2 and queue capacity 1, four tool calls admit two, queue one and reject one; reaching the configured round or reservable-spend limit prevents another upstream call.

[cpa-scope]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers.go#L385-L404
[cpa-caller-api]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L793-L796
[cpa-identity]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/session/identity.go#L210-L250
[cpa-model]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407-L451
[cpa-host-api]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginabi/types.go#L129-L140
[cpa-guard]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/client_guard.go#L80-L114
[cpa-native]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L248-L282
[cpa-loader]: https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L188-L238
[bf-register]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L4635-L4640
[bf-wrapper]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/clientmanager.go#L3030-L3038
[bf-headers]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/clientmanager.go#L3998-L4007
[bf-filter]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/utils/utils.go#L165-L192
[bf-user]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/credstore/per_user_headers.go#L25-L70
[bf-timeout]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/toolmanager.go#L897-L944
[bf-wait]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/toolmanager.go#L1020-L1044
[bf-depth]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L140-L178
[bf-fanout]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L293-L344
[bf-continuation]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L395-L416
[bf-loader]: https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59-L107

<a id="r28"></a>

## R28 — Independent request/response/usage extension

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** The shipped gateway defines `GatewayPluginRequestHook` (`beforeAuth`, `beforeRouting`, `beforePrecheck`, `afterPrecheck`), `GatewayPluginResponseHook.transformResponse`, and `GatewayPluginStreamHook.transformResponse` (G/types.ts:1529–1554, 1563–1583). The response runner applies matching hooks in sequence (G/plugins/hooks.ts:319–350). Separately, startup supplies registered billing publishers, outboxes, and event hooks to the billing publisher, which runs event hooks then fans out to sinks (G/index.ts:355–360; G/billing/publisher.ts:198–249). G denotes recovered source-map files in [manifest](research/2026-11-extension-audit/ccr-manifest.json), from the exact [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).
- **CLIProxyAPI:** `RequestInterceptor.InterceptRequestBeforeAuth/AfterAuth`, `ResponseInterceptor.InterceptResponse`, `StreamChunkInterceptor.InterceptStreamChunk`, and separate `UsagePlugin.HandleUsage(context.Context, UsageRecord)` expose independent roles; the adapter dispatches usage to each plugin ([contracts](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1192), [usage contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1486), [dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_usage_translation.go#L536)).
- **Bifrost:** `HTTPTransportPlugin` hooks inspect HTTP and `LLMPlugin.PreLLMHook/PostLLMHook` inspect typed model traffic; `ObservabilityPlugin.Inject(context.Context, *Trace) error` independently receives completed traces asynchronously. Retention requires copying pooled trace data ([HTTP](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L295), [LLM](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L408), [Inject](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L574)).

**Integration / gaps.** **EXTENSION:** a recorder supplies persistence and redaction. **SDK-HOST/UNVERIFIED:** CCR virtual loops bypass some ordinary final hooks; CPA observers in mutation chains see preceding edits; Bifrost export can drop saturated traces ([export](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/tracing/tracer.go#L1400)). Supported describes seams, not complete durable transcripts or universal hook coverage.

**ProxyKit recommendation (proposed).** Transport core exposes named request/response observations; optional AI runtime emits separate usage/attempt observations; a feature extension owns filtering, export and storage. Follow CPA's separate subscriber and Bifrost's explicit trace lifecycle, with clear snapshot ownership, stage, limits and loss reporting; do not make recording depend on a mutator's position.

**Pattern choice.** Use CPA's separate usage subscriber and Bifrost's isolated bounded exporter fan-out for optional telemetry. Name observations by stage so recorder ordering does not determine whether it sees original or rewritten content; required audit delivery has a separate contract in R33.

**Acceptance example (not executed).** Enable a recorder and a response rewriter together; the recorder receives a labeled upstream usage observation and the client receives the rewritten response.

<a id="r29"></a>

## R29 — Actual usage retained before presentation edits

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Prior mock tests observed 18 upstream tokens versus 1,998 displayed, but billing hooks run before all publishers/outboxes and may drop or transform the event (G/billing/publisher.ts:209–249, 453–503; recovered-source [manifest](research/2026-11-extension-audit/ccr-manifest.json), [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)).
- **CLIProxyAPI:** The OpenAI-compatible executor parses usage from its retained upstream body for publication, and observes upstream stream frames before translating them ([code](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/openai_compat_executor.go#L239), [stream](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/openai_compat_executor.go#L561)). Prior tests observed 10 actual versus 1,998 displayed; missing usage was reported as zero, without a presence bit.
- **Bifrost:** The retry executor populates trace response attributes from the provider result before downstream post hooks ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L7774)); trace export is best effort, with saturated consumers skipped ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/tracing/tracer.go#L1400)).

**Integration / gaps.** No router establishes a protected, durable, independent original-usage ledger for every provider, translator, failure and stream. **SDK-HOST:** capture before presentation mutation, retain presence/unknown and identify the attempt. **EXTENSION:** persist and export; its crash/delivery policy must be explicit. CPA's measured behavior is route-specific; CCR event hooks can alter or drop accounting before any sink. Bifrost traces alone do not guarantee persistence.

**ProxyKit recommendation (proposed).** Optional AI runtime captures a detached provider-reported usage snapshot per attempt before presentation edits, with explicit unknown fields and provider/attempt identity. A feature extension receives this distinct accounting record and chooses storage/delivery semantics; transport core supplies lifecycle, not token interpretation. Separate presented values from provider values and do not infer zero from absence. No mature universal ledger was established in the audit.

**Acceptance example (not executed).** An upstream response reports 10 tokens, a presentation extension changes the displayed total to 1,998, and the stored attempt record still reports 10; missing provider usage is recorded as unknown.

<a id="r30"></a>

## R30 — Account for internal model calls / attempts

**Status:** CCR: Integration needed; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** Prior synthetic virtual-tool loop made two upstream calls and reported aggregate usage 36; failed-provider coverage remains untested (G/gateway/handler.ts:2590–3335, 4490–4715; [manifest](research/2026-11-extension-audit/ccr-manifest.json), [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz)).
- **CLIProxyAPI:** `host.model.execute` resolves callback context and invokes the normal `ExecuteModel` path, skipping the originating plugin; its non-stream API rejects `stream=true` ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407)). Each executor `NewUsageReporter` creates an execution UUID and takes trace, selection and auth metadata from context ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/helps/usage_helpers.go#L92)); `UsagePlugin.HandleUsage` receives records ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1486)). Prior mock tests observed two usage records for a nested model call and separate failure/success records for 429 failover.
- **Bifrost:** Core creates a model-call span and `retry.attempt.N` spans around attempts, populating provider response/error attributes ([attempts](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L7594), [attributes](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L7774)). Agent mode preserves the original request ID, optionally generates new IDs for continuation calls, and merges usage into the final response ([initialization](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L160), [continuation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L401)). Prior synthetic tests saw 12 + 23 = 35 and traced exhausted fallback attempts.

**Integration / gaps.** **EXTENSION/UNVERIFIED:** CPA generic `host.http` and direct network calls do not automatically publish model usage; exact parent-attempt linkage and unknown usage are not assured. Bifrost trace delivery can drop, and its aggregate must not be summed again with constituent calls. Supported applies to observed host-managed paths, not every provider or durable billing.

**ProxyKit recommendation (proposed).** Optional AI runtime routes each managed continuation and retry through one attempt recorder: assign exchange, parent call and unique attempt IDs; emit outcome and provider-reported usage or unknown. Feature extensions persist records; separately label aggregates for presentation and never add them to constituent attempts. External service calls require explicit reporting; don't imply coverage of arbitrary sockets.

**Pattern choice.** Combine CPA's nested calls through the common executor with Bifrost's explicit retry spans. Record leaf attempts and derive aggregates; a second billing pipeline for internal tools would invite omissions and double counting.

**Acceptance example (not executed).** A first attempt fails without usage, a retry reports 12 tokens, and an internal continuation reports 23: the recorder exposes three distinct attempts, unknown for the failure, and an aggregate of 35 without double counting.

<a id="r31"></a>

## R31 — Reliable lifecycle / attempt correlation

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** The shipped gateway's `BillingQueueEvent` carries `eventId`, `requestId`, optional upstream attempt sequence, fallback and trace fields (G/billing/publisher.ts:28–70). These are useful join primitives, but do not establish a complete cross-path relation. G references are recovered from the [1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), with source-map hashes in [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** `newRequestLifecycleTracker` generates a lifecycle UUID and takes a trace ID from context; `UsageReporter` independently generates its own UUID and uses context trace/selection IDs ([lifecycle](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_interceptors.go#L119), [reporter](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/helps/usage_helpers.go#L126)). Prior tests found different IDs even for one attempt and shared trace IDs for nested calls.
- **Bifrost:** The retry executor makes LLM and `retry.attempt.N` spans and records response attributes; the MCP loop retains the original request ID while accumulating usage ([attempts](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L7594), [agent](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/mcp/agent.go#L160)). `ObservabilityPlugin.Inject(ctx, *Trace)` requires copying retained data because traces are pooled ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L574)).

**Integration / gaps.** **SDK-HOST / CORE/UNVERIFIED:** No audited router establishes universal request → execution → attempt joins across all paths. CPA's trace groups nested calls and is not a unique join key; matching independent UUIDs or timestamps is unsound. Prior Bifrost tests cover selected retries and continuations, not universal correlation.

**ProxyKit recommendation (proposed).** Transport core assigns a request ID; optional AI runtime assigns execution IDs with parent IDs and distinct attempt IDs, propagating them to immutable observations. A feature extension persists them. Count attempt records separately from aggregate final usage. No mature complete implementation was established.

**Acceptance example (not executed).** A request with one nested execution and one retry yields one root, two executions and three uniquely parented attempts, without joining on timing.

<a id="r32"></a>

## R32 — Distinguish missing usage from measured zero

**Status:** CCR: Unknown; CLIProxyAPI: Integration needed; Bifrost: Unknown.

**Implementation evidence.**

- **CCR:** `calculateUsageBilling` normalizes usage token fields before billing (G/billing/calculate.ts:58–69); whether every original provider payload is observable before normalization is unverified. G is recovered from the [1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz); see the source-map [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** `ParseOpenAIUsage` gets the raw `usage` node but discards the parser's presence boolean and returns `usage.Detail`; its primary token counters are non-null integers ([parser](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/runtime/executor/helps/usage_helpers.go#L1709), [detail](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/usage/manager.go#L101)). A prior test observed omitted usage delivered as all-zero counters and `Failed:false`.
- **Bifrost:** The typed Responses object has an optional `*ResponsesResponseUsage` ([schema](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/responses.go#L194)); retry tracing populates attributes from a response or error ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L7774)). Presence in this type is a primitive, not evidence of preservation across every adapter and trace path.

**Integration / gaps.** **EXTENSION / SDK-HOST / CORE/UNVERIFIED:** An observer that receives the original payload can record presence before normalization, then correlate it with the attempt; CPA's normalized usage callback alone cannot recover it. Coverage and correlation need verification or host changes where earlier access is unavailable. CPA's integration label describes this possible path, not a guarantee.

**ProxyKit recommendation (proposed).** The optional AI runtime records per-attempt usage as absent/unknown or present with values, including measured zero; adapters preserve field presence before defaults. The feature extension stores and reports it, without inferring zero from failures or absence. No mature cross-provider implementation was established.

**Acceptance example (not executed).** Two otherwise identical upstream responses, one omitting usage and one reporting zero tokens, produce distinct unknown and measured-zero records.

<a id="r33"></a>

## R33 — Persist telemetry after capture / avoid pre-capture loss

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** `publishBillingEventInternal` applies event hooks first, then invokes webhook, `outbox.append(event, context)` and publisher delivery; a hook can return `false` and drop an event (G/billing/publisher.ts:198–245, 439–443, 453–503). These are delivery seams, not an immutable capture guarantee. G source-map provenance: [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** `Manager.Publish` appends to a process-memory slice, and its worker dispatches sequentially to plugins; `NewManager(buffer)` does not use the argument ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/usage/manager.go#L648), [enqueue and dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/usage/manager.go#L720)). A crash before callback can lose records; a slow callback delays others.
- **Bifrost:** `ObservabilityPlugin.Inject` receives completed traces asynchronously, and must copy anything retained ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L560)). Tracer fan-out uses per-plugin nonblocking semaphore acquisition; saturation increments a drop counter and skips that callback ([implementation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/tracing/tracer.go#L1400)).

**Integration / gaps.** **EXTENSION:** A database or outbox can persist received events and retry export. **SDK-HOST / CORE:** It cannot restore events dropped, transformed, or lost before capture; stronger guarantees require an earlier, complete capture point and a specified durability/acknowledgment boundary. This is a specific delivery gap, not a requirement for host-owned storage or jobs.

**ProxyKit recommendation (proposed).** Feature extension owns durable storage and retries; optional AI runtime exposes original per-attempt observations with stable IDs. For deployments requiring crash durability, define an acknowledged durable handoff before claiming capture and explicit behavior on failure; best-effort telemetry can use a bounded queue with visible drop counts. No audited end-to-end lossless implementation was established.

**Acceptance example (not executed).** After an acknowledged durable record, restart the collector before export; the same event ID is retried and stored once at the destination.

<a id="r34"></a>

## R34 — Explicit policy denial before upstream

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR + gateway:** `GatewayPluginRequestHook.beforeAuth(input)` returns `{ok:false, status?, error, details?}`. The core executes registered matching hooks in order and stops on failure; the HTTP pre-auth handler responds with the requested status or 403 before routing (`G/types.ts:1429,1529`, `G/plugins/hooks.ts:54`, `G/gateway/routes.ts:602`). These G paths are recovered from the [1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz); the [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json) records artifacts and recovered files.
- **CLIProxyAPI:** `RequestInterceptor.InterceptRequestBeforeAuth` can successfully return `Terminate`, `StatusCode`, `ResponseHeaders` and `ResponseBody`. The host stops iterating; the handler turns termination into an error before execution ([adapter](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L112), [termination](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L198), [handler](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_interceptors.go#L493)).
- **Bifrost:** `PreLLMHook` returns an `LLMPluginShortCircuit{Error: ...}`; the pipeline stops later pre-hooks ([runner](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9818)). Set `BifrostError.AllowFallbacks` to false: nil permits fallback ([fallback check](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L5852)). HTTP `HTTPTransportPreHook` can return a response or error and the middleware returns before the handler ([implementation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/handlers/middlewares.go#L905)).

**Integration / gaps.** CONFIG / EXTENSION: configure the intended route coverage and use the actual denial return, not an unexpected exception. Bifrost post-hooks can recover an error, so policy ordering also matters. Prior probes observed denial before upstream; that does not prove coverage of every path.

**ProxyKit recommendation (proposed).** Transport core owns an explicit pre-upstream policy decision: allow or deny with status and safe response; feature extensions supply the decision. Denial must stop routing and retries/fallbacks for that operation. This combines the direct CCR/CPA decision with Bifrost's explicit fallback control.

**Pattern choice.** Use an explicit deny/respond result like CPA termination or Bifrost short-circuiting, with a terminal policy-denial type that cannot enter ordinary provider fallback. CCR's staged pre-upstream gates show where to invoke it.

**Acceptance example (not executed).** A policy denies a request with 403; a counting upstream receives zero requests even with a fallback configured.

<a id="r35"></a>

## R35 — Fail-closed mandatory policy on unexpected error

**Status:** CCR: Integration needed; CLIProxyAPI: Host gap; Bifrost: Host gap.

**Implementation evidence.**

- **CCR:** Core protected execution defaults `failureMode` to `fail_closed`, and its request-hook runner propagates failed executions (`G/plugins/execution.ts:49,93`, `G/plugins/hooks.ts:77`; G provenance: [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz) and [manifest](research/2026-11-extension-audit/ccr-manifest.json)). But CCR's outer request transforms catch failures and continue ([source](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L350)); startup can disable a failed plugin ([source](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L259)).
- **CLIProxyAPI:** Request interceptor errors and recovered panics return no successful interception; the chain continues ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L22)). Successful explicit termination is a separate path, as in R34.
- **Bifrost:** `RunLLMPreHooks` records returned Go errors and continues; a short circuit is distinct ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9843)). HTTP pre-hook errors instead return 500 before the handler ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/handlers/middlewares.go#L910)).

**Integration / gaps.** CCR: CONFIG / SDK-HOST must ensure the required hook is loaded, matched and used on every protected path; core default alone does not ensure availability. CLIProxyAPI: CORE/UNVERIFIED generic interceptor has no established required-role override; handling anticipated errors inside the plugin cannot cover plugin absence or crash. Bifrost: CORE/UNVERIFIED no generic mandatory LLM-role override established; HTTP error behavior is a narrower usable boundary, not a universal guarantee. These labels describe audited host guarantees, not effort or impossibility. Prior probes tested selected error behaviors only.

**ProxyKit recommendation (proposed).** Transport core owns a configured required-policy binding and rejects startup or protected requests when it is unavailable. For its invocation, exceptions, invalid decisions, deadlines and overload deny before upstream; extensions own policy logic. Define behavior across retries and alternate paths, and never let later hooks turn a mandatory denial into success. No complete mature cross-path implementation was established.

**Acceptance example (not executed).** A required policy throws unexpectedly; the request fails and neither primary nor fallback upstream sees traffic.

<a id="r36"></a>

## R36 — Bound extension-owned workers / host hook execution

**Status:** CCR: Supported; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR + gateway:** `GatewayPluginExecutionConfig` exposes optional `timeoutMs`, `concurrency`, `maxQueueSize`, `failureThreshold`, `cooldownMs` and `failureMode` (`G/types.ts:1300`). `runGatewayPluginProtectedOperation` wraps each keyed hook with a limiter and timeout; queue-full rejects, and a timed-out active operation retains its slot until the underlying promise settles (`G/plugins/execution.ts:49,164,200,233`). Timeout is `Promise.race`, not forced termination: blocking synchronous JavaScript may also prevent the timer from firing. The feature extension can separately bound its own service/workers. G provenance: [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz) and [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json).

**Integration / gaps.** CCR's Supported label establishes configurable admission and waiting limits, not a hard CPU limit or termination of arbitrary trusted code; unset limits are not bounds. **CLIProxyAPI — EXTENSION / SDK-HOST:** an extension can bound its own worker pool, but generic `guardedPluginClient.Call` launches a goroutine and caller cancellation does not terminate the inner call ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/client_guard.go#L80)); the prior 24-call cancellation probe confirmed that boundary. **Bifrost — EXTENSION / SDK-HOST:** own admission limits are feasible; provider buffer/concurrency limits ([defaults](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/provider.go#L18)) do not establish a generic synchronous hook execution bound ([runner](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9843)). A separately supervised service can enforce its own resources; native callbacks still execute under their host's rules.

**ProxyKit recommendation (proposed).** Transport core or optional AI runtime owns bounded per-role admission, finite queue and deadlines where hooks are introduced; feature extensions own their worker pools and service limits. Keep a permit until work actually ends, reject when full, and propagate cancellation. Describe deadlines as cooperative; hard termination requires an independently isolated worker, only when a concrete need warrants it. CCR's limiter is the closest observed pattern, not a complete resource sandbox.

**Pattern choice.** Borrow CCR's protected-operation runner: bound admission and keep a timed-out operation's concurrency slot occupied until it actually finishes. Start with deadline, semaphore and a small bounded queue; defer circuit breakers unless operational evidence warrants them.

**Acceptance example (not executed).** With one permit and no queue, a stalled hook causes a second request to be rejected; a timed-out first invocation retains its permit until it actually exits.

<a id="r37"></a>

## R37 — Deterministic extension order

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** Packaged gateway loads enabled `config.plugins` sequentially, registering each module's `requestHooks`, `requestTransforms`, `routeResolvers`, `responseHooks` and `streamHooks` in array order (G/plugins/loader.ts:145–156, 208–233). The keyed Map registry's `list()` preserves insertion order; overwriting a key replaces its value without moving its position (G/gateway/runtime.ts:55–77). `executeGatewayPluginRequestHookStage` iterates that list for `beforeAuth`, `beforeRouting`, `beforePrecheck`, and `afterPrecheck` (G/plugins/hooks.ts:33–37, 54–84). G denotes recovered sources from [the exact npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz); [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json). This is stage/registration ordering, not a dependency graph; prior audit found virtual-loop paths bypass some ordinary final hooks.
- **CLIProxyAPI:** `sortRecords` orders descending priority, then ascending plugin ID; `InterceptRequestBeforeAuth` / `InterceptRequestAfterAuth` traverse active records sequentially, feeding the accumulated body and headers to the next interceptor ([sort](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/snapshot.go#L158), [dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L112)). `PickAuth` chooses only the first active scheduler; unhandled or invalid output returns control to built-in selection, not the next plugin ([scheduler](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/scheduler.go#L11)).
- **Bifrost:** `SortAndRebuildPlugins` groups `pre_builtin`, `builtin`, `post_builtin`, sorts ascending `Order`, and preserves registration order for ties ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/lib/config.go#L9075)). `PreLLMHook` runs forward; `PostLLMHook` runs backward for entered pre-hooks, including short circuits ([pipeline](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9817), [post](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L9929)). Prior audit tests covered hook order; this is not evidence for every path.

**Integration / gaps.** None establishes a general dependency DAG or cycle validation. Ordering does not imply universal hook coverage or successful execution.

**ProxyKit recommendation (proposed).** Transport core defines named stages and explicit configured order; optional AI runtime defines its semantic stages. Reject duplicate IDs and conflicting exclusive-role bindings at startup. Configuration list position defines order; document whether paired response stages reverse order. Defer dependency declarations and graph validation until a concrete composition needs them. Keep route-provider choice explicit rather than silently trying all providers.

**Pattern choice.** Prefer CCR's named stages with one explicit operator-ordered list. CPA's priority/ID order and Bifrost's placement/priority order are deterministic, but neither hierarchy is necessary for the first ProxyKit composition model. Define any paired cleanup order explicitly.

**Acceptance example (not executed).** Two configured transforms append `A`, then `B`; the observed upstream value is `AB` on repeated requests, and duplicate extension IDs fail startup.

<a id="r38"></a>

## R38 — Coordinate cooperating extensions / compactor choice

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** Trusted JavaScript module factory `createGatewayPlugin` or default export receives `{config, plugin}` and returns a plugin object; it can compose cooperating functions behind registered request hooks/transforms (G/plugins/loader.ts:574–595, 208–221; [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json)). It can also communicate with its own separately supervised service; these are author-owned mechanisms, not built-in compactor arbitration.
- **CLIProxyAPI:** A native plugin registers capabilities through the C ABI/JSON `plugin.register` contract and can expose a single request interceptor that coordinates its own components ([ABI](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginabi/types.go#L5), [interceptor dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L132)). Trusted native code can instead call a separately managed coordinator service.
- **Bifrost:** A Go shared object may export `Init(config any) error`, required `GetName` and `Cleanup`, and optional hooks; a hook can delegate to its own composed components or service ([loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59), [hook interface](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L412)). SDK-only capabilities should not be assumed available through the `.so` initializer.

**Integration / gaps.** **EXTENSION + CONFIG:** the coordinator selects exactly one compactor and invokes cooperating components in its chosen order; operator disables competing independent compaction paths. The original labels describe feasibility, not an existing complete implementation: none automatically arbitrates unrelated, unaware plugins. Cross-process state, durability and failure policy remain coordinator work; no host DAG is necessary for this arrangement.

**ProxyKit recommendation (proposed).** A feature extension owns compaction and its domain-specific dependencies; optional AI runtime supplies supported semantic transformation stages. Configure one authoritative compactor per route/session and reject conflicting declarations where visible. A coordinator may choose internally among strategies; do not put compaction algorithms in generic transport. No mature automatic cross-plugin arbitration was established.

**Acceptance example (not executed).** Enabling two authoritative compactors on one route fails validation; selecting one causes only that compactor to modify the upstream working context.

<a id="r39"></a>

## R39 — Keep configuration stable for active work

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Implementation evidence.**

- **CCR:** Module synchronization loads and initializes enabled modules before a swap, while runtime component registries are mutable and hooks are listed at individual request and response call sites (G/plugins/loader.ts:130–160; G/gateway/runtime.ts:55–77; G/gateway/handler.ts:388, 8506–8509). [Tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json). These reads do not establish a single request-wide snapshot.
- **CLIProxyAPI:** `activeRecords()` starts from the current `Snapshot()` and filters stale identities; individual request and response dispatches fetch active records independently ([snapshot](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/snapshot.go#L55), [dispatch](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L132)). Atomic publication of an individual snapshot is not a guarantee that one request sees only that version.
- **Bifrost:** `SortAndRebuildPlugins` copies and atomically stores a sorted base list, then rebuilds interface caches ([source](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/transports/bifrost-http/lib/config.go#L9075)). This alone does not establish atomic, lifetime-safe replacement of every plugin/config object for a whole request.

**Integration / gaps.** **EXTENSION + CONFIG:** capture immutable extension-owned settings at work start; disable live replacement or stop admission and drain active work before replacing the configuration. This operational approach needs actual deployment wiring, including long streams and extension-owned jobs. **SDK-HOST / CORE, UNVERIFIED:** independently hot-swapping host registries during active requests needs a request-wide lifetime guarantee; an extension cannot impose one on other plugins. No mature live-swap guarantee was established.

**ProxyKit recommendation (proposed).** Transport core pins one immutable configuration and extension-set version at admission through completion, including streaming; optional AI runtime and extensions receive that version, and old instances remain alive until all dependent work finishes. Start with restart or drain for updates; add live swapping only when required. Extension-owned jobs capture and release their own version.

**Acceptance example (not executed).** Request a configuration update during an active stream: admission drains, the stream completes with its original setting, old resources close, and requests admitted after restart use the new setting.

<a id="r40"></a>

## R40 — Own background jobs and lifecycle

**Status:** CCR: Extension-owned; CLIProxyAPI: Extension-owned; Bifrost: Extension-owned.

**Implementation evidence.**

- **CCR:** The outer CCR service collects stop hooks, runs them in reverse order, and closes owned HTTP backends/SQLite stores ([outer lifecycle](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L293)). This is distinct from the packaged core. The shipped gateway dynamically imports a configured module and calls `createGatewayPlugin` or its default factory with `{config, plugin}`. Trusted JavaScript can start its own workers, server and storage. The narrower `GatewayPluginExtension` event publisher/outbox/store roles expose optional `init`, `ready`, `health`, `flush`, `drain`, and `close`; the loader initializes those collected roles and closes them on replacement/failure. Ordinary hook-only modules do not automatically receive this lifecycle contract. G/plugins/loader.ts:135–170, 444–453, 570–598; G/plugins/events.ts:74–84, 160–188. G denotes recovered shipped source in [the source-map manifest](research/2026-11-extension-audit/ccr-manifest.json), from [@the-next-ai/ai-gateway 1.0.21](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).
- **CLIProxyAPI:** The Unix host loads a native library, resolves `cliproxy_plugin_init`, and passes the host API; the plugin can own goroutines and external resources. Detachment calls its optional ABI `shutdown` callback, but leaves the image mapped because its runtime cannot safely be unloaded ([loader](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179-L225), [shutdown](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L293-L324)).
- **Bifrost:** `plugin.Open` loads Go code; optional `Init(config any) error` can start workers, and required `Cleanup() error` releases them on shutdown ([loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L45-L107), [contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L285-L292)). Its framework Sidekiq runner defines a durable store, atomic claims, heartbeats and handler checkpoints, but is not injected by `.so` Init ([runner](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/sidekiq/sidekiq.go#L17-L46)).

**Integration / gaps.** **EXTENSION:** implement admission, persistence, retries, idempotency and graceful shutdown as needed; a separately supervised companion may instead own these. **CONFIG:** deploy and supervise that companion independently. No automatic child-process supervision or general durable job service for ordinary plugins is established. The broad CCR cleanup wording in the status detail needs the role qualification above; this does not change its Extension-owned label. No durable-job runtime test was established.

**ProxyKit recommendation (proposed).** Feature extension owns jobs and domain logic; supply a small lifecycle stop/drain contract for in-process extensions when the first use requires it. Start with an independently supervised service if durability or isolation is needed. Add shared job infrastructure only for a demonstrated common need; choose storage and runtime later.

**Acceptance example (not executed).** An extension persists a queued item, restarts, retries it with a stable idempotency key, produces one committed effect through transactional deduplication, and drains its worker on shutdown.

<a id="r41"></a>

## R41 — Sandbox untrusted extension code

**Status:** CCR: Optional; CLIProxyAPI: Optional; Bifrost: Optional.

**Implementation evidence.**

- **CCR:** The gateway imports plugin JavaScript into its runtime and passes the factory full `config` and `plugin`; manifest validation occurs after module execution. Thus registration validation is not an OS or secret sandbox. G/plugins/loader.ts:570–598; [source-map manifest](research/2026-11-extension-audit/ccr-manifest.json), [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz).
- **CLIProxyAPI:** Unix `dlopen` loads the native image into the host process, invokes its init with a host API, and calls function pointers directly; logical shutdown retains the mapped image. This is trusted native execution, not process confinement ([loader](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179-L225), [call and shutdown](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L248-L310)).
- **Bifrost:** `plugin.Open` and optional `Init(config any)` execute Go plugin code in process; `GetName` and `Cleanup` are required symbols. Nothing in that loader establishes filesystem, network or secret isolation ([loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L45-L107)).

**Integration / gaps.** **CONFIG / EXTENSION:** run a companion under separately configured OS or container permissions if desired; the in-process adapter remains trusted. **CORE/UNVERIFIED:** none of these audited boundaries establishes an untrusted-code sandbox or per-extension secret isolation. Optional describes the chosen trusted model, not a claim that sandboxing is already implemented. A mature built-in sandbox was not established.

**ProxyKit recommendation (proposed).** Treat feature extensions and in-process adapters as trusted; keep domain logic in the extension. Document that configuration declarations and credential handles alone do not restrict same-user code. If untrusted plugins become a concrete requirement, separately specify an isolated process, limited credentials, explicit IPC and OS-enforced resource/network/filesystem policy; select technology only then.

**Acceptance example (not executed).** In an independently isolated companion deployment, a denied filesystem read fails while an explicitly permitted gateway request succeeds; the test separately identifies the trusted adapter boundary.

<a id="r42"></a>

## R42 — Short-circuit cache / local response

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** The shipped gateway exposes `httpRoutes` with method, path, `priority: pre | fallback`, `auth: gateway | none`, and a Fastify request/reply handler (G/types.ts:1586–1608). Its global pre-handler resolves a matching pre route before the normal handler; `handlePluginHttpRoute` runs configured authentication and sends the handler's value or lets it send directly (G/gateway/routes.ts:99–108, 340–418). Request policy hooks instead return a structured failure; `beforeRouting` sends an error envelope, not an arbitrary cached success (G/types.ts:1429–1438, 1521–1539; G/gateway/handler.ts:387–413). These G references are recovered from the [@the-next-ai/ai-gateway 1.0.21 npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), with source-map and artifact evidence in [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CLIProxyAPI:** `RequestInterceptor.InterceptRequestBeforeAuth/AfterAuth` returns `Terminate`, `StatusCode`, `ResponseHeaders`, and `ResponseBody`; a successful termination stops the interceptor chain, and the after-auth path converts it to `RequestTerminatedError` before upstream execution ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1192), [fields](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1297), [chain](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L198), [execution](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/auth/conductor_execution.go#L342)). Prior audit observed a 403 body and no upstream request; interceptor errors themselves are logged and ignored ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L22)).
- **Bifrost:** `PreLLMHook` can return `LLMPluginShortCircuit` containing a response, stream or error; the pipeline stops and runs post-hooks for entered plugins ([API](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin_native.go#L19), [flow](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L5563)). Its semantic cache searches exact hashes first, optionally semantic matches, returns a hit, and retains state for post-hook storage on a miss ([implementation](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/plugins/semanticcache/main.go#L372), [cached response/stream](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/plugins/semanticcache/search.go#L341)). Prior HTTP transport tests covered short circuits.

**Integration / gaps.** EXTENSION: cache keys, authenticated scope, expiry, invalidation and replay correctness remain feature work. CCR's pre-route can answer an existing path, but a miss that resumes the ordinary handler is not established by this route contract; its policy denial does not provide a general success body. Thus Supported means local response, not an established transparent cache-hit/miss hook. Prior tests did not establish a complete cache across routers.

**ProxyKit recommendation (proposed).** Transport core provides a pre-forward decision: continue or respond with status, headers and bounded body/stream, after appropriate authentication; run defined response observers and preserve cancellation/backpressure. A feature extension owns scoped cache policy and storage. The CPA decision is simplest; Bifrost illustrates explicit streaming and paired observation. Semantic caching is optional.

**Pattern choice.** Use a discriminated local-response result shared by routing/policy/cache extensions. Bifrost's typed short circuit provides a useful precedent; route the result through the same final delivery boundary instead of making cache-specific transport exits.

**Acceptance example (not executed).** An authenticated cache hit returns the scoped stored response, invokes no upstream request, and a miss forwards normally.

<a id="r43"></a>

## R43 — Run a full extension application plus gateway adapter

**Status:** CCR: Supported; CLIProxyAPI: Supported; Bifrost: Supported.

**Implementation evidence.**

- **CCR:** With `trusted-code`, `loadConfiguredPlugin` loads JavaScript and calls `setup(context)` or `activate(context)`, then registers returned contributions. Context includes gateway/core plugin registration, routes, HTTP backends and SQLite ([loader](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L510), [context](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L158)). `registerHttpBackend` starts a Node HTTP server and returns its address; service shutdown invokes stop hooks and closes owned backends ([backend](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/backend-service.ts#L61), [stop](https://github.com/musistudio/claude-code-router/blob/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e/packages/core/src/plugins/service.ts#L293)). Trusted code can also use ordinary OS APIs or connect to a separately supervised application.
- **CLIProxyAPI:** The native loader opens a shared library, locates `cliproxy_plugin_init`, calls it with host and plugin C ABI tables, and validates the version and call/free functions ([loader](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/loader_unix.go#L179)). Native initialization and callbacks can run ordinary application code, listeners or workers, or communicate with a companion; capabilities such as `RequestInterceptor` supply the gateway adapter ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1192)). This requires a compatible native ABI bridge, not arbitrary direct JS loading.
- **Bifrost:** `plugin.Open` loads trusted Go code; the loader calls optional `Init(config any) error`, requires `GetName` and `Cleanup`, and discovers optional HTTP and LLM hooks ([loader](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L59)). That code can own listeners, storage and goroutines or call a companion; the Go hook is its adapter. `Init` does not inject the SDK's account registry or MCP tool registration.

**Integration / gaps.** EXTENSION and deployment: each application owns its service lifecycle, persistence, job semantics, health and supervision; a separate service requires a gateway-compatible adapter. These are source-supported runtime possibilities, not a tested turnkey deployment or a host sandbox. No new tests were run.

**ProxyKit recommendation (proposed).** Feature extensions own their domain application and may run in-process or as independently supervised services with a thin adapter. Provide a small versioned registration and shutdown contract for supported adapter roles; do not require a host dashboard, database, scheduler or enrollment API. Runtime and language remain undecided. Test the selected deployment path when chosen.

**Pattern choice.** Prefer CCR-like setup/register/dispose ergonomics if a JS/TS runtime is selected. Preserve the companion-service option demonstrated by all three trusted runtimes; avoid adopting CPA's C ABI or Bifrost's Go shared-object constraints before runtime and deployment needs justify them.

**Acceptance example (not executed).** A sample extension starts its own health listener and registers a request hook, answers the health request, modifies a proxied request, and releases its listener on shutdown.

<a id="r44"></a>

## R44 — Negotiate target capabilities and translate model-specific settings

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R02 selects routes, R09 edits requests, R13 addresses wire adapters, and R20 protects opaque state. None requires a capability decision and semantically valid translation for every field when the target changes.

**Implementation evidence.**

- **CCR:** `GatewayPluginRouteResolver.resolve` can return a model, dynamic `targetProviderConfig` or target list, and request transforms can replace the body or `standardRequest` (G/types.ts:1455–1462, 1495–1518). `SourceAdapter.toStandardRequest` and `TargetAdapter.buildRequestFromStandard` provide conversion seams; `StandardRequest` carries `reasoning`, `thinking`, tools, tool choice and output settings, several as `unknown` (G/types.ts:1192–1208, 1234–1265). The handler resolves the plugin route before upstream dispatch (G/gateway/handler.ts:416–435). These fields and adapters do not certify equivalence, target support, or context capacity. G is recovered shipped source: [npm package](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** [`ModelRouter.RouteModel`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L574) receives source format, raw body, requested model and stream flag and can target a provider and model ([contract](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L693)); request interceptors can replace the body. Custom executors can perform specialized translation. These mechanisms do not establish an authoritative capability matrix or mandatory validation across subsequent provider selection.
- **Bifrost:** [`PreRequestHook`](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L6112) may mutate provider, model, fallbacks and request fields; model catalog information is provided to context. SDK `Account` and `KeySelector` provide provider/key control ([config](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/bifrost.go#L20)). Catalog availability and provider/model validation do not establish semantic compatibility for all listed features; pre-request hook errors are logged and skipped.

**Additional implemented translation evidence.** CPA already has model-specific thinking validation, not merely raw hooks: `ValidateConfig` rejects unsupported thinking, converts effort levels to budgets or budgets to levels, and clamps selected cross-family conversions while keeping certain same-family inputs strict ([validator](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/thinking/validate.go#L38)). `applyThinkingDocCore` resolves target model metadata and the provider applier ([application](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/thinking/apply.go#L261)). CCR's Gemini adapter also maps supported effort labels and thinking budgets (G/adapters/builtins/target/gemini-generate-content.ts:394–449; same source-map provenance). These are concrete patterns to borrow, but automatic clamping is not caller-authorized semantic equivalence and neither establishes all modalities/tools/schema constraints. Prefer small target adapters with explicit adaptation records over a universal capability framework.

**Integration / gaps.** EXTENSION + CONFIG: maintain versioned, scoped target capability facts and translations; reject unsupported or unknown semantics unless downgrade is authorized. SDK-HOST / CORE, UNVERIFIED: verify policy covers retries, fallback, streams and every execution path and fails closed; ordinary hook presence alone does not ensure this. Neither equal field names nor token estimates prove equivalent reasoning or context behavior.

**ProxyKit recommendation (proposed).** Optional AI runtime passes a target-specific capability view to an extension-owned negotiator after each route choice, including fallback. Return accept, adapted request with explicit changes, or rejection; unknown is not support. Keep protocol mapping in adapters and domain catalog in the extension; generic transport stays opaque. Validate reasoning budget/effort, tools and choices, output schema, modalities, context and streaming independently, with explicit downgrade authorization and preserved opaque state.

**Acceptance example (not executed).** A replacement lacking structured output is rejected before dispatch; when a caller explicitly permits a documented adaptation, the request and audit record identify the change. A target with the same `reasoning` field but incompatible meaning is not silently accepted.

<a id="r45"></a>

## R45 — Reroute after inspecting an upstream outcome before client commitment

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R10–R11 cover response and stream mutation, R22–R27 continuations and bounds, R30 attempt accounting, and R35 required policy. None composes successful-content validation, another attempt and a client-commitment/side-effect boundary.

**Implementation evidence.**

- **CCR:** `ProviderPlugin.transformResponse` receives the upstream `Response` and parsed payload; response hooks can replace payload, status and headers (G/types.ts:1408–1426, 1556–1583). The handler records failed upstream attempts and may advance to a target after eligible failures (G/gateway/handler.ts:900–927), and response-plugin failure can enter failure handling (G/gateway/handler.ts:930–980). This is not a documented general accept/reissue decision for successful content, and ordinary hooks do not cover every virtual-model path. G: [package](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** [`InterceptResponse`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1362) sees a successful nonstream body and may replace it; stream interception works chunk by chunk, with bounded already-delivered history. A custom executor can call [`host.model.execute`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407), inspect outcomes and return a chosen result. Its nested call skips the calling plugin's interceptor and router; this is not automatic complete policy coverage. Prior audit observed nested-call accounting, not this whole workflow.
- **Bifrost:** [`tryRequest` runs post hooks](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L6500); returned errors can enter the [fallback decision](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L5839), while a successful primary does not trigger it. Pre/post hook and short-circuit primitives permit useful result inspection or replacement, but do not establish a universal content-retry, commitment and tool-side-effect contract.

**Integration / gaps.** EXTENSION: write classifiers, bounded candidate policy and safe idempotency rules. SDK-HOST / CORE, UNVERIFIED: wire a precommit result decision over supported paths, record every attempt and hold tool execution; streaming validators requiring the whole answer must buffer with limits. A 429/error retry is distinct from rejecting a successful response. Once bytes or tools are committed, transparent replacement cannot undo them.

**ProxyKit recommendation (proposed).** Generic transport owns the explicit commit boundary and bounded buffering/cancellation. Optional AI runtime invokes an extension decision on each uncommitted outcome: accept, transform and reissue, retry another target, or reject. Enforce shared attempt/time/spend limits, record actual usage including failures and unknowns, and never repeat side effects automatically. After commitment, fail or use an explicitly supported continuation protocol.

**Acceptance example (not executed).** A complete buffered response fails a schema validator; one permitted alternative succeeds and both attempts are attributed before a single response reaches the client. A later stream failure after the first visible byte does not silently restart.

<a id="r46"></a>

## R46 — Coordinate classifier calls, candidate fan-out, hedging and result selection

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R02 route selection, R22–R27 private calls and bounds, R26 cancellation, R30 accounting and R43 extension applications provide ingredients. None guarantees common dispatch, bounded parallel selection and deferred tool side effects as one workflow.

**Implementation evidence.**

- **CCR:** `routeResolvers.resolve` can asynchronously choose dynamic provider configurations and return ordered targets (G/types.ts:1495–1518); a trusted plugin may run its own application. Handler fallback tracks upstream attempts and resets request state between targets (G/gateway/handler.ts:542–557). Ordered fallback is not a parallel fan-out/selection API. A plugin making independent network calls must arrange its own attribution and lifecycle. G: [package](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** [`ModelRouter`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L574) and [`ProviderExecutor`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L749) permit a planner/executor; [`host.model.execute`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407) dispatches a nonstream nested call with context, provider and auth pin options and skips the caller's own interceptor/router. A separate streaming callback exists; its [bridge buffer](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/stream_bridge.go#L16) is 16, not a global concurrency budget. Prior research observed two accounted nested calls, not fan-out selection.
- **Bifrost:** SDK configuration accepts an `Account`, `KeySelector` and plugins ([SDK config](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/bifrost.go#L20)); pre-request hooks can set target and fallbacks, and pre-LLM hooks can short-circuit with a response or stream ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin_native.go#L19)). SDK embedding can own orchestration around calls; stock shared-object [`Init(config any)`](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L70) does not receive an SDK instance. Fallback is sequential error handling, not a fan-out guarantee.

**Integration / gaps.** EXTENSION + CONFIG: own planner, concurrency semaphore, classifier and selection policy; SDK-HOST / CORE, UNVERIFIED: establish common dispatch and attributable costs for all calls, loser cancellation, and suppression of tool effects until selection on each chosen path. Cancellation cannot guarantee an already accepted upstream request incurs no charge; record actual or unknown cost. An extension-owned service is feasible but its independent calls are not automatically host-attributed.

**ProxyKit recommendation (proposed).** Keep planning in the extension; optional AI runtime offers a scoped dispatch function returning attempt handles, outcomes and usage, with cancellation and a shared budget. Generic transport supplies deadlines and bounded flow control. Race or ensemble only within configured fan-out; hold tool calls as data until selection, then authorize execution once. No workflow engine is needed.

**Acceptance example (not executed).** A classifier selects two candidates under a fan-out limit; the first valid answer wins, the other is cancelled, both attempts and any available usage are recorded, and only the selected candidate's tool call may execute.

<a id="r47"></a>

## R47 — Preserve the client protocol contract across provider/model substitution

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R02 selects a route; R09–R11 mutate individual requests, responses and streams; R13 registers protocol adapters; R24 concerns mixed tool IDs; R29–R30 concern actual usage. None establishes the complete client-facing round trip, including errors, final objects, streaming, structured output and truthful served provenance after substitution.

**Implementation evidence.**

- **CCR:** `routeResolvers` receive the requested model and body before the target loop; each target resolves a target adapter and, for conversion, invokes `sourceAdapter.toStandardRequest`, `targetAdapter.buildRequestFromStandard`, `targetAdapter.toStandardResponse`, and `sourceAdapter.fromStandardResponse` (G/gateway/handler.ts:418–430, 644–655, 696, 783, 1549, 2025). Source formatters map finish reasons and render tool/reasoning content (G/adapters/builtins/source/formatters.ts:64, 97, 122, 193). Stream hooks run before relay (G/gateway/handler.ts:1075, 1402). These are concrete conversion paths, not an exhaustive fidelity guarantee. G denotes the recovered 1.0.21 sources from the [npm tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), mapped by the [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** `ModelRouter` and `HostModelExecutionRequest` expose entry/exit protocol, model, forced provider and optional auth ID ([API](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L576), [execution request](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L767)). The translator registry uses native or plugin transforms for request, stream and nonstream; absent a transform it can retain the original payload ([registry](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/translator/registry.go#L114), [stream](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/translator/registry.go#L263)). The executor bridge deframes/translates stream chunks ([source](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_executors.go#L775)). These mechanisms do not certify every format pair or terminal/error shape.
- **Bifrost:** `PreRequestHook` commits provider/model/fallback mutations; `PostLLMHook` can edit typed results/errors ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L411)). `RoutingInfo` distinguishes actual attempt provider/model, requested model and provider-reported server-side fallback ([schema](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/bifrost.go#L2131)). These fields are scoped metadata, not proof every client receives a faithful provenance field or all adapter translations preserve features.

**Integration / gaps.** **EXTENSION / SDK-HOST / CORE/UNVERIFIED:** qualify each inbound/outbound pair and error path, including post-commit failure and structured-output/tool fidelity; reject unsupported combinations. CCR adapters and CPA translator fallbacks require deliberate coverage. Bifrost's stock plugin initialization does not by itself provide an SDK host; new protocols may need SDK-host/core wiring or an extension-owned listener. Preserve actual route and provider-reported model separately; don't infer successful equivalence from a compatible wire shape.

**ProxyKit recommendation (proposed).** Use CCR's paired adapters with Bifrost's distinct requested and served routing metadata, for one explicit protocol pair first. Generic transport owns bounded streaming, cancellation and commitment. An optional AI runtime validates capabilities before dispatch, translates events/errors and reconciles terminal output with deltas and tool IDs. An extension supplies selection policy; an owned service may implement the complete bridge. A presentation alias is allowed only where the client contract permits; keep served provenance accurate and unknown where unavailable.

**Acceptance example (not executed).** Example user requests an alias; a different provider emits a streamed tool call and structured result. The client receives valid matching events, final object and stop reason, with paired IDs; records identify the actual provider/model. Unsupported conversion returns a client-shaped error before output, and never asserts semantic model equivalence.

<a id="r48"></a>

## R48 — Preserve or migrate conversation affinity and provider-held state when rerouting

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R02 routes accounts; R16 and R18 permit extension-owned durable state and concurrency; R19 covers hidden continuation replay; R20 considers provider-held/signed state during compaction. This contract additionally decides per turn and per attempt whether a provider/account change is safe and how foreign state is reconstructed or rejected, even without compaction.

**Implementation evidence.**

- **CCR:** The route resolver sees the source request and model before the target loop (G/gateway/handler.ts:418–430, 644–655); request transforms can modify raw or standard requests (G/types.ts:1448–1492). Gemini's target adapter stores thought signatures keyed by a cache scope and tool-use ID in a bounded in-process map (G/adapters/builtins/target/gemini-generate-content.ts:756–810). This is a useful provider-specific replay aid, not durable, cross-account migration or proof that every opaque reference is classified. G source provenance: [1.0.21 tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz) and [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** `Scheduler.Pick` chooses among auth candidates; `HostModelExecutionRequest` can force provider, `AuthID`, and caller scope ([interfaces](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L570), [request](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L767)). Nested host execution skips the originating plugin's router and interceptor to avoid recursion ([callback](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407)). A lost WebSocket continuation can return `FullContextRequired` and ask for full input without `previous_response_id` ([error](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/cliproxy/executor/websocket.go#L33)). That asks the client to reconstruct; it does not do so on all reroutes.
- **Bifrost:** `PreRequestHook` changes route and input once for subsequent fallback attempts; hook errors are logged and do not reject ([contract](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L411)). SDK configuration offers `KeySelector` ([schema](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/bifrost.go#L20)). OpenAI compaction conversion copies `PreviousResponseID` ([conversion](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/providers/openai/responses.go#L1644)); copying is not validation against another provider or account.

**Integration / gaps.** **EXTENSION / SDK-HOST / CORE/UNVERIFIED:** establish authenticated session identity, durable mapping and atomic concurrency, capture sufficient history and enforce compatibility on every fallback as well as the initial route. Route selection and key pinning alone do not prove state portability. Hook failures require an explicit denial path; Bifrost's SDK selector requires host wiring and is not implied by stock `.so` initialization. A separate extension-owned listener/coordinator/database is feasible but needs its own full visibility and control; no complete integration was established.

**ProxyKit recommendation (proposed).** Prefer affinity plus explicit rejection as the smallest safe initial policy. Optional AI adapters classify provider/account-bound response IDs, signed reasoning, cache handles, files and tool state. An extension owns tenant/session mappings and full-history records; before each attempt the runtime either pins the compatible origin or reconstructs a complete stateless request through an explicitly supported adapter, with resolvable resources and paired tools. If reconstruction, identity or safe retry is uncertain, reject; never merely drop a reference. Generic transport owns attempts and commitment, and cannot retry after visible output or side effects without a defined safe continuation.

**Acceptance example (not executed).** Work laptop sends a later turn with a provider A response ID and signed reasoning. A requested switch to account B is pinned to A or rejected unless the extension can reconstruct complete portable history and resource references; after restart the tenant-scoped mapping still selects correctly, and another tenant cannot use it.

<a id="r49"></a>

## R49 — Bind each route change to destination-specific credentials and authorization

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R01–R04 cover provider/account selection and eligibility, R34–R35 explicit denial and failure behavior, and R39 configuration snapshots. This adds a single per-attempt binding across destination, account, credential, redirects, nested calls and fallback policy; individual hooks do not establish that composition.

**Implementation evidence.**

- **CCR:** The gateway calls `routeResolvers[].resolve` after `beforeRouting`; a resolution supplies `targetProviderConfig` or ordered `targetProviders` (G/gateway/handler.ts:387–465, G/types.ts:1495–1524). The provider `authenticate` hook receives the target and current upstream request, and its successful result replaces the `UpstreamRequest`, including URL and headers (G/gateway/handler.ts:8214–8313, G/types.ts:989,1403–1424). Per-path model restrictions exist (G/gateway/handler.ts:759,1236,1711,2797). The normal upstream fetch builds headers and calls fetch without an explicit redirect option (G/upstream/client.ts:175–208); this alone does not prove credential safety for all redirect/header combinations. G is recovered gateway 1.0.21 source: [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** [`ModelRouter`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L576) selects self, executor or provider and target model before auth; `Scheduler.Pick` chooses among host auth candidates and can explicitly reject. The [host callback](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/host_callbacks.go#L407) routes nested calls through execution but excludes the originating interceptor and router. [Interceptor failures](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/internal/pluginhost/adapters_interceptors.go#L22) can continue; scheduler invalid/unhandled results may fall back. Custom executor controls its own destination and authentication.
- **Bifrost:** [`PreRequestHook`](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/plugin.go#L411) commits route/model/fallback mutations but errors continue. SDK [`KeySelector`](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/bifrost.go#L20) selects keys; stock [.so Init](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/framework/plugins/soloader.go#L70) does not supply SDK Account access. [`AllowFallbacks=false`](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L5839) can stop fallback; per-attempt key selection and aliases exist.

**Integration / gaps.** EXTENSION supplies route and policy; SDK-HOST/CORE or configuration must enforce a required check on every effective destination, nested call and fallback, bind the corresponding credential and sanitize inherited authentication, including redirects. None of the cited seams establishes this complete fail-closed guarantee or all redirect behavior. An extension-owned service can control its own dispatch, but host-managed coverage still requires wiring. No new tests were run.

**ProxyKit recommendation (proposed).** Generic dispatch accepts one immutable attempt decision containing destination, credential reference and identity; validates an explicit destination policy, removes inherited credential-bearing headers, then injects only the selected credential. Disable automatic redirects or reauthorize and rebuild authentication at every hop. Optional AI runtime invokes the same required check for each nested call and fallback; extensions own policy and custom destination configuration. Deny on missing decision or policy error. This governs managed dispatch, not trusted plugin sockets.

**Acceptance example (not executed).** A request initially chooses provider A, then falls back to configured provider B. The B attempt has only B credentials; a redirect to an unauthorized origin and a nested call to a denied model fail before sending credentials.

<a id="r50"></a>

## R50 — Separate requested/served route provenance and partition caches accordingly

**Status:** CCR: Integration needed; CLIProxyAPI: Integration needed; Bifrost: Integration needed.

**Existing coverage:** R28–R33 address observation, actual usage and attempt correlation; R39 snapshots configuration and R42 permits local cache responses. This adds complete requested-versus-effective route provenance and cache partitioning linked to that provenance, including classifier and candidate calls, rather than merely capturing usage or serving a cache hit.

**Implementation evidence.**

- **CCR:** The route resolver sees the requested model and can return a target and reason (G/gateway/handler.ts:415–445, G/types.ts:1495–1518). `BillingQueueEvent` has request ID, attempt sequence, model and provider fields (G/billing/publisher.ts:28–70); billing hooks may mutate or drop events before publishing (G/billing/publisher.ts:198–245). A pre-route handler can respond locally (G/gateway/routes.ts:340–418), but its miss/resume contract is not established. G denotes recovered gateway 1.0.21: [tarball](https://registry.npmjs.org/@the-next-ai/ai-gateway/-/ai-gateway-1.0.21.tgz), [manifest](research/2026-11-extension-audit/ccr-manifest.json).
- **CPA:** [`ModelRouteRequest` and response](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L694) carry requested model and target model/reason. [`UsageRecord`](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/pluginapi/types.go#L1644) includes model, alias, provider, base URL, auth ID, selection ID and tier fields when available. Nested `host.model.execute` uses host execution but skips its originating router/interceptor; [prepared stream routes](https://github.com/router-for-me/CLIProxyAPI/blob/91e0dee827abf476f9f86246f68c500a112f7155/sdk/api/handlers/handlers_context.go#L129) are not reused for that nested call. After-auth interceptors support local responses.
- **Bifrost:** [`BuildRoutingInfo`](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/schemas/account.go#L685) records requested and attempt provider/model, key name and resolved key alias; [worker execution](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/core/bifrost.go#L8469) resolves aliases per key and snapshots routing information. Semantic cache [direct IDs](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/plugins/semanticcache/search.go#L196) use cache key, request/parameter hashes and optionally provider/model; [identity scope](https://github.com/maximhq/bifrost/blob/6e640279b36337820f2088fb85f6e78f339063c3/plugins/semanticcache/utils.go#L340) is included when enabled. Those are useful scoped mechanisms, not proof of every effective target dimension.

**Integration / gaps.** EXTENSION owns cache rules, durable records and invalidation; SDK-HOST/CORE must expose and correlate effective endpoint, account, settings and route-decision version on every managed leaf attempt, including failed, nested and classifier calls. A pre-selection cache lookup cannot assume the final key-specific resolved alias. Accounting cannot rely solely on the client-visible model or best-effort telemetry. No new tests were run.

**ProxyKit recommendation (proposed).** Borrow Bifrost’s requested/attempted route distinction and CPA’s account-attributed usage fields. Generic dispatch emits immutable request and per-attempt provenance with parent IDs and unknown fields explicit; optional AI runtime supplies semantic model/settings/protocol and captures provider usage before presentation. Extensions persist records and cache only after deriving a key from authenticated tenant, effective destination, credential scope (or account when behavior depends on it), decision version, settings, protocol and relevant tools/input. An extension may deliberately share cache entries across proven-equivalent accounts; it must not share across authorization or semantic boundaries. Resolve before lookup, or use a validated versioned route identity; disable hits when required identity is unknown. Keep client model alias independent of recorded served model.

**Acceptance example (not executed).** An alias changes from provider A/model X to B/model Y. Identical input for the same tenant misses A's cache; a different tenant also misses. A classifier and a retried candidate each have separate actual-route/usage records even when the client response shows the original alias.

## Implementation priorities (proposed, not authorization to build)

**Start with one complete vertical slice:** a trusted broker extension owns its account catalog and optional enrollment application, selects an upstream, supplies credentials, and coexists with a separate recorder. This exercises R01–R11, R28–R37, R39, and R43 through a small transport core. It does not require enrollment, storage, UI, or job frameworks in that core.

**Then prove one recall/tool slice:** add an optional AI coordinator with one gateway search tool, one client tool, an archive extension, and an explicit caller/session identity. Exercise disconnects, duplicate tool names, mixed handoff, repeated history, archive failure, and restart recovery before expanding protocols. This exposes the difficult R12 and R19–R26 boundaries early.

**Defer features whose contracts are not yet needed:** hot reload, plugin marketplaces, dependency graphs, shared job/database services, universal model normalization, and untrusted sandboxing. Support extension-owned services from the outset through lifecycle and ordinary runtime access; add convenience APIs only after concrete repetition justifies them.

CCR offers the closest authoring model and useful protected hook execution. CLIProxyAPI offers the clearest broker-facing auth, selection, refresh, and nested execution seams. Bifrost offers useful typed hooks, precommit response-header handling, retry tracing, and a reusable nonstream MCP loop. Borrow these individual mechanisms rather than reproducing any router's entire framework. None of the audited interfaces establishes all 50 requirements as a complete, composable, tested feature set.

Building ProxyKit is reasonable if owning these shared contracts is the product goal. A new implementation still has to earn protocol correctness and compatibility; it will not inherit the maturity of the audited routers merely by adopting their ideas. The objective is a small, extensible foundation with explicit boundaries, not a promise that every possible extension works transparently with every client.
