# ProxyKit: project direction and architecture waypoint

Updated: 2026-11-08. Status: direction and historical research. Transport, managed native execution, installed Node hooks and optional application examples are implemented; bounded local installed-worker and combined-system acceptance tests pass. See the [implementation tracker](IMPLEMENTATION_TRACKER.md) and linked package contracts for tested scope. Historical proposals below are not a claim that every capability is implemented.

The authoritative [six extension architecture decisions](research/2026-11-extension-audit/extension-requirements.md) cover R01–R50 under the extension-owned application model. Read the [core readiness review and staged gates](CORE_READINESS.md) before implementing extensions. Historical router evidence does not override these ownership rules.

The [composition design note](EXTENSION_COMPOSITION.md) records the latest priorities: broad trusted-extension freedom, developer-defined ordering, internal metadata/ledger, handler-local pass decisions and user-adjustable limits. Its recommendations refine the six decisions; earlier credential enforcement and operator ordering prescriptions are superseded.

Start implementation planning with the [consolidated extension plan](EXTENSION_PLAN.md), which records accepted decisions, recommendations, open cases and progress. Separate check callbacks are withdrawn; saved-input fallback and internal HTTP authentication reuse are explicit goals.

## Purpose

Build a customizable HTTP proxy core whose extensions can supply account brokering, context management, memory, skills, policy checks, gateway tools, and analytics. The core should remain useful without those features. AI-aware behavior should be an optional layer over generic HTTP transport.

This document consolidates the founding conversation, an attached earlier Codex Broker design conversation, and public Context Mode Gateway documentation reviewed during that discussion. Conversation excerpts below are paraphrased to preserve intent without publishing the raw private thread or personal details. The founding external claims below are historical. Later targeted source audits and local probes are recorded in the [engineering reference](EXTENSION_ENGINEERING_REFERENCE.md); they do not certify complete external implementations.

## Founding intent, paraphrased

> At its core, this is an HTTP proxy with a customizable extension layer. Developers should be able to modify requests and responses and build account management, context compression, and telemetry on top, without recreating the proxy each time.

> A broker extension could do what Codex Broker does: choose an available account and forward with the appropriate credential. Other extensions could provide billion-context-style folding, recall, memory, or usage presentation.

> Pi provides an appealing extension experience, but its runtime architecture should not constrain a server that has different responsibilities.

The desired product is a reusable foundation, not merely a renamed Codex-specific gateway. The user explicitly wants flexibility beyond copying Pi's hooks. Development should follow YAGNI, modularity, minimalism, and concise, reviewable changes.

## Immediate scope and authorization

The initial context-only handoff and subsequent readiness-only phase are historical. The user has now authorized extension implementation, including GitHub CLI installation and manager/worker implementation and review groups. Current progress and unresolved acceptance gates are tracked separately.

The milestones remain acceptance gates, not instructions to turn every optional capability into a mandatory core subsystem. Go owns transport and execution; trusted Node ESM workers provide the initial JavaScript/TypeScript authoring boundary. Installation requires explicit trust and server restart for activation.

## Recommendation

Use a small core with typed extension roles. Keep Pi's approachable TypeScript authoring philosophy without making every feature an event handler. A routing decision, a streaming transformation, a gateway-owned tool, and a background memory extraction job have different execution and failure semantics.

One extension package may register several roles:

1. **Transforms:** modify request context, instructions, tool definitions, headers, or responses.
2. **Providers:** supply a capability the host invokes, such as route selection or credential renewal.
3. **Tools:** register model-callable operations executed by the gateway.
4. **Observers and jobs:** collect usage and analytics or extract memory off the streaming path.

A recall extension might register an archive writer, a context transform, and a search tool. A broker extension might provide account pools, quota-aware selection, refresh logic, and usage decoration. Extensions may own complete applications and coordinators. Shared dispatch and delivery let cooperating extensions reuse transport and enforce the guarantees promised for managed traffic.

## Responsibility boundaries

### Generic transport core

