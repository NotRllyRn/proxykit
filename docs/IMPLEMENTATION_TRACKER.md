# Extension implementation tracker

Status: the bounded extension layer and local integration acceptance pass are implemented and verified. All dispatched implementation/review groups have returned and their approved changes are integrated locally. This is not completion of every optional R01–R50 capability: unsupported modes and remaining evidence gaps are listed in the independent audit. Historical research remains historical.

## Delivery rules

- Keep the generic Go transport and its no-extension regression suite intact.
- Use small, explicit contracts, YAGNI, readable concise solutions, modular packages and observable tests. Do not build 50 mandatory core subsystems from a capability catalog.
- No separate check callback. Handlers pass or return an explicit replacement/local response/continuation.
- Recovery uses the last accepted input. Metadata/body replacements commit atomically. Per-unit streaming recovery is not whole-answer rollback. Whole-body modifiers explicitly retain bounded input before delivery.
- Default limits are configurable; disabling a limit removes its guarantee. Timeouts reject late results and do not free native capacity until execution actually ends.
- All local responses enter the response chain. Response-generated requests re-enter the request chain. Managed child calls reuse configured auth/routing without trusting public lineage headers.
- Trusted extensions are arbitrary server-side code, not sandboxed. Installing code requires explicit trust acknowledgement. Download/validation must not execute package scripts.
- Pin extension registrations for active work. Initial install/update activation uses restart, not implicit hot reload; CLI must clearly report this. Dashboard and published npm/npx distribution are not claimed.
- Public documentation must distinguish supported host boundaries, tested example applications, explicitly rejected unsafe modes, optional deployment responsibilities and unsupported functionality.

## Initial work groups

All managers: Pi `codex-broker/gpt-6-astra`, thinking medium. All implementation workers: Pi `codex-broker/gpt-6.1-sol`, thinking medium. Managers plan, delegate via T3, review/test, and dispatch fresh correction tasks until ready. Worktrees isolate groups; parent integrates commits and runs composition/system gates.

1. **Execution** — package `extension/` and its `proxy/` integration; stages/order, bodies, route dispatch, identities/ledger/metadata, failure/admission, lifecycle and managed re-entry. Owns core API. Do not change CLI or optional AI packages.
2. **Distribution/runtime** — packages `extensions/` (installer), `worker/` (Node process protocol), `sdk/`, examples for JS authoring, and CLI wiring. Implement independently testable installer/worker first; bridge to `extension/` after core contract integration. Do not change `proxy/` or `extension/`.
3. **Optional AI/application examples** — package `ai/` and `examples/` subpackages for adapters, tools/coordinator, durable recall and dynamic routing (excluding `examples/brokerapp/` and `examples/node-*`). Keep independent managed-call interfaces until core integration; never implement a second proxy. Do not change CLI, `proxy/`, `extension/`, `worker/` or `extensions/`.
4. **Broker application proof** — `examples/brokerapp/`: extension-owned enrollment/services, catalog/ACLs, credential refresh, durable activation/outbox, quota snapshots and lifecycle. This proves application freedom without putting account management into the generic core.

## Chosen planning defaults / open-case resolutions

- P01/P07: immutable candidate snapshots, per-chunk/event recovery for stream handlers, bounded whole-body recovery only when declared; never restore already delivered bytes.
- P02: bounded memory is a sufficient initial retention implementation. Explicit overflow errors; no disk-spool promise until implemented/tested.
- P03/P08: host-authenticated scoped internal context, fresh child identity and shared atomic root budgets. Public tracking fields do not authorize identity inheritance; strip reserved headers on both boundaries.
- P04: child result is a new parent input, each exchange has its own response chain; provenance and examples must make non-idempotent decoration explicit.
- P05/P06: reject unavailable recursive capacity immediately rather than queueing behind a parent holding it; no automatic hook skipping. Native execution remains charged after timeout.
- P13/P16: stable stage and hook-ID ordering with required/optional dependencies; startup validation and drain/restart activation, no live reload.
- P18: Go host API first; dependency-free Node ESM runtime with TypeScript declarations and a documented versioned process protocol. TypeScript authors may compile to ESM; do not silently require arbitrary install/build scripts.
- Installer: `proxykit extension install <GitHub URL>` with explicit trust flag, validated versioned manifest, resolved commit provenance, path/symlink checks, staged atomic activation, list/remove commands and local-fixture tests. Running servers require restart for changed registrations.

