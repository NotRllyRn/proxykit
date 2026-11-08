# ProxyKit extension layer: consolidated plan and decision log

Updated 2026-11-08. **Planning only. No extension execution API is implemented by this document.** This is the current entry point for extension planning. It incorporates the latest user decisions and supersedes conflicting proposals in earlier documents, particularly separate match/check callbacks and skipping unrecoverable streams without retained input.

The [project direction](PROJECT_DIRECTION.md), [six architectural decisions](research/2026-11-extension-audit/extension-requirements.md), [composition research](EXTENSION_COMPOSITION.md), [transport readiness review](CORE_READINESS.md) and [R01–R50 engineering reference](EXTENSION_ENGINEERING_REFERENCE.md) remain supporting material. Router status labels are historical evidence, not proof of ProxyKit support. Recommendations below are distinguished from settled user direction; unresolved choices are listed explicitly.

## Implemented activation update

[Safe hot activation](HOT_ACTIVATION.md) supersedes older restart-only/deferred-live-reload assumptions: installations require explicit `apiVersion: 1` and default to persistently disabled; online enable/disable publishes complete validated generations while reusing unchanged workers. Roots and descendants remain generation-pinned. Required dependency failures reject activation, unresolved legacy intent blocks startup, and overlapping stateful replacement is rejected. Bounded retirement and trusted-code limitations are documented there; this does not introduce untrusted hosting or resource migration.

## 1. User decisions to preserve

1. Build a minimal generic HTTP proxy with an optional extensible execution layer. Keep Pi-like authoring simplicity without importing a CLI execution model into a streaming server.
2. Trusted extensions may be complete applications: listeners, enrollment, dashboards, databases, account stores, refresh, jobs, tools, model coordinators and external services. These do not require corresponding core subsystems.
3. Extensions have broad freedom over requests, responses, routing and credentials. ProxyKit does not police whether a provider credential matches the extension's selected destination. Application mistakes belong to extension developers; host transport and documented API mistakes remain ProxyKit's responsibility.
4. Users should not manually arrange extensions. Developers declare hooks and their execution requirements. Explain known conflicts before activation; do not claim universal compatibility for arbitrary code.
5. Keep request-local routing, original intent, editable working content, shared exchange metadata and a bounded execution ledger. Internal metadata is not automatically sent upstream or to clients.
6. **No separate check/match function.** Invoke the handler with its applicable input/context. It decides whether to act and can immediately return unchanged/pass so the next handler runs. Do not require an additional eligibility API or duplicate expensive checks.
7. A failed transformation should discard its candidate output and continue from that hook's prior accepted input where recoverable. Retain stream input needed for recovery; do not hand the next hook an exhausted reader or partial failed transformation.
8. An extension-generated response enters the response pipeline, even if no upstream call occurred. A response-triggered new request enters the request pipeline again. Neither is a shortcut around the other direction's hooks.
9. Extensions can call providers privately, or call the ordinary ProxyKit HTTP endpoint to reuse its configured authentication/broker and routing behavior. Internal lineage/round tracking should work through a documented header convention, without a second inference endpoint.
10. Defaults should limit stalls and resource consumption. Users may raise or disable configurable limits. A disabled limit removes its guarantee; unlimited extension freedom cannot imply guaranteed response completion.
11. Keep protocols/adapters, tools, durable context and observability modular. The 50 requirements are a capability catalog, not 50 mandatory core frameworks.
12. Record newly discovered edge cases and unresolved contracts here during implementation. No extension implementation is authorized by this planning update alone.

## 2. Recommended architecture

Keep six conceptual boundaries: routing/dispatch; optional orchestration/tools; extension-owned state; original observations versus editable delivery; configurable failure/resources; explicit composition/lifetime. Implement ordinary small interfaces around real examples, not an all-purpose workflow system.

An extension package registers individual hooks. One package can have multiple roles. Suggested declarations are a stable hook ID, stage, supported input format/body access, ordering dependencies and failure behavior. Exact names/types remain undecided. These declarations are contracts for integration, not security permissions for unrestricted native code.