Own HTTP lifecycle, connection management, cancellation, deadlines, backpressure, bounded buffering, and response commitment. Expose ordinary request/response and streaming access. Unknown AI protocols can remain opaque HTTP; pass-through does not imply AI feature support. WebSocket support is a separate capability, not implied by HTTP or SSE support.

Keep transport correctness and honest API guarantees in the host. Extensions own application policy and correctness, including destination and credential choices. Resource limits have configurable defaults and may be disabled; unrestricted extension behavior cannot carry unconditional composition or liveness guarantees.

### Protocol adapters and client compatibility

AI-aware extensions should work with messages, tool definitions, completed tool calls, and usage through adapters. Adapters parse and encode supported protocols while preserving provider-specific data, signed reasoning, opaque content, and unknown fields where safe. A shared representation must not silently discard details.

Separate protocol knowledge from client-specific behavior: two clients can use the same wire format but differ in history replay, compaction, IDs, and supported tool forms. Compatibility claims should name tested client/provider versions and features.

Expose a deliberate raw escape hatch for specialized transformations. Do not promise that arbitrary raw edits are compatible with semantic transforms; define ownership, ordering, and validation when both are enabled.

### Shared dispatch and optional orchestration

The execution layer provides ordered transforms, request-local route decisions, attributed managed attempts and final delivery. An extension can own retry and model/tool coordination, or use an optional shared AI runtime. All calls covered by gateway policy/accounting must use managed dispatch; private sockets opened by trusted code are outside that guarantee.

The coordinator must preserve tool-call/result IDs, stop for unresolved client tools, reconcile private continuations with replay when needed, and obey cancellation and shared budgets. These obligations follow the feature, not a requirement to place the coordinator in the generic HTTP core.

### Extension-owned services

Extensions may own enrollment listeners, dashboards, account catalogs, credential stores, databases, transactions, jobs and independently supervised services. No corresponding host endpoint or database API is mandatory. Host conveniences can be added for a concrete need. A companion service still needs a gateway adapter with sufficient interception/control and trusted context.

Keep credentials out of unrelated transforms at the API boundary. Unrestricted trusted code sharing an OS identity is not isolated by credential handles or manifests.

## Six architectural decisions

The [canonical six decisions](research/2026-11-extension-audit/extension-requirements.md) replace the founding list of eight considerations:

1. **Routing and credentials:** extension selection policy, shared attempt execution, extension-owned credentials and distinct requested/served routes; destination policing is optional.
2. **Orchestration and tools:** optional extension or shared-runtime ownership, with valid tool handoff, budgets and managed inference.
3. **Conversation state:** extension-owned persistence, archive-before-reduction, explicit identity/revisions and supported replay/migration.
4. **Observation and mutation:** original attempt evidence plus final delivery coverage, explicit body modes and commitment rules.
5. **Failures and resources:** safe continuation or explicit failure, optional blocking protection features, adjustable limits and cooperative cancellation.
6. **Composition and lifetime:** developer-defined stages/dependencies, compatibility validation, stable configuration and setup/drain/cleanup.

These are proposed contracts, not six implemented subsystems. Start with ordinary data structures and a few stages. Defer dependency graphs, live reload and untrusted hosting until required. The 50 rows are a capability catalog spanning core boundaries, optional protocol runtimes and extension applications—not 50 mandatory core modules.

### Compatibility and accounting constraints

Client transcript, upstream working context and provider-held state are distinct. Transparent HTTP interception cannot universally rewrite a client's saved history or disable its local compaction. Client-specific adapters and optional client companions must state tested versions and features. Never strip provider-held references without reconstruction or another supported mode.

Capture actual upstream usage before presentation mutation, including managed internal calls and failed attempts when usage is available. Missing usage is unknown, not zero. Cached tokens still occupy context; quota snapshots, provider usage and hypothetical savings are different measurements.

Preserve protocol-specific signed/opaque data or reject an unsupported transformation. An adapter can translate the response wire format; it cannot promise that a substituted model behaves like the originally requested model. Streaming, unknown protocols and opaque WebSocket forwarding do not establish semantic compatibility.

