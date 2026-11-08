# ProxyKit: router extension audit

The [50-requirement engineering reference](../../EXTENSION_ENGINEERING_REFERENCE.md) adds implementation traces, source citations, integration explanations, and proposed minimal ProxyKit contracts for every requirement.

> **Updated interpretation:** the user permits extensions to run full applications with their own listeners, storage and jobs. Use the [current 50-item checklist](capabilities-current.json) and [six architecture decisions](extension-requirements.md) for current decisions. The 43-row revision is historical. This original document describes the stricter host-supplied-services model.

Audited 2026-11-07. **None of the three supplies the complete requested foundation through its existing extension contract.** There is substantial overlap with the basic router/transform/provider idea, but the combined Broker + durable recall + streaming mixed-tool + independent accounting contract remains unfulfilled.

**Recommendation:** keep ProxyKit's architecture work, and treat CCR's underlying gateway as the first reuse/adaptation candidate. Its TypeScript API is much closer than the outer CCR wrapper suggests. CLIProxyAPI is the strongest broker-specific plugin substrate. Bifrost is a strong Go SDK/custom-host candidate. A wholesale rewrite of ordinary routing, credential hooks and telemetry would duplicate existing work; the valuable work is the missing host guarantees below. This historical audit did not select a runtime; Go has since been selected for ProxyKit transport.

## Quick decision

| Candidate | Closest fit | Decisive remaining work |
|---|---|---|
| **Claude Code Router + packaged AI gateway** | TypeScript roles, protocol adapters, arbitrary routes, streaming internal tools, publisher/outbox contracts | Final hook coverage differs in virtual loops; tool context lacks trusted conversation identity; private results are not reconciled on replay; mixed-turn results are skipped or exposed |
| **CLIProxyAPI** | Broker authentication, login/poll/refresh, account scheduling, custom execution and independent usage | No shared gateway-tool/mixed-continuation engine; public route/listener restrictions; policy errors fail open; durable context and transactional enrollment are plugin infrastructure |
| **Bifrost** | Operational AI gateway with Go SDK account/key services, hooks, MCP and observability | SDK services exceed stock plugin loader; autonomous tools are nonstreaming; mixed summaries lose duplicate-name results; durable replay absent; core policy errors fail open |

The original audit assumed the host supplied servers, loops, databases and jobs. That ownership assumption is superseded: extensions may supply them. Retained gaps concern shared interception, dispatch, delivery and enforcement guarantees, not the absence of specialized host services. The classifications below preserve historical evidence and must not be used as the current requirements.

## What was actually tested

- **CCR:** 12 local probes against the actual packaged gateway, all passed. Selected upstream suite: **51 passed, 1 failed, 0 skipped**; the failure concerns bundled-plugin permission defaults. Probes confirmed real internal SSE continuation, mixed-call limitations, hook bypass and missing hidden-history replay.
- **CLIProxyAPI:** actual server plus a loaded native C plugin and mock upstream: **17 assertions passed**; selected Go contract/scheduler/auth suites passed. Confirmed auth replacement, denial versus fail-open errors, nested calls, account failover, raw usage versus presentation rewrites, public-route restrictions and missing-usage normalization to zero.
- **Bifrost:** **54 selected named tests passed**, including eight added probes. Confirmed nonstream mixed handoff, duplicate-result loss, cancellation/fan-out boundary, policy error behavior, routing pins, HTTP hooks, attempt tracing and exporter bounds/drops. The native .so loader was source-inspected, not exercised with a new binary plugin.

Some passing probes deliberately confirm undesirable behavior. These counts are not full-suite or acceptance certification. All inference/tool/auth fixtures were synthetic and local; no live account enrollment/provider or real coding-client compatibility test was performed. Full-repository comprehension cannot be certified: the checkouts have 863 / 2,264 / 6,552 tracked files. The audits trace the relevant public contracts and execution paths with source links, rather than claiming every line and dependency was reviewed. Restart-safe compaction, distributed consistency, production load and all-provider protocol fidelity remain unproven.

## Central capability checklist

