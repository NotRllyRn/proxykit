# Extension implementation tracker

Status: implementation authorized by the user; work in progress. This file records current decisions and acceptance evidence, not blanket support claims. Historical research remains historical.

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

No extension requirement is marked complete yet. Each manager must return requirement IDs, concrete tests/commands/results, limitations and its commit IDs. Final evidence needs cross-package integration, process tests, race tests, vet, build and the no-extension socket regressions. R41 untrusted sandboxing is optional and not offered by trusted workers; it must not receive a false implemented checkmark.

### Integrated group results

- **Group D / broker application:** manager commit `b05cbc6` integrated locally as `8f94c53`. Parent inspected the broker, store and application implementations and reran `go test -race -count=1 -timeout 90s ./...` and `go vet ./...` successfully. Package scope covers extension-owned authentication/routing, refresh, catalog/enrollment, atomic activation/outbox and lifecycle. See [the detailed evidence](implementation/brokerapp.md). Host registration, managed transport, attribution and composed acceptance tests remain pending; this is not end-to-end requirement completion. No remote push or release.
- **Group A / execution:** first implementation passed its manager's test/race/vet/build checks but was not approved or committed. Review requires immutable original upstream metadata/body observations (excluding local responses), plus a distinct proxy-boundary delivery observation rather than treating reader completion as delivery. Fresh Astra-medium manager round `proxykit-group-a-manager-r2` is running against the preserved isolated checkout, with Sol-medium correction/review instructions. Pi child availability initially failed its version check; a direct version check and the new manager launch subsequently succeeded. Do not merge until correction review passes.
- Groups B and C remain active; bridge integration waits for reviewed contracts rather than coding against moving APIs.

Baseline before implementation: `go test -race -count=1 -timeout 90s ./...`, `go vet ./...` and `go build ./cmd/proxykit` passed on Go 1.27.1 / Linux. Node v24.16.0 is available for worker process tests. This is transport evidence only.

Integration gates after group review:

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
