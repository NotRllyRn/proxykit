# Extension architecture: ownership and six core decisions

Updated 2026-11-08. **Current design guidance for R01–R50; proposed extension contracts, not implemented APIs.** This replaces the original host-supplied-services interpretation in this file. Go is selected for the transport core; the extension authoring/worker interface is still to be designed.

Read the [50-requirement engineering reference](../../EXTENSION_ENGINEERING_REFERENCE.md) for router evidence and requirement details, the [current matrix](capabilities-current.json) for classifications, and the [core readiness review and implementation gates](../../CORE_READINESS.md) before implementation. The original 42-row and revised 43-row audits are historical evidence, not current ownership instructions.

The newer [composition design note](../../EXTENSION_COMPOSITION.md) records the user’s priorities and recommendations for developer-defined stages, handler-local pass decisions, internal ledgers, cooperative HTTP lineage and streaming. It supersedes mandatory credential policing and user-controlled hook ordering. Proposed API details still require agreement.

The [consolidated extension plan](../../EXTENSION_PLAN.md) is the current planning entry point and decision log, including internal HTTP authentication reuse and full pipeline re-entry.

## Ownership rule

An extension may be a complete trusted application: its own listener, dashboard, enrollment flow, database, credential store, workers, jobs and orchestration. It may run in process where the runtime permits or as a separately supervised service with a gateway adapter. Those facilities do not require corresponding host subsystems or public host endpoints.

The generic HTTP core owns correct transport. The extension execution layer supplies the shared control and observation boundaries promised for gateway-managed traffic. Optional AI adapters/runtime supply protocol semantics. Feature extensions own application behavior and state. An extension may own its coordinator or use a shared optional coordinator; neither a tool loop nor a context database is mandatory in the generic core.

The 50 requirements are a capability and acceptance catalog, not 50 mandatory core modules. A finite API cannot guarantee every possible extension or every client/provider combination. Unrestricted trusted code can perform private network calls outside gateway accounting and policy; shared guarantees apply to managed dispatch, not arbitrary sockets. Credential handles and registration permissions are API boundaries, not an OS sandbox.

## Six decisions that change whether composition works

### 1. Routing and credentials: extension policy, shared attempt execution

A routing extension may ignore the requested model and choose another provider, model, endpoint, account and supported settings. It may inspect inputs, use quota/cost/latency policy, ask a classifier, inspect an uncommitted response, or compare bounded candidates. Keep requested client intent separate from the effective route.

Shared dispatch accepts one request-local route decision, executes the attempt, propagates cancellation and emits attributed outcomes. Extensions own destination policy, credentials, signing and redirect choices; ProxyKit does not enforce a credential-to-provider mapping or provider allowlist. Sending a credential to the wrong destination is an extension defect. Optional protection extensions may enforce stricter checks. Apply promised dispatch observations to participating fallbacks, nested calls and candidates; private calls remain outside those guarantees.

Extensions own account catalogs, refresh policy/singleflight, eligibility and durable credential storage. Transport owns connection and body lifetime; the coordinator owns retry strategy within shared limits. No mutation of shared proxy configuration per request. Use typed selection/denial outcomes rather than hiding routing failures in header edits.

Relevant requirements: R01–R09, R27, R30–R35, R44–R50.

### 2. Orchestration and tools: optional ownership, common execution rules

An extension can own the model/tool loop, or use a small optional AI runtime. Generic HTTP forwarding must work without either. A local tool handler and an MCP-backed service are both valid implementations; MCP is not mandatory for every tool.

The coordinator preserves ordered tool-call/result IDs, keeps trusted caller/session context separate from model arguments, honors configured budgets and propagates cancellation. It executes only tools it owns, stops for unresolved client tools and reconciles subsequent replay where required. Classifier calls, quality escalation, candidate fan-out and ensembles use the same managed dispatch boundary. Hold speculative side-effecting tools until a result is selected and authorized; do not promise exactly-once external effects without idempotency support.

One shared dispatch primitive is preferable to separate forwarding implementations for every extension. It does not require a workflow engine or a host-owned agent framework.