**Available** = usable seam exists; feature plugin still needs implementation. **Partial** = restricted path, custom host or extra plugin infrastructure. **Gap** = no matching host guarantee in the reviewed extension contract, even if trusted code can build its own. **Unverified** = insufficient evidence. **T** = local runtime/contract test; **S** = source inspection. Tests apply only to the scoped explanation, not every possible behavior in that cell.

CCR includes its actual packaged core, clearly distinguished in [its report](ccr.md). Bifrost's column describes the stock dynamic-plugin boundary and explicitly labels broader SDK capabilities. Each detailed report contains commit-pinned source references supporting the checklist. The [interactive checklist](capability-matrix.html) uses the same [machine-readable data](capabilities.json).

### Broker

| Requirement | CCR + gateway | CLIProxyAPI | Bifrost |
|---|---|---|---|
| R01 Replace inbound authentication | △ Partial **S/T** — Core beforeAuth can reject; built-in auth/introspection remains a distinct layer. | ✓ Available **T** — FrontendAuthProviderExclusive replaces ordinary request auth; management auth is separate. | △ Partial **S/T** — HTTP pre-auth translates credentials or denies; stock governance remains authoritative. |
| R02 Choose account / upstream route | ✓ Available **T** — routeResolvers can return dynamic provider configs and fallback lists. | ✓ Available **S/T** — Scheduler and ModelRouter over host-eligible candidates; not unrestricted retry ownership. | △ Partial **S/T** — Plugin can pin eligible keys; full Account / KeySelector replacement requires SDK/custom host. |
| R03 Replace credentials / implement refresh | ✓ Available **T/S** — ProviderPlugin.authenticate replaces upstream credentials; refresh service is plugin-owned. | ✓ Available **S/T** — AuthProvider parse/login/poll/refresh plus managed auth callbacks; live OAuth untested. | △ Partial **S** — Custom Account in SDK; stock plugin loader does not register a general refresh provider. |
| R04 Account CRUD / label / client ACL model | △ Partial **S** — Plugin routes and SQLite support own model; account connector is quota-oriented. | △ Partial **S/T** — Auth list/get/save + metadata; no auth.delete callback or complete relational label/ACL service. | △ Partial **S** — Built-in provider-key and virtual-key ACL CRUD; not plugin-defined account domain. |
| R05 Public enrollment POST + private service auth | △ Partial **T/S** — Arbitrary routes exist; scoped service auth/cookies/listener separation are own work. | △ Partial **T/S** — Management POST protected by management key; public resources GET-only. External listener needed. | △ Partial **S/T** — Built-in management sign-in is provider-specific; generic plugin route registration absent. Custom host needed. |
| R06 Isolated public enrollment listener | ✕ Gap **S** — Can run own backend/service; no generic host-enforced enrollment process boundary. | ✕ Gap **S** — Plugin can run own service; no host-managed isolated enrollment listener. | ✕ Gap **S** — Own listener or custom host; no dynamic-plugin listener service. |
| R07 Atomic activation + labels + notification outbox | ✕ Gap **S** — SQLite/outbox primitives exist separately; no shared enrollment transaction contract. | ✕ Gap **S** — File save and runtime upsert; no shared plugin/account/outbox transaction. | ✕ Gap **S** — Framework databases exist; no corresponding dynamic-plugin transaction contract. |
| R08 Quota snapshots / per-attempt outcomes | ✓ Available **S/T** — Account connectors, provider hooks and billing/trace events; exact Broker policy is own work. | ✓ Available **S/T** — QuotaProvider and UsagePlugin include account/failure metadata. | △ Partial **S/T** — Core key retries and trace spans; broker-specific quota refresh/store is own work. |

### Transforms & protocols