Recommended stages:

- **Request preparation:** incoming context and transforms that also apply when a local response will be returned. Installed authentication can run here with declared prerequisites.
- **Resolution:** select local response or proceed toward an upstream. A responder can return pass; the first selected local response changes direction rather than ending processing.
- **Upstream preparation/dispatch:** route, extension-selected credentials, outgoing transforms and attributed attempts. Only runs when an upstream is needed.
- **Response start:** all selected response sources expose metadata/headers and establish the body-processing plan before delivery.
- **Response body/completion:** stream or retained-body processing, protocol encoding and final delivery. Completion observations cannot rewrite already delivered bytes.
- **Finish:** outcome, ledger and resource cleanup on success, error, cancellation and local responses.

Hook outcomes should be explicit and stage-appropriate: unchanged/pass, replacement, local response, or child request/continuation. A returned new request must not be mistaken for response bytes. Advanced raw access can remain possible with a clear statement of which ordinary guarantees it bypasses; do not design that escape hatch before a concrete need.

Within stages, recommend stable hook-ID ordering plus narrow developer `before`/`after` or required-capability declarations. Reject cycles, impossible stage dependencies and missing prerequisites. Distinguish optional ordering from mandatory dependencies. Do not expose user ordering. Numeric priorities alone do not express why one hook depends on another. Exact dependency syntax remains open.

Every response enters the complete applicable response chain. “Applicable” means a handler registered for that format/event, not silently bypassing a hook because a cache or another extension supplied the output. A whole-body handler runs once; an event handler runs per event; an error handler receives error information rather than a fabricated successful model answer.

## 3. Re-entry and internal authentication

There are two related paths:

- An extension uses an ordinary HTTP client to call ProxyKit, choosing its normal API path and payload. It receives a normal HTTP response, while the new request runs the configured request and response pipelines.
- A response hook asks the host to continue with a new request before final output commitment. That child must follow the same pipeline behavior rather than directly bypassing request hooks. Whether this is a typed return value or a small HTTP helper is still open.

The purpose of the internal path includes **reusing gateway-managed upstream authentication**. An extension should not need to maintain provider accounts merely to ask a model a helper question. However, identifying an internal caller and authorizing reuse of a caller/account context are separate from upstream credential selection. A plain caller-supplied “internal” header must not silently become permission to impersonate another user.

Recommend a documented root request ID, parent request ID, originating extension ID and nesting depth. An optional helper can propagate these and a deadline while using any normal HTTP endpoint. Exact header names and the internal authentication mechanism remain open: deployment-issued service credentials or a scoped context reference are candidates. Public callers' forged lineage must not grant additional access. Strip reserved internal fields at the proxy boundary before provider forwarding; no ledger or secrets in diagnostic headers.

Each child gets a fresh exchange/attempt identity while linking to its root. Decide explicitly which caller identity, broker policy and namespaced metadata it inherits; do not blindly clone every mutable parent field. A helper model call may be part of the same operation without belonging to the client's durable conversation.

Depth measures nesting. Sequential calls and parallel fan-out need separate counters; tool rounds are coordinator-owned. A shared root attempt count can account for participating calls, incremented atomically when needed. Unmarked/private calls cannot be identified reliably by source IP or timing. Conventions support cooperating extensions; they do not prove absence of infinite loops.

All applicable hooks run again on re-entry. A handler can inspect lineage and immediately return unchanged to avoid triggering itself repeatedly. Do not automatically skip its hooks behind its back. Root cancellation/deadlines can propagate through a cooperating helper; arbitrary HTTP clients may ignore them.

When a child is used to produce a parent answer, distinguish child delivery from parent delivery. Both exchanges have response pipelines, but avoid accidentally applying a non-idempotent parent decoration twice to the same final representation. The ownership of child results, cancellation of abandoned upstream bodies and metadata merging require a concrete acceptance example before implementation. Ledger origin IDs should make this diagnosable.

## 4. Failure recovery and saved streams

