# Extension composition: freedom, predictable stages and streaming

Updated 2026-11-08. **User priorities are current direction; API names, stage layout and defaults below are recommendations, not implemented or finally accepted contracts.** This note supersedes earlier prescriptions for operator-controlled ordering and mandatory destination-credential enforcement. The R01–R50 research remains evidence about capabilities, not a mandate to implement every safeguard in the core.

The [consolidated extension plan](EXTENSION_PLAN.md) is the current planning entry point. It removes the separate check callback, clarifies internal HTTP authentication reuse, and tracks saved-stream fallback and bidirectional pipeline re-entry.

## User priorities and responsibility

- Trusted extensions have broad control over routing, credentials, content, application services and orchestration. ProxyKit does not mandate a provider allowlist or validate that an extension's chosen credential belongs to its destination. Credential misuse and extension-authored policy/content errors are extension defects. Optional protection extensions may enforce stricter rules.
- Users should enable extensions without manually solving their execution order. Developers declare where their hooks belong and what they need. Diagnostics should identify incompatible hooks rather than blaming users for an unexplained broken response.
- Defaults should avoid stalled requests and excessive resource use. Users can increase or disable configurable deadlines and limits. Removing them removes the corresponding guarantees. Unrestricted trusted code is not sandboxed.
- ProxyKit remains responsible for its own dispatch, framing, hook invocation, state separation and documented API behavior. Assigning extension bugs to developers does not excuse host bugs or make arbitrary incompatible transformations compose.
- Keep original intent, editable working content, extension metadata and execution history distinct. Metadata stays internal unless an extension explicitly chooses to publish it. No automatic ledger or internal tracing headers sent to providers/clients.

These priorities authorize documentation/design changes, not extension implementation. No external accounts, production services or gateway binaries were exercised for this note.

## Recommended composition model

Use **hook registrations**, not one type for an entire extension. One package can register a request transformer, resolver, response decorator and observer. Each registration declares its stage, input format/body needs and supported outcomes. Capability declarations describe API behavior; they are not security permissions for trusted code.

A small initial stage sequence:

1. **Request preparation:** operations that must see an incoming request even if a cache/local extension later answers. Authentication, when installed, is a hook with explicit ordering dependencies here.
2. **Resolution:** a hook can return a local response or pass. The first response wins; later resolvers and upstream-only preparation are recorded as not reached. Routing/coordinator extensions can use this boundary. Hooks that merely transform request input belong in preparation, not an unrestricted response-producing transform.
3. **Upstream preparation and dispatch:** only when resolution did not supply an answer. Apply the chosen route/credentials and attempt-specific transforms; record actual dispatches. Retries are new attempts, not a second client request.
4. **Response start:** every selected response, including local/cache answers, supplies headers, provenance and applicable metadata before delivery. Decide its body processing plan before consuming/committing it.
5. **Response body and completion:** declared stream or buffered transforms, with protocol adapters where required; the host writes output. Completion is for observations/cleanup or supported terminal stream events, not retroactive replacement of committed bytes.
6. **Finish:** terminal outcome/ledger observation and cleanup, including error/cancellation paths. A transport failure is not disguised as a successful model answer.

Original attempt observation is separate from final delivery stages. A coordinator can make multiple attempts but selects one client-facing response. Transport errors can produce an error response through response-start where still possible; after commitment, termination and finish are the available paths. Upgrades and informational responses need explicit contracts, not an assumption that every path has an ordinary body.

An extension that needs both cache hits and misses registers preparation or response hooks. One that only needs upstream requests registers upstream preparation. This eliminates ambiguous “after early response”: a response either exists and proceeds to response stages, or dispatch remains necessary. No stage named “anywhere”; give common hooks a sensible default stage.

Within a stage, use stable hook IDs as the default tie-breaker and narrowly scoped developer `before`/`after` declarations when ordering really matters. Required dependencies differ from optional ordering if another hook is installed. Reject cycles, contradictory stages and missing required capabilities with names and reasons before activation. Do not silently disable an existing installation when a new extension conflicts. Users need no ordering controls.