| Requirement | CCR + gateway | CLIProxyAPI | Bifrost |
|---|---|---|---|
| R09 Rewrite request / instructions / tool schemas | ✓ Available **T** — Raw body + standard request transforms before routing/upstream. | ✓ Available **T/S** — Interceptors, body patchers and translators. | ✓ Available **S/T** — PreRequest / PreLLM typed mutation and HTTP hooks. |
| R10 Modify ordinary nonstream response | ✓ Available **T** — Provider and final response hooks; virtual model final hook differs. | ✓ Available **T** — ResponseInterceptor and translation stages. | ✓ Available **S/T** — PostLLM and HTTP transport post hooks. |
| R11 Modify ordinary live stream | ✓ Available **T** — streamHooks wrap upstream Response on ordinary paths. | ✓ Available **T** — StreamChunkInterceptor + host streaming callbacks; host adds terminal marker. | ✓ Available **T/S** — Typed post hooks and transport chunk interceptor; drop/abort semantics tested. |
| R12 Same output hooks cover hidden tool loops | ✕ Gap **T** — Virtual nonstream bypasses final response hook; optimistic virtual stream bypasses stream hook. | △ Partial **S/T** — Nested model calls skip caller interceptor; no generic host tool loop to cover. | △ Partial **S** — Nonstream continuations reenter core; no streaming agent loop. Coverage depends on role/path. |
| R13 Add new wire protocol through plugin | ✓ Available **S** — sourceAdapters + targetAdapters; preservation must be implemented/tested by adapter. | △ Partial **S** — Custom translator formats/executors; new inbound HTTP endpoints require host/service work. | △ Partial **S** — Custom provider based on existing wire formats; entirely new protocols require host integration. |
| R14 Register arbitrary HTTP method/path | ✓ Available **T/S** — Core httpRoutes and outer registerGatewayRoute. | △ Partial **T** — Management prefix routes + public GET resources; arbitrary public POST returned 404. | ✕ Gap **S** — Stock .so loader has no route registration; custom SDK host can mount routes. |
| R15 Generic bidirectional WebSocket extension | ? Unverified **S** — Built-in Responses/MCP WebSocket code exists; generic plugin frame/upgrade contract unverified. | △ Partial **S** — Built-in WebSockets and response observer; no generic frame/upgrade registrar. | △ Partial **S** — Built-in realtime routes; no generic upgrade/frame registration in dynamic loader. |

### Context & tools

| Requirement | CCR + gateway | CLIProxyAPI | Bifrost |
|---|---|---|---|
| R16 Scoped durable storage for extension | △ Partial **S/T** — Outer openSqliteStore/dataDir; core plugins need their own storage access. | ✕ Gap **S** — Native code may own a DB; no general host storage callback. | △ Partial **S** — Framework stores/KV available to custom host; not injected through generic plugin Init. |
| R17 Archive-before-removal transaction | ✕ Gap **S** — Built-in archive exists; no generic extension working-context transaction. | ✕ Gap **S** — Plugin must implement transaction and explicitly terminate on failure. | ✕ Gap **S** — Plugin-owned transaction; no shared archive/context commit service. |
| R18 Context revisions + concurrent session ownership | ✕ Gap **S** — No generic revision/CAS compactor contract established. | ✕ Gap **S** — No context revision/CAS API in capability or callback enumeration. | ✕ Gap **S** — No transcript revision/CAS service in plugin interface. |
| R19 Restore private continuation on client replay | ✕ Gap **T** — Same-session replay lacked hidden result; fold exposes text instead. | ✕ Gap **S** — No host private-transcript/replay state machine; implement in plugin. | ✕ Gap **S** — Agent loop state is local; summary text is not a durable reconciliation map. |
| R20 Compaction with provider-held / signed state | ? Unverified **S** — Built-in client compaction code exists; full opaque/state preservation not tested. | ? Unverified **S** — Translations exist; proposed compactor must resolve provider-held state explicitly. | ? Unverified **S** — Provider compaction exists; extension rewrite/state fidelity not established. |
| R21 Register executable gateway tool | △ Partial **T/S** — Virtual profiles bind MCP tools; no direct tool implementation array in core plugin result. | ✕ Gap **S** — No generic tool registrar/dispatcher; custom executor can implement one. | △ Partial **S/T** — SDK RegisterMCPTool / configured MCP server; not handed to dynamic plugin Init. |
| R22 Host-owned nonstream tool continuation | ✓ Available **T** — Virtual model tool_loop and separate transparent-tool path. | ✕ Gap **S** — host.model.execute supports plugin-owned orchestration, not a shared host tool loop. | ✓ Available **T** — MCP agent mode continues Chat/Responses and sums usage. |
| R23 Host-owned streaming tool continuation | △ Partial **T** — Optimistic virtual loop works; hook/identity/replay gaps remain. | ✕ Gap **S** — Streaming bridges exist; tool-loop state machine is plugin-owned. | ✕ Gap **S** — Agent Mode explicitly excludes chat_stream/responses_stream. |
| R24 Lossless mixed client / gateway handoff | △ Partial **T** — Stops for client calls; internal calls skipped by default or results exposed when folded. | ✕ Gap **S** — No shared tool ownership or mixed-handoff engine. | △ Partial **T** — Executes gateway tools and returns client IDs; same-name results collapse in text summary. |
| R25 Trusted tenant/session context in tool execution | ✕ Gap **S** — Virtual path builds profile-scoped session and forwards args/multimodal meta to MCP. | ✕ Gap **S** — No host tool-invocation API; plugin must bind caller and session itself. | △ Partial **S** — Bifrost context exists; registered local tool callback takes args only; binding needs care. |
| R26 Cancellation through every internal tool | △ Partial **S** — Transport abort exists; virtual tool call does not receive client abort signal. | △ Partial **T/S** — Host callbacks cancel; arbitrary native execution continues until it cooperates. | △ Partial **T** — Canceled loop still dispatches mock callbacks; downstream must enforce; local tool callback lacks context. |
| R27 Bounded tool turns + parallel execution | △ Partial **S/T** — Virtual maxTurns/maxToolCalls; serial tools; no complete elapsed/spend budget tested. | ✕ Gap **S** — No shared continuation budget; generic native calls admit unconstrained concurrent work. | △ Partial **T/S** — maxAgentDepth exists; 64 tool calls entered concurrently without loop-level cap. |