## What Context Mode Gateway illustrates

Public documentation reviewed during the founding discussion describes a hosted application-aware proxy, separate from its local plugin. These are vendor-documented mechanisms, not audited guarantees:

- **Recall:** retain recent messages plus an index of archived older work. Inject search instructions and tool definitions; answer gateway tools on the server.
- **Search:** SQLite full-text/BM25 search and document-ID retrieval, with bounded indexing and paged reads. Only traffic visible to the gateway can be recorded.
- **Memory:** extract selected facts through rules, background model extraction, or an explicit remember tool. Inject relevant facts into later requests. Keep validity and recording times so changed facts retain history without all remaining current.
- **Skills:** import `SKILL.md` content, match messages against names/descriptions with a relevance check, and inject the selected body. Central prompt distribution does not install local scripts, dependencies, or tools on every client.
- **Context saving:** fold noisy output, preserve archived originals, shorten tool descriptions, and keep prompt prefixes stable. Provider cache markers are protocol-specific.
- **Thinking in code:** execute data processing in a gateway sandbox, or translate a tool into a client's normal shell tool. Send compact results to the model rather than the full dataset.
- **Protect:** inspect model tool calls before client delivery and rewrite blocked calls into harmless refusal-producing calls. Documentation says Claude Code and Codex are enforced, while Pi is recorded only. This does not inspect arbitrary local script behavior and is not an OS sandbox.
- **Instructions:** inject standing text, distinct from learned memory.
- **Analytics:** record observed traffic and usage; estimated savings require an explicit baseline.
- **Session resume:** a local helper reconstructs native client session files. This feature is not solely HTTP interception and does not imply exact full-fidelity restoration.

The model learns about gateway features because the request includes their instructions and tool schemas. No change to model weights is needed. Server-owned tools require a private continuation loop; an ordinary request/response hook is not sufficient by itself.

Documentation has feature-specific limits and can change. For example, Recall and Active Context pages describe different image retention behavior. Do not promise universally lossless archives without testing the exact path and retention/redaction behavior.

## Lessons from the attached billion-context discussion

The earlier conversation reported source review of `ranxianglei/billion-context` at commit `bebc239f2767e1a35c7664755f3ea8cb51faa641` (version 0.1.212), without live validation. Treat these as historical research notes to verify before reuse:

- Persistent compression records are reapplied when unchanged clients resend original history.
- Proxy-owned compress/decompress/search tools run in an internal, bounded model loop.
- Separate protocol adapters handle Responses, Chat Completions, Anthropic, and Google formats.
- Native identity and replay reconciliation matter separately; similarity-based identity cannot distinguish identical independent conversations reliably.
- Client-specific compaction interception can work where a client accepts the returned shape. A reported synthetic plaintext marker in an opaque compaction field was translated by that proxy on replay; it was not a valid provider encrypted object and depended on continued routing through that proxy.
- The reviewed project's ordinary Responses path reportedly stripped `previous_response_id` without reconstructing history. Do not copy that history-loss hazard.
- Unknown protocols being forwarded unchanged does not establish compression compatibility.

Borrow the architectural lessons, not unverified compatibility tricks. Review licenses before copying implementation code. This brief does not choose an implementation dependency or license.

## Starting point in Codex Broker

The existing `feature/proxy-extension-layer` branch is useful prior work, not the implementation of this repository. Its reviewed contract provides:

- Go transport/data plane and trusted Node/TypeScript custom extensions.
- Request, prepared, attempt, response, body, parsed SSE event, and finish hooks; routing/retry observations; settings and logs.
- Native usage-decoration and request-size-compaction extensions.
- One worker per custom extension, request-scoped factories, filename/registration ordering.
- Process-local `updateThread` state; extension data directories allow separately implemented durable storage.
- A 100 ms handler budget and serialized invocations through each extension worker.
- Fail-open extension isolation.
- Host-owned authentication, account selection, credential injection, and failover.
- No host-managed gateway-tool loop or general agent-session ownership; non-Responses protocols are opaque and WebSocket upgrades are rejected.

