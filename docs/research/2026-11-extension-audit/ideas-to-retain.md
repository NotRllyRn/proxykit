# Useful ideas to retain

These are architectural findings, not code selected for copying or an implementation plan. Source revisions are pinned so later work can recheck them.

## From the three audited routers

- **CCR:** typed provider/request/route/response/event roles under one plugin package; source/target protocol adapters; real streaming virtual-model continuations; configurable per-role execution limits; outbox/publisher/delivery-state separation. Preserve the good separation while closing hook coverage, tool identity and replay gaps. See [audit](ccr.md).
- **CLIProxyAPI:** explicit exclusive frontend auth; auth acquisition/refresh as a provider; selection over host-eligible accounts; host model execution with origin-plugin recursion avoidance; separate original usage from response rewriting; schema-versioned payload opt-outs/byte-range patches to avoid copying full histories for every chunk. Add explicit continuation budgets and execution ancestry rather than treating recursion avoidance as a complete loop contract. See [audit](cliproxyapi.md).
- **Bifrost:** account/key-selection interface separate from transport; routing phase before per-attempt hooks; paired reverse-order post hooks; pre-commit response-header hooks; bounded async exporters with visible drop counters; durable job framework with claims/checkpoints. Distinguish observer dropping from durable audit delivery, and expose intended shared services through the actual extension contract. See [audit](bifrost-findings.md).

## Focused billion-context inspection

Target: [ranxianglei/billion-context at cda6e735](https://github.com/ranxianglei/billion-context/tree/cda6e73501e7c29b6e88f657003514987e464f79). **Source/document inspection only**, limited to cooperative-plugin identity, revision guards, status provenance and persistence. No runtime test or whole-project endorsement. Headroom and a separately identified Accordion implementation were not audited; the requested compression behavior is covered in the capability catalog without attributing an uncertain product implementation.

These mechanisms address gaps not supplied as general extension contracts by the three audited routers:

1. **Cooperative client capability negotiation.** [`PLUGIN.md`](https://github.com/ranxianglei/billion-context/blob/cda6e73501e7c29b6e88f657003514987e464f79/PLUGIN.md) specifies `/__bili/plugin/manifest`, real conversation-ID headers, server-provided tool schemas and optional client-native execution. This avoids having the proxy guess identity or inject duplicate tools when a client integration already owns them. Keep this optional; transparent proxy mode still needs its own explicit limits. Documentation is evidence of the proposed/implemented protocol surface, not universal client compatibility.
2. **Revision check under the same session lock as mutation.** [`src/plugin.ts:2273`](https://github.com/ranxianglei/billion-context/blob/cda6e73501e7c29b6e88f657003514987e464f79/src/plugin.ts#L2273) checks `expectedRevision` inside `withSessionLock`, rejects stale calls with 409, and reads the remembered model-visible snapshot under that lock. This is stronger than a loosely shared session object. Distributed ownership/crash guarantees still need their own proof.
3. **Context footprint has provenance.** [`src/plugin.ts:1875`](https://github.com/ranxianglei/billion-context/blob/cda6e73501e7c29b6e88f657003514987e464f79/src/plugin.ts#L1875) and [status output near 2023](https://github.com/ranxianglei/billion-context/blob/cda6e73501e7c29b6e88f657003514987e464f79/src/plugin.ts#L2023) separate context-token estimate/measurement/unavailable, observation time and generation from cumulative billed usage. This directly addresses misleading zero/missing usage and fabricated compaction-savings displays. The status fields were source-inspected, not verified against providers.
4. **Native compaction and forking are explicit lifecycle events.** The cooperative protocol includes snapshot/fork and native-compaction notification. A client shortening its own history is a state transition, not merely another request for the same session. Treat session lineage and retained archive mappings as first-class concepts where a concrete extension needs them.
5. **Atomic file replacement is useful but has a precise boundary.** [`src/plugin.ts:320`](https://github.com/ranxianglei/billion-context/blob/cda6e73501e7c29b6e88f657003514987e464f79/src/plugin.ts#L320) writes a temporary conversation index, fsyncs and renames it. The surrounding code explicitly calls this best-effort, debounces it and ignores write failures. Borrow careful replacement mechanics; do not mistake this particular index write for guaranteed archive-before-removal durability or a distributed transaction.

## Design cautions demonstrated by tests

- A function named “response hook” may not run on every output path. Test ordinary, buffered, streaming, tool-loop, fallback and error paths independently.
- A tool result map keyed by tool name loses repeated-call results. Preserve call IDs and ordered results.
- “Plugin can do HTTP” is not “host accounts for every model call.” Prefer a typed host inference service for internal calls.
- “Context supports cancellation” is not “callbacks stop running.” Bound admission and specify what happens to timed-out work.
- “Has persistent logs” is not “has durable conversation revision/replay.” These serve different invariants.
- Returning an error can mean continue. Required policy must have host-enforced behavior for unexpected failure, startup failure and unload as well as explicit denials.
- A public enrollment page must never receive the host's administrative credential merely because that is the only POST route scope available.

Licenses checked for the primary candidates: CCR and CLIProxyAPI MIT; Bifrost Apache-2.0; packaged CCR gateway declares MIT. Dependency, asset, optional enterprise and proposed source-copy licensing would need a targeted check when choosing actual code to reuse.
