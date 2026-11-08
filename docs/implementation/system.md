# Optional combined broker + AI + durable-original proof

## Implemented slice

`examples/system` is an importable, optional composition of the real Engine,
`brokerapp.NewHost`, `aihost.App`, `ai.HostManaged`, recall store and independent
required original recorder. It is not a second HTTP dispatcher, mandatory core
feature, binary/CLI integration or complete R01–R50 certification. No changes
were made to extension/proxy/CLI/worker/installer/SDK/recall implementation.
Configuration/deployment prerequisites are in `examples/system/README.md`.

### Small explicit integration contracts

- `brokerapp.HostConfig.Ingress` installs trusted middleware **after** caller
  credential stripping, Engine.Bind, broker authentication and request-local
  state installation. The combined composition uses it to install current,
  trusted tenant/conversation claims, then parse AI ingress. Subject comes from
  bearer validation (or trusted bound reentry), never public identity headers.
- `HostConfig.Select(ctx, Input) (Selection, error)` opts into a specific account
  and encoding protocol. `Selection` contains no secret. Account selection still
  rechecks the broker's committed current ACL, activation, endpoint allowlist,
  expiration and refresh state. Its final endpoint **must equal** the effective
  encoded request URL. Unlike default selection, explicit selection does not
  inherit a parent's fallback exclusions and does not initiate automatic
  fallback: logical candidates/reroutes are separate managed calls.
- `brokerapp.RouteEvidence(ctx, Input)` returns actual selected account,
  provider, catalog model, endpoint and protocol from that exchange's private
  state, validating the observed effective request URL. It is not a URL-to-first-
  account guess or owner-metadata/header inference. Default broker selection,
  without a declared protocol, cannot produce this AI evidence.
- `HostManaged.BindTarget` and the corresponding optional `aihost.App` field bind
  a trusted exact target before encoding. The composition rejects tuples absent
  from its explicit catalog and carries account/protocol in a private context.
  This allows two authorized accounts on the **same endpoint** without guessing
  by endpoint or trusting caller account headers. It is native trusted-code
  cooperation, not isolation against malicious Go extensions.
- `HostRecorder.EvidenceContext` supports that request-local provenance while
  preserving the original `Evidence(Input)` API for existing hosts. The combined
  callback compares actual broker account/provider/endpoint/protocol against the
  exact binding; actual response model still comes only from decoded upstream
  output. `Attempt.Request` is the original host's effective method/URL/redacted
  headers; request bodies are inspected on the actual socket in the tests, not
  claimed as retained in `Observe` snapshots.
- `system.New` validates that extra request policies are blocking Prepare hooks
  explicitly before `ai-app.coordinate`. Resolve/Upstream policies would miss
  the root local-response short circuit and are rejected. Engine validation
  still handles dependencies/IDs/stage cycles. `Select`, `Ingress` and target
  binding overrides are rejected rather than silently composing incompatible
  authority. Registrations/configuration remain immutable during serving.
- `system.FileSink` serializes synchronous JSON-lines writes, file fsync and
  directory fsync before acknowledging an original batch. Original sinks remain
  independent of presentation/selected responses. Failures return to the managed
  call, leaving uncertain work quarantined before new tool effects/delivery.
  It requires a private local directory and one process owner; partial-write,
  retention/capacity and reconciliation remain operator duties.

All model inference uses the actual invocation-scoped `Input.Call`, including
classification, both candidates, an explicit precommit reroute, the selected
mixed-tool call and resumed inference. No private HTTP client is used by the
application to dispatch inference; test HTTP clients represent external clients.
Default host/AI resource limits were not relaxed. Credentials remain installed
after request hooks and absent from unrelated hooks and original recorder data.

## Combined observable evidence

These are actual local upstream/gateway sockets, not fake `Managed` tests.
`TestProcessHelper` creates a fresh broker, Engine, Session and HTTP servers in a
new OS process on each invocation; the parent upstream and durable directories
survive. Synthetic account tokens and generic identities only.

