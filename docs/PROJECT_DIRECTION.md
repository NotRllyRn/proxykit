# ProxyKit: project direction and architecture waypoint

Created: 2026-11-07. Status: founding brief, not an implemented specification.

## Purpose

Build a customizable HTTP proxy core whose extensions can supply account brokering, context management, memory, skills, policy checks, gateway tools, and analytics. The core should remain useful without those features. AI-aware behavior should be an optional layer over generic HTTP transport.

This document consolidates the founding conversation, an attached earlier Codex Broker design conversation, and public Context Mode Gateway documentation reviewed during that discussion. Conversation excerpts below are paraphrased to preserve intent without publishing the raw private thread or personal details. External implementations have not been independently validated by this project.

## Founding intent, paraphrased

> At its core, this is an HTTP proxy with a customizable extension layer. Developers should be able to modify requests and responses and build account management, context compression, and telemetry on top, without recreating the proxy each time.

> A broker extension could do what Codex Broker does: choose an available account and forward with the appropriate credential. Other extensions could provide billion-context-style folding, recall, memory, or usage presentation.

> Pi provides an appealing extension experience, but its runtime architecture should not constrain a server that has different responsibilities.

The desired product is a reusable foundation, not merely a renamed Codex-specific gateway. The user explicitly wants flexibility beyond copying Pi's hooks. Development should follow YAGNI, modularity, minimalism, and concise, reviewable changes.

## Immediate scope and authorization

The founding task authorizes creation of this public repository, this brief, and a new T3 conversation to digest the context. It does **not** authorize implementation in that new conversation yet. The new thread must read, acknowledge, and wait for the user's next prompt.

The proposed milestones below are guidance for future work, not instructions to start now. Language/runtime, license, packaging, deployment model, and final public extension API remain decisions to make explicitly.

## Recommendation

Use a small core with typed extension roles. Keep Pi's approachable TypeScript authoring philosophy without making every feature an event handler. A routing decision, a streaming transformation, a gateway-owned tool, and a background memory extraction job have different execution and failure semantics.

One extension package may register several roles:

1. **Transforms:** modify request context, instructions, tool definitions, headers, or responses.
2. **Providers:** supply a capability the host invokes, such as route selection or credential renewal.
3. **Tools:** register model-callable operations executed by the gateway.
4. **Observers and jobs:** collect usage and analytics or extract memory off the streaming path.

A recall extension might register an archive writer, a context transform, and a search tool. A broker extension might provide account pools, quota-aware selection, refresh logic, and usage decoration. The host coordinates these roles rather than forcing each extension to rebuild transport and control flow.

## Responsibility boundaries

### Generic transport core

Own HTTP lifecycle, connection management, cancellation, deadlines, backpressure, bounded buffering, and response commitment. Expose ordinary request/response and streaming access. Unknown AI protocols can remain opaque HTTP; pass-through does not imply AI feature support. WebSocket support is a separate capability, not implied by HTTP or SSE support.

Keep transport correctness and operational limits in the host. "Everything as an extension" does not mean every extension can independently override framing, tenant isolation, accounting, and retry invariants.

### Protocol adapters and client compatibility

AI-aware extensions should work with messages, tool definitions, completed tool calls, and usage through adapters. Adapters parse and encode supported protocols while preserving provider-specific data, signed reasoning, opaque content, and unknown fields where safe. A shared representation must not silently discard details.

Separate protocol knowledge from client-specific behavior: two clients can use the same wire format but differ in history replay, compaction, IDs, and supported tool forms. Compatibility claims should name tested client/provider versions and features.

Expose a deliberate raw escape hatch for specialized transformations. Do not promise that arbitrary raw edits are compatible with semantic transforms; define ownership, ordering, and validation when both are enabled.

### Host-managed execution

The host coordinates ordered transforms, route selection, attempts, upstream responses, tool handling, and final client delivery. It owns retries and continuation budgets, even when extensions supply selection policy or tool implementations.