The recovery baseline is **the input accepted immediately before the failing hook**, preserving successful earlier transforms. The immutable provider/client original is a separate optional restoration source. Discard partial candidate output and associated uncommitted changes; continue with the baseline when it can actually be replayed. External database writes, HTTP calls and tools are not rolled back.

Recommend host-owned input retention and candidate-output ownership rather than allowing a transforming hook to write directly to the client. The host commits successful results, discards late results after timeout and controls response framing. A hook returning unchanged must not require cloning its entire input.

There are two different recovery guarantees:

- **Per-unit streaming recovery:** retain the current chunk or complete protocol event while a hook processes it. Commit its result only after success. If it fails, pass the retained unit to the next hook. This preserves low latency, but earlier successfully committed units remain changed. A stateful transform may need a larger recoverable unit or buffered mode.
- **Whole-body recovery:** retain/spool the hook's complete input and withhold its candidate output until completion. If it fails, replay the prior body into the next hook. This meets whole-answer rollback but delays delivery and consumes storage. An unbounded/live stream may never complete.

**User intent is retained input and fallback; the recovery unit/default is still to be selected.** Recommendation: incremental event recovery for explicitly compatible stream hooks, whole-body retention for hooks promising whole-answer rollback. Never label event recovery as restoration of an entire answer. Do not claim that saving input alone makes rollback possible after output has reached the client.

A pipeline may retain data in memory below a threshold and spill to disk above it. Thresholds, total quotas, overflow behavior, cleanup, permissions and retention lifetime remain open. Intermediate bodies can be released after the last consumer/recovery need; no automatic permanent history or diff database. Slow readers and extension stalls must exert bounded backpressure.

Headers, status, encoding/length, trailers and namespaced metadata may need to commit together with body changes. A failed transformation must not leave headers describing discarded output. In-place edits of shared mutable objects cannot support reliable timeout rollback; a concrete replacement/ownership contract is required.

A configurable 30-second invocation default remains a recommendation, not an implemented setting. Separate handler invocation, stream inactivity, entire stream lifetime and overall operation deadlines. Users may disable them. Timeouts stop waiting, not arbitrary in-process code; do not reuse capacity or accept late edits as if the underlying work has stopped. Worker isolation is needed for forcible termination.

## 5. State, ledger and observations

Create a private exchange context for each incoming HTTP request. Its response already belongs to that exchange through the host call/transport; no client thread header is needed for request-to-response metadata. Broker metadata written on the outgoing path remains available to response decorators. Concurrent exchanges do not share mutable working state.

Keep distinct:

- Original request facts and per-attempt provider facts, with body retention explicitly declared.
- Current accepted working content and revision.
- Namespaced extension metadata, with producer/schema and declared ownership.
- Host-written ledger of hook invocation, pass/change, early response/child request, failure/timeout, not-reached reason, timing and revision IDs.
- Attempt observations: upstream calls, outcomes and actual usage, before presentation changes.

No separate “check passed” metadata remains. A handler can report a short pass reason without running an additional callback. “Changed” can describe the returned outcome, not a costly verified byte diff. Bound ledger growth; expose truncation and let an observer persist/export it. Do not attach secrets or automatically log full payloads.

A telemetry extension can subscribe to original attempt data; it need not infer usage from decorated text. It may opt into original body/event observation if its function requires that data. Broker quota snapshots, provider token usage and UI presentation are different values. Missing usage remains unknown.

Conversation identity links multiple requests and is separate from exchange identity. Protocol/client adapters can supply an explicit identity scoped to an authenticated principal when available. Do not guess conversation membership from identical prompts, IP addresses or concurrent arrival. Durable recall must state unsupported clients or require an explicit client integration.

Optional full history/diffs remain deferred. Retention requests must be known before bytes are discarded; a later request for an unretained revision returns unavailable. Arbitrary compressed/binary replacements do not necessarily yield small diffs.

## 6. Streaming/protocol recommendation