### Telemetry

| Requirement | CCR + gateway | CLIProxyAPI | Bifrost |
|---|---|---|---|
| R28 Independent request/response/usage extension | ✓ Available **T/S** — Transforms plus billing publishers/outboxes; virtual stream hooks need separate coverage. | ✓ Available **T** — UsagePlugin separate from mutators; raw transcript visibility depends on chain order. | ✓ Available **S/T** — HTTP/LLM hooks and independent ObservabilityPlugin.Inject traces. |
| R29 Actual usage retained before presentation edits | △ Partial **T/S** — 18 real vs 1998 displayed verified; billing event hooks may mutate/drop before sinks. | △ Partial **T/S** — 10 real vs 1998 displayed verified on OpenAI-compatible route; not all translators. | △ Partial **S/T** — Core traces capture usage before ordinary post hooks; full independent ledger not validated. |
| R30 Account for internal model calls / attempts | △ Partial **T/S** — Two-call loop aggregates 36; all failed-provider cases not tested. | ✓ Available **T** — Nested host.model.execute + 429 failover each emit usage; generic host.http does not. | ✓ Available **T/S** — Continuation usage and per-attempt traces tested; avoid double-counting aggregated result. |
| R31 Reliable lifecycle / attempt correlation | △ Partial **S** — Request/attempt fields exist; comprehensive cross-path joining not tested. | △ Partial **T** — Lifecycle and usage RequestID differ; shared TraceID cannot uniquely join nested attempts. | △ Partial **S/T** — Trace hierarchy and agent IDs exist; pooled objects require ownership/copy discipline. |
| R32 Missing usage distinct from measured zero | ? Unverified **S** — All missing-usage paths not tested. | ✕ Gap **T** — Successful missing usage normalized to zero counters; no presence flag. | ? Unverified **S** — All provider/trace missing-usage paths not established. |
| R33 Durable export / bounded observer delivery | △ Partial **S** — Outbox/publisher/delivery-state extension contracts; durability supplied by chosen backend. | ✕ Gap **S/T** — In-memory global queue, sequential sinks; buffer argument does not cap global queue. | △ Partial **T/S** — Bounded async exporters drop saturated traces; built-in logs are a separate durable path. |

### Operations