The proposed positive-then-negative priority scheme is deterministic if specified, but arbitrary numbers cannot express “needs broker quota metadata” and invite competing “run first” priorities. Prefer semantic stages and a small startup dependency sort; no dynamic workflow engine. Before/after declarations cannot refer to nonexistent future lifecycle events or override stage boundaries. Optional references to absent hooks impose no order; required references prevent activation. Mutual dependencies are a clear error.

Two independent resolvers can coexist with documented first-response-wins order; intentionally exclusive capabilities can declare conflicts. This does not discover unknown semantic conflicts. Provide an explainable execution plan and ledger. Stable ordering improves reproducibility, not universal compatibility. Test extensions both alone and in representative combinations.

## Handler decisions and ledger

The separate match/check callback proposal is withdrawn. Invoke the applicable handler directly with its input/context; it may immediately return unchanged/pass, optionally reporting a reason. There is no check-result cache or check-passed flag. Declared stages and formats still determine applicability.

Record host-authored ledger entries: request/attempt ID, extension/hook ID, stage, input/output revision, pass/change/local-response/child-request outcome, not-reached reason, failure/timeout and elapsed time. “Changed” describes a returned outcome, not an automatically verified byte diff. Stream hooks need not create a ledger entry per byte. Bound and redact diagnostic history; expose truncation. Persistence/export remains extension-owned.

Ledger entries are read-only through the normal API; namespaced extension metadata is separate. The host retains the input needed for the selected recovery guarantee, discards failed candidate output and continues from the prior accepted state. See the consolidated plan for event versus whole-body recovery and the still-open retention defaults.

## Originals, versions and diffs

Keep original request metadata and per-attempt original response metadata. Keep working content separately. Body preservation must be explicit: copying a request object does not copy its stream. A downstream extension cannot recover bytes that have already been discarded merely by requesting an older revision.

Start with opt-in bounded original body capture and the current working body. Full history, intermediate snapshots and reconstructable diffs are optional debugging/archive capabilities, not the default fast path. They require retaining data before it is consumed. Expose “not retained” honestly; declare body/history needs before processing starts.

Patches can save space for some JSON edits, but arbitrary replacements, binary bodies and compressed streams can produce patches as large as the original. Computing/replaying diffs costs CPU, increases complexity and retains sensitive content. A debug mode may store snapshots or extension-provided patches with byte/time/retention limits; it must distinguish reported patches from verified reconstructions. No universal minimal diff engine is proposed.

An extension can restore the original body if retained and not yet committed. It is responsible for restoring compatible headers, protocol state and related metadata. Read-only original views protect against accidental API mutation, not malicious native code.

## Request identity is not conversation identity

The host creates an exchange context when it accepts an HTTP request and keeps it with the upstream call, response processing and finish. Broker metadata written during the request remains in that context when the response arrives. Five concurrent requests have five contexts. HTTP/1 request association and HTTP/2 stream association already link responses to requests; no provider-supplied conversation ID is needed for this.

Use a host request ID, individual attempt IDs and parent/root IDs for participating internal calls. Thread/session IDs are a separate optional, adapter-extracted and identity-scoped field for linking multiple client requests over time. Do not assume universal header names, infer identity from similar text, or merge unknown sessions. Durable recall may require a client companion or reject unsupported continuity; ordinary forwarding and same-exchange metadata do not.

## Ordinary HTTP internal calls and loops

Keep the ordinary HTTP endpoint. A suggested convention carries root ID, parent request ID, originating extension ID and nesting depth in reserved internal headers. An optional small helper populates these headers, propagates deadlines/cancellation where possible and uses any normal HTTP client. It is not a second inference endpoint. Strip internal headers before forwarding upstream or emitting client responses. Do not serialize the full ledger/metadata into them.