## Acceptance status

The [independent R01–R50 evidence matrix](implementation/acceptance-audit.md) classifies tested boundaries, example-only behavior, conditional support, unsupported modes and missing evidence. It accepts the native execution boundaries and bounded application examples, not blanket completion. Each manager must return concrete tests/commands/results, limitations and commit IDs. Combined application integration and the independent installed-activation review have returned. The latter found a reproducible small-frame worker error defect, now corrected and independently verified after integration. The final system edge-case tests are integrated and the full combined checks pass. Full-catalog certification remains unclaimed. R41 untrusted sandboxing is optional and not offered by trusted workers; it must not receive a false implemented checkmark.

### Integrated group results

- **Group D / broker application:** foundation `b05cbc6` integrated as `8f94c53`; reviewed host bridge `4f9c915` integrated as `e74d704`. `NewHost` now composes actual native registrations, `proxy.NewWithEngine`, bind-before-auth ingress, lifecycle-owned services, request-local credential selection and bounded GET/HEAD/OPTIONS fallback. Manager socket tests cover concurrent identities, revocation/refresh races, spoof rejection, shared budgets, one-slot recursion, cancellation and original-versus-delivered observations; package coverage was 87.2%. Parent inspected `examples/brokerapp/host.go` and independently passed repository race tests (`-timeout 180s`), vet and CLI build after integration. See [detailed evidence and limits](implementation/brokerapp.md). Combined AI/broker/recorder evidence has since been integrated (see system group below). Transport-error fallback and live deployment validation remain unclaimed. No remote push or release.
- **Group A / execution:** corrected and independently approved manager commit `aadae69` integrated as `cafaa06`. Original upstream observations are detached from local/presentation output; `Finish` reader completion and root-only `Delivery` writer evidence are separate. Parent reran repository race tests, vet and build successfully. See [execution contracts](implementation/execution.md). Writer acceptance is not peer acknowledgment. Third-review commit `d93bc96` integrated as `d1c1377`: invocation-scoped `Input.Call`, sanitized shared-ledger failure categories and opt-in `SuppressInformationalResponses` are implemented. Parent integration exposed an intermittent one-slot admission failure; reviewed correction `1b22b3a` is integrated as `e60e121`. It releases the admission slot before publishing a completed result while retaining shutdown tracking through publication. A pre-fix regression reproduced the defect; the manager independently passed 100 one-slot broker repetitions. Parent's subsequent full race suite, vet and build pass.
- **Group B / distribution/runtime:** foundation `23ada1c` integrated as `f83dd40`; reviewed installed HTTP activation bridge `c39637d` integrated as `8f36088`. `nodehost.Open` activates validated workers through the native engine, with bounded managed HTTP callbacks and startup rollback. Compiled-process fixtures cover GitHub-fixture installation through real Node hooks and upstream/client output. Parent inspected activation/managed-call code and passed the full repository race suite (`-timeout=180s`), vet, CLI build and all three SDK tests, including TypeScript with no skips. See [distribution contracts](implementation/distribution.md). Published npm/npx distribution and a dashboard are not claimed.
- **Group C / optional AI and recall:** foundation `aac1a40` integrated as `0d234d2`; reviewed managed-host bridge `863daaf` integrated as `1aa85ca`. Includes bounded Chat/Messages adapters, tool coordination, candidate selection, usage presence, durable recall/CAS, actual `Input.Call` inference, required original-evidence recording, cumulative durable budgets and process-restart tests. See [AI contracts](implementation/ai.md). Parent reran the full repository race suite (`-timeout=180s`), vet and CLI build successfully after bridge integration. Multi-original-source results are explicitly rejected pending authoritative selected-source provenance. The separate combined system group has now supplied composition with the real broker example (see below); fixture-local catalog/auth hooks alone were not treated as that evidence.