| Requirement | CCR + gateway | CLIProxyAPI | Bifrost |
|---|---|---|---|
| R34 Explicit policy denial before upstream | ✓ Available **T** — Core beforeAuth/other hooks can reject; outer transform errors alone do not. | ✓ Available **T** — Successful Terminate or explicit auth denial blocks upstream. | ✓ Available **T** — LLM short-circuit error and HTTP denial; disable fallbacks for mandatory denial. |
| R35 Fail-closed mandatory policy on unexpected error | △ Partial **T/S** — Core defaults fail_closed; outer transforms fail open and startup can skip failed plugin. | ✕ Gap **T/S** — Interceptor errors fail open; scheduler semantics differ; no global required-role override. | ✕ Gap **T/S** — LLM errors fail open; HTTP errors differ; no generic mandatory-role guarantee. |
| R36 Per-hook deadline / queue / concurrency policy | ✓ Available **S** — Core execution config exposes timeout, concurrency, queue, failure threshold/cooldown; optional. | ✕ Gap **T/S** — No generic worker bound; 24 calls entered and remained active after caller cancellation. | △ Partial **S/T** — Provider queues/exporter bounds; synchronous LLM hooks lack independent forced timeout. |
| R37 Deterministic extension order | ✓ Available **S** — Named stages and registration order; no dependency DAG established. | ✓ Available **S** — Priority descending then ID; single chosen scheduler with defined fallback rules. | ✓ Available **S/T** — Pre hooks forward / post reverse; before/builtin/after placement + order. |
| R38 Dependencies / exclusive compactor arbitration | ✕ Gap **S** — No request-transform dependency graph or exclusive compactor contract found. | ✕ Gap **S** — No dependency DAG/cycle check or compactor arbitration. | ✕ Gap **S** — Order/placement is not dependency validation or compactor arbitration. |
| R39 Stable plugin snapshot for whole request | ? Unverified **S** — Loader replacement validation exists; whole-request snapshot guarantee unverified. | △ Partial **S** — Atomic active sets per access; each hook can fetch a new set. | ? Unverified **S** — Live config/plugin swapping exists; whole-request guarantee unverified. |
| R40 Host job service available to ordinary plugin | △ Partial **S** — Delivery workers and plugin backends exist; general durable jobs need plugin service. | ✕ Gap **S** — No general host job callback; plugin owns goroutines and queues. | △ Partial **S** — Sidekiq durable jobs in framework; custom host integration, not injected in .so Init. |
| R41 Enforced secret / OS sandbox per extension | ✕ Gap **S** — Trusted JavaScript sees config; permission flags gate registration only. | ✕ Gap **S** — Trusted native C ABI + raw auth callbacks; no OS sandbox. | ✕ Gap **S** — Trusted in-process Go .so; no sandbox from manifest/config alone. |
| R42 Short-circuit cache / local response | ✓ Available **S/T** — Plugin HTTP routes and policy response handling can answer locally; full cache policy is own work. | ✓ Available **T/S** — Request termination/custom executor can synthesize response; semantic cache is own work. | ✓ Available **T/S** — LLM/HTTP short circuits; built-in cache plugins demonstrate use. |

## Required API and permission catalog

[Extension requirements](extension-requirements.md) maps **12 requested/related extension families** to permissions, named illustrative host services and feature-owned HTTP endpoints, then adds **12 further extension scenarios**: redaction, egress policy, evaluation/shadow traffic, human approval, multimodal artifacts, realtime, distributed leases, signing, deletion/retention, prompt experiments, batch jobs and untrusted plugins. These API names are proposals, not an implemented spec.

Existing route/API differences that matter immediately:

| Need | CCR | CLIProxyAPI | Bifrost |
|---|---|---|---|
| Mount enrollment/admin endpoints | `httpRoutes` / `registerGatewayRoute`, auth gateway or none | Plugin management routes under `/v0/management`; public GET resources under `/v0/resource/plugins/<id>/`; not arbitrary public POST | No generic .so route registrar; custom Go host can add routes |
| Account lifecycle | Plugin-owned service + provider authenticate hook | `host.auth.list/get/get_runtime/save/models`; `AuthProvider` start/poll/refresh; no host.auth.delete callback | SDK `Account`; built-in `/api/providers/{provider}/keys` CRUD |
| Existing built-in auth controls | Wrapper local-auth integration; not generic public enrollment | `/v0/management/auth-files` GET/POST/DELETE, status/fields PATCH, refresh POST; `/codex-auth-url`, `/get-auth-status` under management prefix | Provider-key CRUD; Copilot `/api/providers/{provider}/sign-in` and `/sign-in/poll`; MCP OAuth is a separate credential domain |
| Usage monitor | Billing hooks + publishers/outboxes; plugin routes for UI/API | `UsagePlugin` + plugin management/resource APIs | `ObservabilityPlugin.Inject` + HTTP/LLM hooks; custom collector endpoint/service |
| Recall model tool | MCP-backed virtual profile | Custom orchestrator via `host.model.execute`, streaming bridges | Configured MCP or SDK `RegisterMCPTool`; nonstream agent loop |
| Durable context revisions | No generic host contract | No generic host contract | No generic plugin contract |