Same-machine source addresses cannot prove extension origin. Unmarked calls are indistinguishable from ordinary clients; marked calls are cooperative claims unless an optional authenticated local context token validates them. A marker is never permission to bypass authentication. Missing/malformed lineage must not silently grant trust. The internal path also lets extensions reuse the configured broker/upstream authentication without maintaining provider credentials. Internal caller authentication and context inheritance remain explicit open decisions in the consolidated plan. The helper can increment depth, but an extension making private HTTP calls can omit/reset it. Transparent universal loop detection is not promised.

Depth counts nested requests, not total sequential model calls, tool rounds or parallel fan-out. Ten sequential calls can all have depth one. Tool rounds belong to the coordinator; model-attempt totals require a shared root counter for participating requests. Reserve/increment atomically if that counter enforces a configured limit. Private uninstrumented calls remain outside it. An extension can inspect lineage inside its handler and immediately return unchanged on re-entry or choose another behavior. Root cancellation cannot be propagated across arbitrary HTTP callers that ignore the convention.

Recommend configurable concurrency/queue limits and a 30-second default budget for one handler invocation, adjustable or disabled by users. A long stream's total lifetime is a separate timeout from hook startup or per-event work; do not accidentally kill every long answer after 30 seconds. An overall chain deadline can prevent many individually slow hooks adding up to a stalled client; its value remains to be chosen. Conventions guide recursive work; host invocation/admission limits supply only the protection actually enforceable at that boundary.

## Timeout and optional protection policy

A plain example of a mandatory check is “this request may not leave the machine until an installed approval extension approves it.” If that feature is enabled as blocking, crashing cannot mean approval. ProxyKit need not impose that policy itself. Ordinary customization can default to continue-on-error; a protection extension can declare abort-on-error. Surface the difference clearly. Timeout overrides must not pretend a skipped approval was successful.

Safe default continuation requires a handler to return a replacement before it becomes the working value. Discard late results after timeout. A handler should not mutate shared working state while another handler proceeds. Unrestricted in-process code still cannot be forcibly stopped by cancellation; separate workers are needed for hard termination. External side effects cannot be rolled back by discarding a return value.

Skipping after timeout is safe only if the prior body/state is still usable. A streaming handler that has consumed unreplayable bytes or emitted half a transformed event cannot always be removed midstream. Before final commitment, use retained input or return a clear error. After commitment, terminate correctly if continuation is impossible. Never concatenate a different answer or invent a successful ending. Disabling deadlines explicitly permits indefinite waiting; there is no honest promise of both unlimited extension control and guaranteed timely output.

## Streaming and whole-body work

An HTTP body is a sequence of bytes. Network read chunks are arbitrary: one JSON/SSE event may span reads and one read may contain several events. An SSE adapter reconstructs complete events before semantic hooks run. It can then expose message-start, text-delta and message-end behavior. Ordinary HTTP has no universal “assistant answer” concept.

At response start, choose the plan based on enabled, applicable hook declarations:

- Metadata-only hooks inspect/change headers and exchange metadata before commitment. They do not need the whole body.
- Stream transforms process chunks or declared protocol events incrementally. Per-exchange/per-item state supports “insert once.” The host writes accepted output with backpressure.
- Whole-body modifiers require bounded capture before their output is delivered. If enabled, the response is buffered/spooled before client delivery; latency is explicit. Stream stages can process into that buffer, or after a buffered transform if their declared order requires it. Revalidate format after replacements.
- Whole-body observers can inspect a bounded copy after streaming and cannot rewrite the delivered answer. They must declare capture limits and incomplete/truncated behavior.

“Replayable” means retained for rereading; it does not by itself mean “must wait for completion” or imply permission to mutate. Separate body access needs from timing and outcome capabilities. A whole-body modifier is not universally last: decompress/parse/transform/encode relationships may require different order.