Those boundaries suit traffic customization within Broker. For ProxyKit, account selection needs a provider role, gateway tools need an optional coordinator with valid continuation contracts, durable context needs extension-owned persistence plus sufficient identity/replay hooks, and failure/concurrency policy needs to vary by responsibility.

Keep useful abstractions and behavior tests. Do not assume a wholesale rewrite or wholesale source copy is necessary. Check the source license and current implementation before porting anything.

## Suggested sequence after explicit implementation authorization

The transport milestone is complete within its [declared scope](TRANSPORT_DECISION.md). Follow the [readiness gates and R01–R50 traceability](CORE_READINESS.md) for the extension layer:

1. Prove a small native Go execution boundary with explicit failure behavior, request-local routing, a mock broker and an independent recorder.
2. Add one protocol/client slice with explicit streaming, replay and delivery contracts; then settle the ergonomic extension/worker interface.
3. Add optional tool coordination and durable recall, with mixed tools, restart and concurrent replay checks.
4. Add provider substitution and bounded dynamic routing using those same dispatch/delivery boundaries.

Enrollment, storage and jobs stay in extensions. Add adapters and convenience APIs only when a real extension needs them. Failure behavior is part of the first slice. Mandatory protection is an optional feature, not universal destination enforcement.

## Acceptance criteria to retain

- No-extension behavior is valid ordinary proxy behavior for supported transports.
- Repeated client history, concurrent sessions, retries, and restarts do not corrupt context mappings.
- Unknown identity does not leak or blend another conversation's state.
- Gateway and client tool calls retain valid IDs, results, and ordering, including mixed responses.
- Streaming deltas and final objects agree; cancellation and backpressure work.
- An explicitly blocking protection feature cannot report failure as approval; context extensions must not discard context on archive failure.
- Real upstream accounting includes internal calls regardless of visible response rewriting.
- Provider-held state and signed/opaque fields are preserved or handled through an explicit supported path.
- Tests inspect both what clients receive/persist and what they send on the next request.
- Compatibility reports identify actual tested versions and missing checks.

## Open questions for the next conversation

- What is the smallest extension authoring/worker contract on the accepted Go core, and when is Node IPC justified?
- Which first broker/policy/recorder slice proves the shared execution boundary?
- Which initial protocol/client combination proves the design with the smallest scope?
- What is the first stateful extension’s storage topology: one instance with SQLite, or shared state across replicas?
- Which identity signals are required for durable context features?
- How do routing/credential extensions expose their choices and optional protection contracts?
- What is the first supported optional model-tool coordinator contract and replay strategy?
- Which hook failures permit safe continuation, and how are optional blocking features declared?
- What license and distribution strategy should this new public project use?

## Sources and verification boundary

Public sources reviewed in the founding discussion:

- Context Mode Engine/Gateway distinction: https://contextmode.ai/docs/engine/gateway
- Recall: https://contextmode.ai/docs/recall
- Search: https://contextmode.ai/docs/search
- Memory: https://contextmode.ai/docs/memory
- Skills: https://contextmode.ai/docs/skills
- Saving and code execution: https://contextmode.ai/docs/how-saving-works
- Protect: https://contextmode.ai/docs/protect
- Active Context: https://contextmode.ai/docs/active-context
- Instructions: https://contextmode.ai/docs/instructions
- Session resume: https://contextmode.ai/docs/session-resume
- Local Context Mode source: https://github.com/mksglu/context-mode
- Historical billion-context revision: https://github.com/ranxianglei/billion-context/tree/bebc239f2767e1a35c7664755f3ea8cb51faa641
- Codex Broker source: https://github.com/NotRllyRn/codex-broker
- Broker branch contract: `docs/proxy-extensions.md` and `packages/proxy-extension/index.d.ts` on `feature/proxy-extension-layer`.

The Go transport and extension implementation have local socket/process evidence, with a historical transport benchmark in the README. This architecture brief itself does not establish live-provider compatibility certification, complete R01–R50 acceptance, or a security audit. See the implementation tracker and package evidence for current verification; the readiness review records the original gates.