| Requirement / boundary | Combined test and inspected behavior |
| --- | --- |
| Authentication, catalog/encoding selection, R01–R08 / R44–R50 subset | `TestCombinedTwoTenantProcessRestart` authenticates Personal usage and Work laptop identities with distinct account credentials on the same endpoint. Inspects real upstream bearer tokens and selected Messages request encoding, no spoofed account/tenant identity, effective served account/endpoint/model, and public Chat output. Independent Observe hook denies credential leakage. |
| Classifier/candidates/explicit reroute, R19 / R21–R27 subset | Same test performs Chat classifier, two concurrent Messages candidates, waits for socket loser cancellation, explicit Messages reroute and selected inference. Five admissions are persisted before handoff; continuation restores to six rather than resetting. All completed/available originals are fsynced independently; losing candidates do not run tools. |
| Mixed tools, recall, durable continuation, R09–R13 / R16–R25 / R38 / R48 subset | Same test reads the actual durable original archive from an owned recall tool, executes one owned effect per tenant, exposes only the pending client tool, then replays public history plus client result in a fresh OS process. Upstream resumed Messages input includes both private recall result and client result, while the public handoff has no owned tool. Persisted actual affinity stays pinned; no reclassification after handoff. |
| Original vs client usage, R10–R15 / R29–R33 subset | Same test corrupts child delivery bytes so they cannot masquerade as upstream JSON; source decoding still succeeds from originals. Final public text and usage are independently decorated (provider output 7, client presentation 900), while original sink retains 7. Classifier/probe/winner absent usage stays nil. Provider-private headers never escape. |
| Required recording before selected effects, R32–R36 / R45–R46 subset | `TestCombinedRecordingFailuresAfterClassificationAndOnResume` completes/records classification and routing calls, then fails the sink or original capture bound on selected and resumed calls. No failed response's tool effect or success delivery; prior Turn/effect survives, current-revision replay rejects uncertain work without dispatch. Capture keeps a bounded incomplete prefix; sink failure is not represented as durable original success. |
| Archive-before-reduction, R16–R20 subset | `TestCombinedArchiveFailureStopsInference` allows absent-state Read, then fails Archive's finite serialized size bound. No reducer, inference or effects; revision remains zero and no archive is published. This is an archive publication failure, not a read-error substitute or filesystem power-loss injection. |
| Reauthorization, R44–R46 / R49–R50 subset | `TestCombinedReauthorizationAndDurableBudgetOnFreshProcess` revokes trusted identity, deactivates the selected account, or moves that account's endpoint. Fresh-process continuation and subsequent replay admit no upstream/effect. Endpoint changes cannot silently reuse previously encoded protocol/history. Identity denial occurs before recall state access/mutation. |
| Shared budgets/deadline and cancellation, R26–R27 / R32–R36 subset | Same fresh-process test exhausts the five-call durable budget and separately expires its persisted deadline; no budget reset or new inference. `TestCombinedHostRootAttemptsRemainShared` additionally caps the engine root at root+one child and rejects a second managed call. `TestCombinedCancellationPersistsPartialOriginalBeforeStopping` waits for actual OriginalBody observation, cancels the external client, verifies upstream cancellation and fsynced incomplete original/unknown usage, and denies uncertain replay. |
| Order validation and unsupported provider state, R24–R25 / R43 subset | `TestCombinedOrderingAndUnsupportedPersistedState` rejects silently late/misordered policies; a correctly placed blocking policy runs before all inference. Provider-held ingress IDs are rejected before dispatch, not dropped for cross-protocol reroute. `TestCombinedIngressAuthenticationCannotBeSpoofed` rejects identity headers without valid auth. |
| Fail-closed multi-source boundary | `TestCombinedNestedOriginalsStillReject` performs an actual authorized nested Input.Call inside one managed inference. Both original sources reach the independent sink with distinct physical exchanges and one logical call; no owned effect/success is selected, and work stays quarantined. This supplements (does not replace/weaken) the previous AI nested-body-replacement regression. |

Existing broker tests `TestHostFallbackFreshPolicyAndReplayBounds`,
`TestHostReplayStartsBeforeTransformsAndRejectsEffectivePost` and
`TestHostFallbackRefreshRevocationRace` retain the independent default-mode
GET/HEAD/OPTIONS fallback proof. POST, including idempotency headers, is not
replayed. AI reroutes above are separate logical calls, **not** hidden broker
fallback/retry. Existing AI crash-after-effect, strict wire, budget/deadline,
selected/resumed recording and multi-original rejection tests remain intact.

## Exact remaining seam / non-claims

- The current Engine does not publish an authoritative **selected original-source
  identity plus served account/protocol/effective-request provenance** across
  nested response replacement/retry sources. Request-local broker evidence solves
  account ambiguity for a single source, not winner attribution across multiple
  originals. HostManaged still records and explicitly rejects multiple sources;
  taking the last source would be unsound. General support requires that core
  seam; no core workaround/private dispatcher was introduced.
- Transport errors remain ordinary host errors/502, with no AI or broker hidden
  retry. A canceled candidate before response headers may have **no original
  source snapshot**: its required sink batch is empty, not invented status/model/
  usage evidence. The combined test checks real socket cancellation and retained
  Session admission, not nonexistent provider usage. Partial observed bytes are
  retained only when observation actually completed before cancellation.
- Dynamic routing is only for the declared portable text/function-tool subset.
  Provider-held IDs, signed/opaque state, multimodal/server-tool semantics and
  unsupported wire fields reject; no state translator is implied. Persisted
  continuation affinity is not rerouted/reclassified. Changed catalog endpoint,
  missing exact route/changed limits or expired lifetime fail closed.
- Public ingress here is bounded Chat JSON. Round 2 adds real-socket Messages
  SSE split inside an event across writes, fully buffered before selected
  decode and required original acknowledgement. This is **not** live incremental
  semantic streaming or arbitrary client portability; no real-provider versions
  are certified. No R41 sandbox, universal protocol support, forced
  termination, exactly-once effect/delivery or distributed transactional log.