Do not silently skip a selected whole-body modifier to preserve streaming. If a required input exceeds limits, apply its documented error policy; safe optional bypass requires enough retained input to continue correctly. Infinite streams cannot be fully buffered; incompatible declarations should be rejected or explicitly scoped out. A local response follows the same plan as an upstream response.

Once final output is committed, whole-answer replacement is impossible without a client-supported protocol. Candidate selection must happen before that point; canceling losers belongs to the coordinating extension. The host manages delivery, not an arbitrary callback writing directly to the client. A raw transport takeover can be an explicit advanced capability later, with ordinary hook guarantees no longer claimed on that path.

## Source evidence: Pi and Codex Broker

Targeted source reads on 2026-11-08; no new runtime tests. Pi checkout at `6e2222a8dda06e3d69fad46d630f152f5873567a`; Broker checkout at `42a931e15daaaa1540958ebdc283c2a48acfcb8b` (reviewed tracked files unchanged). These are scoped findings, not a claim to have reviewed every extension or proven runtime compatibility.

- Pi [runner.ts](https://github.com/badlogic/pi-mono/blob/6e2222a8dda06e3d69fad46d630f152f5873567a/packages/coding-agent/src/core/extensions/runner.ts#L1312): `emitContext` clones initial messages and chains handlers. It distinguishes conversation transforms from full-system transforms. Later full-system hooks can still remove essential state; it reports the problem while honoring output. Arbitrary transformations are not universally compatible.
- Pi [input handlers](https://github.com/badlogic/pi-mono/blob/6e2222a8dda06e3d69fad46d630f152f5873567a/packages/coding-agent/src/core/extensions/runner.ts#L1532): transform results chain; `handled` stops later handlers. [Tool results/calls](https://github.com/badlogic/pi-mono/blob/6e2222a8dda06e3d69fad46d630f152f5873567a/packages/coding-agent/src/core/extensions/runner.ts#L1197) have specific merge/block behavior. [Duplicate tools](https://github.com/badlogic/pi-mono/blob/6e2222a8dda06e3d69fad46d630f152f5873567a/packages/coding-agent/src/core/extensions/runner.ts#L636) use the first registered name; shortcut conflicts also have explicit handling. Borrow clear event contracts, stable snapshots and diagnostics, not an unsupported “extensions never conflict” claim.
- Broker [accountusage](https://github.com/NotRllyRn/codex-broker/blob/42a931e15daaaa1540958ebdc283c2a48acfcb8b/extensions/accountusage/extension.go) prepares a quota notice and removes prior decoration on replay through its Responses-specific helper. [Native wrapper](https://github.com/NotRllyRn/codex-broker/blob/42a931e15daaaa1540958ebdc283c2a48acfcb8b/internal/proxyextension/builtins.go#L46) builds the notice from the supplied account snapshot.
- Broker [streamNotice](https://github.com/NotRllyRn/codex-broker/blob/42a931e15daaaa1540958ebdc283c2a48acfcb8b/internal/responsesproxy/notice.go#L225) parses SSE frames, tracks item IDs and `started` state, prefixes the first qualifying text delta once per answer item, and updates completed text/items/final response to agree. If final-answer phase is not known, it buffers the candidate message within a limit. Original terminal usage is observed before upstream/client event transformations. This is protocol-specific logic, not one prefix per byte or a generic HTTP capability.

## Next decisions and acceptance examples

No new R51+ numbering yet: these refine composition, lifecycle and observation requirements, while ledger/check/history features need reconciliation with the catalog before formal numbering. Do not silently alter historical router support scores to claim support for the new proposals.

Prove: cache hit skips upstream hooks but runs final decoration; a handler returning unchanged records its reason and continues to the next handler; request metadata survives concurrent response delivery; a timed-out late result cannot change working content; an unbuffered stream is never falsely restored; a buffered rewrite prevents early delivery; one usage prefix appears per intended answer item; cycles/missing required capabilities explain activation failure; cooperative recursive HTTP calls carry lineage without leaking headers; disabled limits are reported honestly. Add these tests with implementation, not empty tests now.