### Integration and review results

Groups used Astra-medium managers and Sol-medium workers in isolated T3 worktrees; substantive corrections went through fresh delegated review rounds. All groups below have returned; no delegated work remains active.

- `proxykit-group-a-manager-r4`: completed, reviewed and integrated; result publication now follows admission release. Further core changes wait for concrete bridge findings.
- `proxykit-group-b-manager-r3`: completed, reviewed and integrated; installed hooks now activate through `nodehost/` and the serving CLI.
- `proxykit-group-c-manager-r2`: completed, reviewed and integrated; real managed inference and durable replay composition in `examples/aihost/`.
- `proxykit-group-d-manager-r2`: completed and integrated in `examples/brokerapp/`.
- `proxykit-system-manager-r1`: completed; reviewed commit `38a9476` integrated as `9c4415f`. Actual broker + AI + recall + independent durable recorder composition now has two-tenant socket/fresh-process evidence, classifier/candidates/explicit rerouting, served-route provenance, mixed tools, restart continuation, original usage versus decoration, revocation, restored budgets and failure quarantine. See [system contracts and tests](implementation/system.md). Parent independently passed all 11 packages with `go test -race -count=1 -timeout=180s ./...`, plus `go vet ./...` and `go build ./...` after integration.
- `proxykit-system-manager-r2`: completed; reviewed `e5b95d9` + `c227191` integrated as `b850b11` + `34475db`. Added combined split buffered SSE with fresh-process continuation, deterministic shutdown waiting on required recording, stopped admission, same-endpoint route/account/wire mismatch rejection and ordinary transport failure without hidden retry. Review found and corrected shutdown fixture scheduling/teardown issues. Cooperative cancellation and buffered semantics remain explicit; authoritative-evidence mismatch and setup-fault injection are not claimed.
- `proxykit-acceptance-audit-r1`: completed read-only manager/worker review at `1aa85ca`; [full evidence matrix and objections](implementation/acceptance-audit.md). No new blocker established for the declared single-source managed-AI slice. Installed activation and combined application evidence were pending at that baseline.
- `proxykit-acceptance-audit-r2`: completed at `e074b34`. Accepts narrow installed activation, not exhaustive compiled-CLI lifecycle coverage. Found P2: `worker/runtime.mjs` truncates exceptions by JS code units rather than encoded frame size, so long Unicode/escaped messages can terminate the worker at small supported limits. Default-frame control survives. Positive upstream credential-delivery assertion also needs strengthening.
- `proxykit-group-b-manager-r4`: completed; reviewed `690b044` integrated as `e6d7bd5`. Complete UTF-8 JSON error frames are bounded at 256/1024/1048576 bytes while preserving correlation, error codes and nonempty diagnostics. Real-process tests prove unrelated/subsequent work survives, including cancellation and shutdown. Installed tests prove optional recovery/required 502 and Authorization delivery to actual outer/inner upstream requests without leaking into later snapshots. Parent independently passed all 11 packages under race, vet/build and all three SDK tests including TypeScript, zero skips.

Latest parent verification after the admission correction: `go test -race -count=1 -timeout 180s ./...`, `go vet ./...`, and CLI build all pass. Historical integration failure `TestHostScopedHTTPAuthAndOneSlot/1` (`host_test.go:315: 502`, hook capacity exhausted) is addressed by the publication-order regression/fix, not by a retry or wider admission.

The parent replaced the timing-sensitive `ai/correction_test.go` observation fixture with explicit cancellation inside the observer: it still proves that an observer returning nil after operation cancellation cannot yield success, without requiring dispatch to begin within 10 ms. `TestObservationCancellationPreventsSuccess` passed 100 race-enabled repetitions. Separate session deadline/late-result tests remain. This is a test-only change, not a production timeout relaxation. Cross-package gates below remain open.

Baseline before implementation: `go test -race -count=1 -timeout 90s ./...`, `go vet ./...` and `go build ./cmd/proxykit` passed on Go 1.27.1 / Linux. Node v24.16.0 is available for worker process tests. This is transport evidence only.

## Final integrated verification