The existing Broker public/private endpoint contract is documented in the requirements catalog. Public enrollment involves secure cookie/service boundaries, account activation, labels, credentials and an outbox transaction; an OAuth start endpoint alone is insufficient. Conversely, the generic proxy need not hardcode Broker's exact route names if it exposes the correct registration and authorization services.

## What to do with these results

1. **Use CCR's core as the first adaptation candidate** if TypeScript authoring remains important. Require explicit closure of the tested hook/tool/replay gaps before treating it as a foundation. Consider the packaged gateway's independent maintenance/source provenance as part of adoption: this audit recovered its source from the published source map, not a separate verified source repository.
2. **Use CLIProxyAPI when brokering is the immediate product priority**, either through its existing plugin contracts or as an upstream service. It already has much of the account machinery; a recall extension would still need an orchestrator and durable state. Splitting services introduces extra routing/accounting boundaries to own.
3. **Choose Bifrost for a Go SDK/custom-host integration** when its operational features are valuable and custom host wiring is acceptable. Do not assume a stock dynamic plugin receives all SDK services.
4. For ProxyKit's full vision, the justified core work is **a shared streaming tool/continuation contract, trusted conversation identity, durable context revisions/replay, complete hook coverage, original usage attribution, and required-policy/resource rules**. Keep optional feature policy in plugins. The decisive test is the combined Broker + recall + observer scenario in the requirements catalog; no audited candidate passed that full scenario unchanged.

[Ideas to retain](ideas-to-retain.md) records useful mechanisms, including a focused billion-context inspection of cooperative identity, revision-guarded mutation and context-usage provenance. Headroom was not studied, as permitted. No claims of universal client compatibility or an exhaustive internet-wide absence are made.

## Source identities and evidence index

| Project | Exact source | Main findings | Runtime evidence |
|---|---|---|---|
| CCR | [057abaa1f95519ab8faf999e1ccef2b80ccdeb9e](https://github.com/musistudio/claude-code-router/tree/057abaa1f95519ab8faf999e1ccef2b80ccdeb9e) + npm gateway 1.0.21 | [CCR audit](ccr.md), [artifact manifest](ccr-manifest.json) | [Probe code](ccr-probe.mjs), [results](ccr-probe-results.json), [upstream test log](ccr-upstream-tests.log) |
| CLIProxyAPI | [91e0dee827abf476f9f86246f68c500a112f7155](https://github.com/router-for-me/CLIProxyAPI/tree/91e0dee827abf476f9f86246f68c500a112f7155) | [CLIProxyAPI audit](cliproxyapi.md), [manifest](cliproxyapi-manifest.json) | [Reproduction](cliproxyapi-fixtures/README.md), [HTTP results](cliproxyapi-runtime-results.jsonl), adjacent logs |
| Bifrost | [6e640279b36337820f2088fb85f6e78f339063c3](https://github.com/maximhq/bifrost/tree/6e640279b36337820f2088fb85f6e78f339063c3) | [Bifrost audit](bifrost-findings.md), [manifest](bifrost-evidence-manifest.json) | [Test summary](bifrost-test-summary.md), [probe patch](bifrost-probes.patch), adjacent logs |

Research artifacts only were added to ProxyKit. No proxy implementation, production dependency, issue, PR, commit or push was created by this audit. The [REA skill](/root/.codex/skills/reverse-engineer-anything/SKILL.md) was used for the shipped CCR package and its broader investigation workflow; its static [evidence summary](ccr-rea-summary.json) is separate from runtime observations.