Generic HTTP bytes do not map one-to-one to text tokens or model events. An adapter assembles complete SSE/protocol events before semantic hooks. Retain per-exchange/per-item state for operations such as prefixing an answer once; completed objects must agree with prior deltas.

Separate three declarations: what data a hook needs, when it runs, and whether it can change output. “Replayable” describes retained input, not an execution stage. Metadata hooks need no body; whole-body observers can run after streamed delivery if capture was configured; whole-body modifiers need a pre-delivery hold. Choose a compatible plan before body consumption. Do not silently skip a modifier to keep the stream moving.

Local responses, fallback answers and coordinator answers use the same final processing contracts. Response-triggered new requests are valid before commitment; after commitment an extension may initiate additional work, but cannot transparently substitute a new answer unless the client protocol supports that transition. Upgrades, 1xx informational responses, trailers and interrupted streams need explicit treatment.

Broker's reviewed usage decorator provides a useful example: it parses SSE, tracks message IDs, adds a prefix once per qualifying item and updates final representations consistently. Pi demonstrates narrow event contracts and sequential transforms, not immunity to extension conflicts. See pinned evidence in the composition note.

## 7. Coverage of the existing 50 requirements

This plan refines the entire catalog without renumbering historical evidence:

- **R01–R08:** auth/broker hooks, extension-owned account/enrollment/transaction services, request-local routes and original account/attempt information. Credential policing is optional extension policy.
- **R09–R15:** staged request/response transforms, identical applicability across response sources, protocol adapters, extension-owned HTTP services and separately scoped WebSocket capabilities.
- **R16–R20:** extension-owned durable state, archive-before-removal, concurrency/revisions, private-history reconciliation and explicit provider-held-state support.
- **R21–R27:** optional tool/coordinator ownership, mixed calls and IDs, trusted invocation context, internal authenticated HTTP reuse, lineage/cancellation and configurable budgets.
- **R28–R33:** independent observations, original usage, nested-attempt correlation, unknown usage and explicit persistence/drop behavior; ledger supplements rather than replaces these.
- **R34–R36:** explicit denial when a protection feature is installed, declared error behavior and configurable invocation/admission limits. No universal mandatory provider policy.
- **R37–R43:** developer-defined composition, declared compatibility, stable active configuration, extension lifecycle/jobs, optional enforced sandboxing, local responses and full extension applications.
- **R44–R50:** target settings/capability translation, precommit rerouting, classifier/candidate coordination, client wire compatibility, state affinity, optional credential authorization and requested/served provenance/cache scope.

New concerns below are tracked as planning items, not newly audited R51+ requirements. Do not imply existing routers support them without further evidence.

## 8. Open decisions and newly identified cases