Parent checkout at `34475db`, including the runtime correction and all combined-system tests:

- `go test -count=1 -timeout=240s ./...` — all 11 packages passed.
- `go test -race -count=1 -timeout=240s ./...` — all 11 packages passed.
- `go vet ./...` — passed.
- `go build ./...` — passed.
- `TSC_BINARY=/root/.npm/_npx/483d2954a4c5190c/node_modules/typescript/bin/tsc node --test sdk/*.test.mjs` — 3 passed, 0 failed, 0 skipped. This compiler path is local verification tooling, not a runtime dependency.
- `git diff --check` — passed.

Local acceptance checkpoints:

- [x] Native execution contracts and no-extension transport regressions.
- [x] GitHub-fixture installation, compiled server activation and Node hook execution.
- [x] Managed nested calls, selected credential delivery, recovery and required failure denial.
- [x] Real broker + AI + recall + independent recorder composition, two tenants and process restart.
- [x] Split buffered SSE, cancellation/shutdown, negative selection, transport-error and multi-source rejection evidence.
- [x] Independent requirement audit and defect correction; supported scope documented.
- [ ] Every optional capability in R01–R50 implemented/certified. See the [classification matrix and limitations](implementation/acceptance-audit.md), not a fabricated blanket checkmark.

Changes are committed on local `main`; no remote push or release has been performed. Performance is not newly benchmarked; tests establish behavior, not maximum throughput. Dashboard/npm publication, untrusted sandboxing, live incremental semantic continuation, general multi-source retry attribution, cache implementation, monetary enforcement and live-provider certification are not delivered by this acceptance pass.

Integration gate definitions (evidence above and package contracts):

1. Merge reviewed isolated commits; bind the worker descriptors/invocations to the native host without duplicating dispatch or body ownership.
2. Prove CLI install → restart activation → real HTTP request → Node hook → upstream → transformed response, plus worker failure fallback and required-hook failure denial.
3. Bind broker, independent observer, protocol adapter, tools and recall through managed dispatch; inspect both provider input and client output, then restart and replay the next client request.
4. Exercise two tenants concurrently, local responses, response-triggered child requests, one-slot re-entry, original usage despite decoration, unknown usage, malformed/spoofed internal headers, timeout late results and cancellation.
5. Exercise SSE split across reads, retained-body overflow, trailers/truncation, duplex and upgrades; retain existing opaque transport guarantees or clearly reject incompatible declared transforms.
6. Exercise rerouting/candidates before commitment, loser cancellation, target-specific credentials, state-affinity rejection and requested/served cache separation.
7. Run repository race tests, vet/build, Node tests, compiled-process startup/shutdown tests and the representative combined scenario. Publish exact command evidence and remaining unsupported modes; do not turn a fixture pass into live-provider certification.

## Discovered cases / follow-up

- Installation executes trusted arbitrary code only on explicit server activation; GitHub ownership/manifest names are not a security review.
- Git URLs, revisions, archive links, executable paths, package IDs and symlinks are attacker-controlled installation inputs. Avoid shell interpolation and ambient Git hooks/config surprises.
- Node stdout is protocol-owned; extension logs must not corrupt framing. Bound messages and associate out-of-order replies by invocation ID.
- Installer rollback/update, worker death during a request, unsupported 101 transforms, cancellation after header commitment and required-observer failures need explicit tests.
- A managed call helper must inherit invocation cancellation even if the extension supplies an independent context; trusted-code status is not a reason to weaken the helper's contract.
- Cross-owner ledgers must not copy arbitrary exception messages containing credentials. Stable failure categories are safer than guessed secret redaction.
- Informational HTTP responses can escape before final response selection; speculative paths must account for this without silently breaking ordinary transparent forwarding.
- Broker HTTP-response fallback is implemented, but transport-error fallback lacks a precommit managed recovery hook. Until that seam is implemented and tested, transport failures remain normal proxy 502s; no private retry dispatcher bypasses the host.
- Combined AI/broker tests verify actual served route and wire format through opt-in exact selection and request-local evidence. Default broker behavior still binds its configured account endpoint; a classifier's proposed destination alone is never proof of served identity.