Relevant requirements: R12, R21–R27, R30, R43, R45–R47.

### 3. Conversation state: extension-owned persistence, explicit consistency

Separate the client's transcript, the upstream working context and provider-held state. Extensions own archives, tenant-scoped storage, revisions, locks and replay reconciliation. One coordinator may combine archive and reduction; a specialized host database/transaction API is optional.

Commit the original material and mapping before relying on reduced context. On archive failure, preserve the original input or deny; a thrown error after mutating shared state is not rollback. Concurrent updates require revision checks or equivalent database-enforced correctness. Unknown identity must not merge conversations.

Before switching providers/accounts, preserve compatible affinity, reconstruct supported portable history/resources, or reject. Never drop a foreign response ID, signed reasoning item or file reference merely to make the request acceptable. State migration is a protocol/extension concern, not opaque transport behavior.

Relevant requirements: R16–R20, R24–R25, R38, R48.

### 4. Observation and mutation: each attempt plus one explicit final delivery boundary

Distinguish original input, effective upstream request, per-attempt response, and selected client-visible output. Define hook coverage across ordinary forwarding, local responses, errors, fallback, internal loops and upgrades. A transport hook does not imply parsed SSE events or WebSocket message access.

Capture actual usage and requested/served route provenance before presentation edits, with missing usage marked unknown. Extensions own storage/redaction/export and cache policy. Optional observers can be bounded and best effort; required audit capture needs an acknowledged durable handoff and explicit failure behavior. Storage cannot recover events lost before its callback.

Final delivery owns headers, framing, trailers, streaming and the commitment point. Cover informational responses and upgrades explicitly. Whole-answer validation/rerouting requires bounded buffering or spooling before delivery; already delivered bytes and external effects cannot be retracted. Protocol adapters preserve client event/tool/error structure, not the originally requested model's semantic behavior. A model alias must not falsify served-route accounting.

Relevant requirements: R10–R15, R28–R33, R42, R45, R47, R50.

### 5. Failures and resources: responsibility-specific policy and bounded admission

No mandatory destination policy is imposed by the generic core. Ordinary customization may continue on failure where prior state is recoverable. An explicitly installed blocking approval/protection feature must distinguish failure from approval. Extensions declare failure behavior; users can configure or disable resource limits. Context extensions remain responsible for archive-before-removal. A timeout cannot undo consumed stream bytes or external side effects; use an error/termination when safe continuation is impossible.

Provide configurable admission, queue, buffer, elapsed-time and applicable call/output/spend limits with conservative defaults; users may raise or disable them. Participating classifiers, retries, candidates and tool continuations share configured parent limits; unmarked private calls cannot be universally detected; refresh and background jobs have separately appropriate limits. Acquire capacity before spawning work, reject overload explicitly, and release capacity only when the underlying work actually ends. Cancellation must stop future admission and propagate to dependencies.

A timeout does not forcibly terminate Go or JavaScript code. Hard resource isolation requires an independently enforceable runtime/process boundary when needed. Spend ceilings need reliable pricing/reservations; missing usage cannot prove a zero charge. Do not add a sandbox or global job scheduler for speculative needs.

Relevant requirements: R17, R26–R27, R32–R36, R40–R41, R45–R46, R49.

### 6. Composition and lifetime: explicit order, stable context, small lifecycle

Start with a few named stages and developer-declared hook placement, with stable hook-ID ordering and narrow before/after dependencies where needed. Do not expose user-controlled ordering. Reject duplicate IDs, contradictory stages, dependency cycles, missing required capabilities and declared exclusive-role conflicts with actionable diagnostics. Select one authoritative routing coordinator and one authoritative compaction strategy per applicable route/session; a coordinator may intentionally use multiple models or internal strategies. Independently loaded unaware plugins do not gain automatic arbitration.

Pin configuration/registrations for the lifetime of active work, including streams and nested calls. Start with startup validation and drain/restart updates. Defer dependency graphs, live reload, marketplaces and arbitrary dashboard embedding until demonstrated needs justify them.