Gateway-owned tools are advertised to the model and executed inside the gateway. Their results can trigger another upstream model request. Client-owned tools return to the client for execution. Mixed responses require an explicit protocol-valid handoff; never blindly continue past unresolved client tool results.

The host must preserve tool-call IDs and result pairing, reconcile private continuations with subsequent client replay, and account for every upstream call. Cap rounds, elapsed time, output, and spending where configured. Cancellation must propagate through the entire exchange.

### Shared services

Provide scoped durable storage, credential references, configuration, attributed logging, usage records, lifecycle management, and job execution as needed by concrete extensions. Keep credentials out of unrelated transforms. Credential handles are an API boundary, not protection against unrestricted trusted code running as the same OS user.

A broker extension can own account pools, renewal behavior, quota policy, and account selection while using host credential storage and forwarding primitives.

## Architectural choices to make early

### 1. Explicit composition instead of accidental filename ordering

Use a small set of named execution stages and explicit dependencies where necessary. Validate ordering at startup; reject cycles and unsupported combinations. Do not begin with an elaborate general-purpose workflow engine.

Multiple extensions can contribute instructions or memory. Initially select one authoritative context-compaction strategy per route/session. Two independent compressors can invalidate one another's message mappings, archive IDs, and summaries. Define precedence for local responses and competing route providers.

### 2. Transcript and working context are different things

The client can retain its complete transcript while the proxy sends a smaller working context upstream. Reapply durable transformations when the client resends the original history. Store mappings among originals, replacements, archive IDs, and context revisions.

Archive successfully before removing material from the model's view. A restart must not lose the only way to interpret reduced context. Durable updates need concurrency control; a few process-local JSON values are insufficient for distributed context ownership.

Prefer explicit native thread and turn IDs scoped to an authenticated identity. Cache affinity, IP addresses, and similar prompts do not reliably identify a conversation. If continuity is unknown, do not merge unrelated conversations heuristically by default.

Requests using provider-held state, such as `previous_response_id`, need explicit support. Never strip references without reconstructing the referenced context or selecting a valid alternative mode.

### 3. Client compatibility has levels

- **Transparent mode:** ordinary clients keep their own history; the proxy consistently transforms the upstream view.
- **Client-specific compatibility:** tested adapters handle particular compaction endpoints, tool formats, or replay behavior.
- **Optional client integration:** a plugin or companion can adopt context revisions, expose local tools/UI, and coordinate compaction.

An ordinary model response cannot universally tell clients to replace their saved history. Reducing upstream tokens also cannot universally disable local auto-compaction. Do not falsify usage to influence client behavior. Context size, historical transcript size, and actual provider usage are different measurements.

### 4. Failure policy follows responsibility

- Optional usage decoration may fail open.
- Required protection checks should fail closed for the protected operation.
- Archive failure must prevent the associated context removal.
- Routing or credential failure needs a valid alternative or an explicit request failure.

Operators control which roles are required. An extension cannot silently downgrade a required policy. Payload rollback cannot undo external side effects, database writes, or bytes already delivered.

### 5. Streaming requires ownership and commitment rules

Once delivered, bytes cannot be retracted. Complete-tool-call validation may require buffering the tool item; whole-answer rewriting may require buffering the entire answer. Preserve agreement between deltas, completed items, and terminal response objects.

Avoid copying full history through worker IPC for every token. Parse once where practical; let extensions subscribe to the semantic units they need. Preserve backpressure and bounded memory. Never automatically retry after visible output or side effects without a defined safe continuation strategy.

### 6. Workloads need different budgets

Fast transformations, archive reads, credential refresh, model continuations, and background extraction need different deadlines. Use bounded concurrency, request cancellation, and serialization scoped to the state that requires it. Avoid globally serializing every request through a slow extension.