- Recall reservation quarantine is intentionally retained after uncertain work.
  Policy/catalog updates are rechecked per dispatch, not atomically serialized
  with arbitrary external tool effects. Delivery is a selected public socket
  response, not proof the peer persisted it; completed-final-response replay has
  no delivery cache/recovery API.
- FileSink is local fsync evidence, not disk-full/power-loss fault injection,
  multi-process concurrent append, encryption, retention or replicated storage.
  Go/OS filesystem I/O and trusted callbacks remain cooperative. Shutdown uses
  existing host lifecycle; process recovery starts only after previous ownership
  ends. Gateway server timeout/TLS/network deployment remains caller-owned.

## Worker validation (this checkout, not independent manager approval)

Executed successfully:

- `go test ./examples/system -count=1 -timeout=90s`
- `go test -race ./examples/system -count=1 -timeout=120s`
- `go test -race -count=1 -timeout=180s ./...` — all ten packages passed
- `go vet ./...`
- `go build ./...`
- `git diff --check`

## Independent manager review and validation

Manager reviewed the routing/evidence API, composition authority/order and required
file sink. A fresh read-only review found no blocking defect within this bounded
slice and inspected socket/restart assertions rather than relying on test names.
No implementation correction was required.

Manager independently executed, all exit 0:

- `go test -count=1 -timeout=180s ./...` — all ten packages, including process fixtures
- `go test -race -count=1 -timeout=180s ./...` — all ten packages
- `go vet ./...`
- `go build ./...`
- `git diff --check`

The reviewer also independently passed focused normal/race tests for system,
brokerapp, aihost and ai. Remaining nonblocking evidence gaps: combined shutdown
racing inference/sink acknowledgement or setup failure; revocation between an
admitted dispatch and an external tool effect; and deliberately conflicting
same-endpoint protocol tuples/evidence mismatch fixtures. These are not certified
by the successful suite. The source-attribution, SSE, filesystem and portability
limits above were not certified by round 1. No push or merge was performed.

## Round 2 narrow combined evidence (this checkout)

- `TestCombinedBufferedSplitSSEContinuation` splits upstream Messages SSE
  inside the `message_start` event across actual flushed socket writes. The
  first selected call's fsynced original has both mixed tool IDs and upstream
  usage 7 before the owned recall effect; public handoff exposes only the client
  tool. A fresh-process continuation sends both private recall and client
  results, records a second complete original SSE, and delivers independently
  decorated Chat usage 900. This is buffered decode, not streaming delivery.
- `TestCombinedShutdownWaitsForRequiredRecorder` reaches active upstream inference
  but deliberately withholds all response headers and body until cancellation;
  no partial original can race shutdown. With the required sink blocked, it
  observes upstream cancellation and stopped admission, verifies cleanup has not
  run before callback exit, then releases the cooperative callback. It sees one
  fsynced empty batch, one cleanup and quarantined work without effects. Fatal
  paths also release the sink and cancel the client before closing the server;
  an empty required batch does not invent source evidence.
- `TestCombinedSameEndpointTupleMismatchRejected` exercises an uncataloged
  version, another caller's account at the same endpoint, and a cataloged
  Chat protocol at a Messages endpoint. The first two do not reach upstream;
  the last reaches the socket with incompatible wire/response, then fails decode
  before effect or selection. Each remains quarantined on replay. Existing
  `TestCombinedNestedOriginalsStillReject` still rejects two original sources;
  `TestCombinedTransportErrorDoesNotSelectOrRetry` confirms a reset socket
  yields one incomplete error original, no hidden retry or effect.
- Focused commands passed: `go test ./examples/system -count=1 -timeout=90s`;
  `go test -race ./examples/system -count=1 -timeout=120s`;
  `go test ./examples/system -run 'TestCombined(ShutdownWaitsForRequiredRecorder|BufferedSplitSSEContinuation|SameEndpointTupleMismatchRejected|TransportErrorDoesNotSelectOrRetry)$' -count=5 -timeout=120s`.

Correction validation: `go test ./examples/system -count=10 -timeout=180s`,
`go test -race ./examples/system -count=3 -timeout=180s`, focused four-case
`go test -race ./examples/system -run '^TestCombined(ShutdownWaitsForRequiredRecorder|BufferedSplitSSEContinuation|SameEndpointTupleMismatchRejected|TransportErrorDoesNotSelectOrRetry)$' -count=5 -timeout=180s`,
and shutdown-only `go test -race ./examples/system -run '^TestCombinedShutdownWaitsForRequiredRecorder$' -count=30 -timeout=120s` passed.

Round 2 does not establish atomic external-effect revocation, a multi-source
winner, a sandbox/cache, live provider/client compatibility, incremental
semantic SSE, exactly-once delivery, power-loss durability or forced Go callback
termination. Shutdown only waits for cooperative callback exit; it does not
forcibly kill arbitrary trusted code. The separate worker/nodehost runtime fix
is outside this change.