- **P01 — Recovery scope:** chunk, semantic event, message or whole answer? Define what happens when a stateful streaming hook fails after earlier successful units. A saved source alone cannot reverse bytes already delivered.
- **P02 — Retention limits:** memory/disk thresholds, aggregate concurrent usage, disk-full behavior, secret-bearing temporary files and crash cleanup. Define infinite streams and unusually large uploads.
- **P03 — Authenticated internal reuse:** service credential versus scoped context reference, caller inheritance, untrusted lineage headers, expiration and prevention of cross-user metadata mixing.
- **P04 — Re-entry duplication:** whether a decorated child response becomes parent working input; define idempotency/provenance so footers, compaction and usage accounting are not unintentionally doubled.
- **P05 — Nested-call deadlock:** an extension holding its only execution slot calls ProxyKit, which waits for that same extension. Re-entry scheduling must not rely only on a timeout. Specify admission/reentrancy behavior before choosing worker concurrency.
- **P06 — Partial effects:** a timed-out handler may still run, mutate private state or finish a tool. Define late-result rejection, cleanup and extension-owned idempotency without claiming exactly-once external work.
- **P07 — Failure atomicity:** commit body, status/headers and relevant metadata consistently; retain previous successful extensions when one fails. Distinguish deliberate restore-original from failure fallback.
- **P08 — Loop counters:** nested depth, sequential tool rounds, cumulative attempts and parallel candidates differ. Decide counter lifetime, atomic updates, cancellation and behavior when tracking is absent or limits disabled.
- **P09 — Completion and terminal errors:** a body can end early, a client can disconnect, or trailers can fail. Distinguish upstream completion, hook completion and actual client delivery; do not record all as success.
- **P10 — Short circuits and errors:** define which response hooks handle local errors, denial, HEAD/no-body responses, 1xx and 101 upgrades; avoid recursive error-hook failures.
- **P11 — Metadata contracts:** producer keys/schema versions, input/output revisions, ownership on child calls, rollback and sensitive-field visibility. Avoid a large schema registry before demonstrated need.
- **P12 — Format compatibility:** two declared-compatible hooks can still conflict semantically. Validate known mismatches and provide stable plan/ledger diagnostics; do not promise automatic proof of arbitrary code.
- **P13 — Ordering syntax:** define required versus optional dependencies and hook-level rather than package-level references. Resolve absent/disabled hooks and cycles without user ordering controls.
- **P14 — Streaming deadlines:** distinguish startup, per-event, idle and total deadlines; a whole operation can otherwise stall through many individually successful slow hooks.
- **P15 — Budget/capture amplification:** retries, candidate calls and recovery buffers multiply resource use. Define parent charging and bounded observation queues; private uninstrumented calls remain outside accounting.
- **P16 — Shutdown and updates:** pin registrations/configuration across a parent and children; stop admission, drain/cancel, release retained bodies and close workers in a defined order.
- **P17 — Optional protection semantics:** an installed approval/auth feature can intentionally block on failure; ordinary decoration can continue from retained input. Expose that distinction without imposing application policy in transport.
- **P18 — Developer ergonomics:** exact Go/TypeScript/worker APIs, body ownership, iterator cancellation, error types and hook return values remain uncommitted. Start with examples and contracts, then implement the smallest interface they need.

## 9. Implementation sequence and observable gates

1. Define the smallest exchange/handler result contract and developer ordering. Prove a no-op handler, a request edit and a response edit; preserve the existing no-extension transport suite.
2. Prove retained-input fallback with two successful transforms around one failing transform, then timeout/late-result behavior. Choose recovery units before promising stream rollback.
3. Add a mock broker plus independent recorder and response decorator. Verify per-request metadata survives concurrency, quota display appears once, and original evidence stays unchanged.
4. Add local-response path switching and response-triggered child requests through the full pipelines. Exercise HTTP auth reuse, lineage, repeated-entry pass behavior and the one-slot nested-call deadlock case.
5. Prove one protocol/client streaming slice, retained-body modifiers, errors, truncation and bounded backpressure. Add an actual worker boundary only after these semantics are stable; repeat relevant tests across it.
6. Add optional tool coordination and durable recall with mixed-tool IDs, restart/replay and concurrent revisions.
7. Add a second provider, dynamic routing/candidates and compatible state migration; validate policy extensions only where configured. Defer unrelated sandbox/dashboard/framework features.

Use unit tests for pure decisions/encoding, socket tests for streaming and dispatch, and process/worker tests for lifecycle, failure and recovery. Combination tests are essential; fifty isolated green checks do not establish composition. No new runtime tests are needed for this documentation-only update.

## 10. Progress log

- Transport milestone implemented; earlier race-enabled Go tests and vet passed at the reviewed revision. This does not establish extension support.
- R01–R50 router research and proposed engineering patterns recorded.
- Six architectural decisions broadened to extension-owned applications and optional coordinators.
- Targeted Pi/Broker source reads recorded with pinned citations; no universal compatibility claim.
- Latest decisions consolidated: remove separate checks, reuse proxy auth over ordinary HTTP, preserve transform input for fallback, and re-enter the full opposite-direction pipeline.
- Next planning gate: settle P01–P05 and minimal hook outcomes/body ownership with concrete examples before implementation. Keep the rest as tracked follow-up work, not a reason to build a speculative framework.