Background jobs need explicit queueing and shutdown semantics. Account for duplicate delivery and retry where side effects matter. Add this infrastructure incrementally, driven by an actual extension.

### 7. Extension management should be simple and explicit

Start with trusted TypeScript packages if that remains the selected authoring language. Use versioned manifests/API contracts, declared roles and dependencies, configuration schemas, pinned versions, and startup validation. Keep active requests on a stable extension/configuration snapshot during updates.

Define process, request, conversation, and job lifetimes. Restart isolation does not equal a security sandbox. Filesystem/network capabilities only become enforced security boundaries with a runtime or OS mechanism that actually restricts them. Do not claim a manifest alone provides isolation.

Defer marketplaces, arbitrary dashboard plugins, hot reload, and untrusted-code hosting until needed. Do not choose a complex sandbox or microservice architecture solely for theoretical extensibility.

### 8. Keep accounting authentic

Record original upstream usage before presentation rewrites, including hidden gateway continuations and retries. Distinguish account quota snapshots from request token usage. Cache reuse predictions are estimates; provider-returned cached-token counts are evidence after a request. Missing usage is unknown, not zero.

Cached tokens still occupy context. Context rewriting can reduce token volume while hurting prefix-cache reuse. Stable layouts and deliberate rewrite boundaries matter. Savings against a hypothetical unmodified request are estimates, separate from billed usage.

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

Those boundaries suit traffic customization within Broker. For ProxyKit, account selection needs a provider role, gateway tools need a shared execution loop, durable context needs stronger storage contracts, and failure/concurrency policy needs to vary by responsibility.

Keep useful abstractions and behavior tests. Do not assume a wholesale rewrite or wholesale source copy is necessary. Check the source license and current implementation before porting anything.

## Suggested sequence after explicit implementation authorization

1. Agree on minimal transport/runtime choice, extension trust model, initial protocol, and a concrete end-to-end acceptance scenario.
2. Establish a small core and routing-provider boundary. Demonstrate a broker-like provider without baking account policy into transport.
3. Add a host-managed gateway-tool interface. Prove one internal search tool alongside ordinary client tool calls.
4. Implement one durable archive/reconciliation extension, including restart and replay behavior.
5. Add skills/instructions and background memory extraction using the same contracts.
6. Add policy enforcement and a second protocol/client to test that abstractions generalize.

Avoid implementing every possible extension point in advance. The decisive test is whether a broker extension and a recall extension coexist without either reimplementing streaming, retries, transport, or the model loop.

## Acceptance criteria to retain

- No-extension behavior is valid ordinary proxy behavior for supported transports.
- Repeated client history, concurrent sessions, retries, and restarts do not corrupt context mappings.
- Unknown identity does not leak or blend another conversation's state.
- Gateway and client tool calls retain valid IDs, results, and ordering, including mixed responses.
- Streaming deltas and final objects agree; cancellation and backpressure work.
- Required policies cannot silently fail open; archive failure cannot discard context.
- Real upstream accounting includes internal calls regardless of visible response rewriting.
- Provider-held state and signed/opaque fields are preserved or handled through an explicit supported path.
- Tests inspect both what clients receive/persist and what they send on the next request.
- Compatibility reports identify actual tested versions and missing checks.

## Open questions for the next conversation

- Preserve Go plus Node, or choose another implementation after evaluating real requirements?
- Is the first deliverable a standalone generic proxy, or a minimal AI gateway on a reusable HTTP core?
- Which initial protocol/client combination proves the design with the smallest scope?
- What is the initial storage topology: one instance with SQLite, or shared state across replicas?
- Which identity signals are required for durable context features?
- How are route providers selected and their credential capabilities isolated?
- What is the first supported model-tool continuation contract and replay strategy?
- Which policies may fail open, and how does an operator mark a role as required?
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

No implementation, live provider compatibility test, performance benchmark, or security audit has been performed for ProxyKit. This document is the waypoint for deciding and building those things.
