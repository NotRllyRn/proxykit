# Dynamic routing: coverage and requirement expansion

Date: 2026-11-07. Documentation research; no implementation or new runtime tests in this work. The focused source audit is complete: all three routers are Integration needed for each full R44–R50 composition. This means useful implemented primitives require further wiring/validation, not that dynamic routing is absent. See the [engineering reference](../../EXTENSION_ENGINEERING_REFERENCE.md#r44) and [current 50-row matrix](capabilities-current.json).

## Direct answer

Yes: the intended extension contract permits ignoring the requested model and choosing another model, provider, endpoint, account and supported settings. Existing R02 covers route selection, R03 credentials, R09 input/settings transforms, and R10–R13 output/protocol adaptation. The R02 scope is clarified rather than assigning a new number to basic routing.

For example, an application can request a logical model alias in its usual API format; a routing extension chooses another provider and a supported reasoning configuration; a target adapter constructs that provider's request; a source adapter encodes the answer for the original client. Returning the client's expected protocol is different from reproducing the originally requested model's behavior. A model-name alias is presentation metadata; actual served provider/model and usage remain separately recorded.

The example's model and reasoning labels are illustrative, not verified provider capabilities. In particular, “high” and “low” are not universally interchangeable settings. Tools, JSON-schema output, images, signed reasoning and provider-held conversations may prevent a transparent substitution.

If “checks the response” means inspecting the incoming request, routing can happen before inference. If it means inspecting an upstream model's answer, at least that initial call has already happened. A validator can reroute before delivering the answer, at added latency/cost. Full-answer validation usually requires buffering or spooling with explicit limits. After visible output or irreversible tool actions, silently replacing the answer is not generally valid.

## Already represented

- R02/R03/R09: destination, account, credentials, requested-model override and request parameters.
- R10–R13: response mutation, streaming mutation, loop hook coverage and protocol adapters.
- R19/R20/R24/R25: replay, opaque/provider state, tool pairing and trusted identity.
- R26/R27/R36: cancellation, execution budgets and bounded admission.
- R28–R33: original usage, nested calls, attempts, correlation and durable capture.
- R34/R35: explicit denial and required policy failures.
- R37–R39: ordering, coordinator ownership and configuration lifetime.
- R42/R43: local/cache response and full extension applications.

These primitives do not establish the combined routing guarantees below. The additions are acceptance contracts on shared dispatch/adapters/coordinators, not seven mandatory subsystems.

## New numbered guarantees

### R44 — Negotiate target capabilities and translate model-specific settings

Map reasoning effort/budget, tools/tool choice, structured output, modalities, context limits and streaming when replacing requested model/provider. Validate target support; explicitly reject, adapt, or authorized downgrade; do not equate same field name with same semantics.

### R45 — Reroute after inspecting an upstream outcome before client commitment

Content/quality/schema/tool-call validators, classifiers, refusal/error/rate-limit decisions can accept, retry another target, or transform/reissue. Bound reroutes; all attempts attributed, no automatic duplicate side effects; once client-visible bytes or tools committed, no transparent replacement without explicit continuation protocol. Distinguish HTTP retry vs successful-content reroute.

### R46 — Coordinate classifier calls, candidate fan-out, hedging and result selection

Extension-owned planners call other models then choose route; parallel candidates/races/ensembles/shadow calls via common dispatch, bounded fan-out, cancel losers, record all costs; candidate tool side effects held until selection; no general workflow engine required.

### R47 — Preserve the client protocol contract across provider/model substitution

Roundtrip inbound protocol -> different outbound provider protocol -> original client shape: stream events, final object, errors, stop reasons, tool IDs, structured output. Requested model alias can be presentation alias if contract permits, never falsify served provenance. Do not claim Grok acts semantically identically to requested model or universal client support.

### R48 — Preserve or migrate conversation affinity and provider-held state when rerouting

Switch providers/accounts across turns or attempts without forwarding foreign previous_response_id, signed reasoning, cache handles, file IDs or tool state. Explicit stateless full-history reconstruction or affinity/reject; tenant/session mappings durable extension-owned. Distinct from ordinary context compaction.

### R49 — Bind each route change to destination-specific credentials and authorization

Atomic selection of provider/model/endpoint/account/credentials per attempt; remove old auth, don't leak one provider credential to another URL or redirects; rerun mandatory model/destination/tenant policy for nested calls and fallbacks; configured custom destinations allowed. Logical core guarantee not a sandbox against trusted plugin sockets.

### R50 — Separate requested/served route provenance and partition caches accordingly

Record requested model alias vs actual provider/model/endpoint/account/settings/route decision version for all classifier,candidate,retry calls. Cache key boundaries include tenant, effective target/settings/protocol/tool context as needed; alias change must not cross-contaminate. Preserve truthful accounting even if client-compatible model field aliases original. Extension owns policy/cache, generic dispatch emits provenance.

## Scenarios this enables

- Fixed model override, aliases, tenant-specific defaults, canaries and A/B assignment: R02/R09 plus R47/R49/R50.
- Cost, latency, quota, region or privacy-based selection: extension-owned policy, R02/R08/R27/R34, and R44/R49/R50.
- Cheap classifier selects a specialist; draft → critique → stronger model; JSON repair or quality escalation: R44–R47 plus R26–R33.
- Retry on transport failure, rate limits, invalid output or an extension-defined semantic outcome: R45 plus existing retry, policy and budget boundaries. A transport retry alone is not semantic routing.
- Hedge a slow request, compare candidates, aggregate answers or run an observed shadow candidate: R46, with all candidate usage counted and side-effecting tools held until selection.
- Switch provider mid-conversation or between internal turns: R47/R48 plus durable replay and trusted conversation identity. Affinity or explicit rejection may be the correct result.
- Cache a routed answer or present a stable alias while changing the backend: R42/R47/R50. Cache hits must still satisfy current caller and destination policy.
- Route through an extension-owned local model or another gateway: R02/R03/R13/R49; bound recursive routing and preserve cancellation/provenance.

## Minimal design direction

Keep a small route decision containing target provider/model/endpoint, account/credential reference, effective settings, adapter and a reason. Preserve original client intent separately. Dispatch validates the decision, binds destination credentials, runs required checks and records each attempt. An optional coordinator may call dispatch repeatedly or concurrently within budgets and select a result; one delivery boundary encodes the selected answer for the client.

No built-in semantic router, model classifier, ensemble framework or universal capability registry is required initially. Start with one explicit override across a tested protocol pair, then a bounded pre-commit retry. Routing policy, capability data, scoring, storage and cache policy can all live in extensions.

Current ProxyKit has an accepted Go transport milestone; this research does not implement its future extension host or establish these routing guarantees in that milestone. See [transport decision](../../TRANSPORT_DECISION.md).