Provide setup/cleanup and an explicit shutdown order: stop admission, drain or cancel active work, then close extension resources and transports. An extension owns its workers and durable recovery; a companion service may be supervised separately. A missing declared prerequisite prevents that configuration from activating. An installed blocking protection feature must not silently turn startup failure into permission.

Relevant requirements: R14, R27, R35–R43, R46.

## Shared boundaries, not a fixed framework

The proposed minimal execution layer comprises request context/identity, ordered role invocation, managed dispatch and final delivery, detached observations, a bounded execution ledger and setup/cleanup. Handlers decide whether to act or pass without a separate check callback. Namespaced metadata, saved-input recovery and explicit body retention are described in the consolidated plan. Adapters/coordinators can live above it. Concrete Go types, the Node IPC contract, error shapes and versioning must be settled by a small working slice, not invented wholesale from the checklist.

Use the existing standard-library transport rather than implementing HTTP framing again. Body access is explicit: metadata-only, streaming, or bounded buffered/replayable. Do not copy every request/history/token across worker IPC by default. Preserve cancellation and upgrade-capable body interfaces through wrappers.

## Feature/API ownership catalog

The following names describe operations, not committed API identifiers or mandatory HTTP endpoints.

| Feature | Gateway boundary needed | Extension-owned implementation |
|---|---|---|
| Broker accounts | Trusted caller, route/credential selection, per-attempt outcome and required policy | Account CRUD, labels/ACLs, quota fetches, refresh coordination, credential store |
| Public enrollment | Publish committed eligibility/credentials through broker integration | Own POST/listener/dashboard, cookies, OAuth/device flow, service auth, transaction/outbox; separate process if isolation is needed |
| Usage monitor | Original/effective observations, IDs, actual usage/provenance, capture semantics | Database, retention, exports, redaction, UI and durable delivery |
| Archive/compaction | Awaited input transform, identity, supported protocol state handling | Archive-before-reduction transaction, search, revisions, locks, replay and recovery |
| Recall/tools | Typed tool ownership/context, managed inference, valid client handoff | Tool implementation, optional MCP service, coordinator and private transcript store |
| Memory/skills | Request transforms and observed events; managed inference if it is to be accounted | Content/fact stores, relevance selection, workers, imports and APIs |
| Protection/approval | Required pre-dispatch/pre-delivery/pre-tool decision and explicit suspension contract if implemented | Policy, approval UI, durable operations and idempotency |
| Code execution | Contextual tool invocation, cancellation, bounded output/artifact references | Separate execution sandbox, file/network rules and artifact service |
| Cache and dynamic routing | Local response, effective route/settings, shared dispatch, final encoding | Cache scope/invalidation, routing algorithms, classifier/ensemble logic, capability catalog |
| Resume/fork/migration | Trusted identity, complete visibility and protocol-specific continuation controls | Lineage/CAS/archive and client-native import/export companion |
| Jobs and quotas | Lifecycle/cancellation and accounted dispatch when needed | Queue/store, leases, claims, retries, counters and job APIs |

No public `/execute-anything` route, enrollment path, dashboard mount, host job API or shared database is required solely because an extension needs the corresponding feature. Add same-port mounting or convenience services only for a concrete integration need.

## Further stress cases

Keep residency/egress checks after route selection; signing after final mutations; reversible redaction before applicable exports; bounded shadow calls with real accounting; and tenant-safe artifact retention. Realtime frame transformation requires an explicit bidirectional protocol layer. Distributed leases, approval recovery, compliance deletion and untrusted-code isolation belong to the extensions/deployments that need them. Their guarantees must be tested when claimed; they are not an instruction to build these systems in the core now.

## Combined acceptance scenario

Combine a broker provider, an independent recorder and an archive/recall extension. Exercise an authorized account fallback, one internal search plus one client-owned tool, replay after restart, and a supported cross-provider reroute before client commitment. Verify archive-before-removal, identity isolation, exact call IDs, installed policy behavior, actual usage including participating extra calls, safe state handling and cancellation under configured concurrency limits.

The earlier router audits did not establish that complete scenario through one stock drop-in plugin contract. Passing it for ProxyKit will require the staged implementation and observable checks in the readiness plan; current transport tests alone do not establish it.
