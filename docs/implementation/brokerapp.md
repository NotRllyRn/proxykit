# Group D — broker application proof

Status: standalone application **previously manager-reviewed**; round-2 native host bridge **implemented, manager-reviewed and independently verified**. This is a trusted extension-owned application example, not a mandatory host account subsystem and not a second proxy. Parent integration and system acceptance remain separate gates.

## Round 2 minimal host bridge plan (manager proposal; implementation evidence below)

1. Add an opt-in native adapter in `examples/brokerapp/` using `proxy.NewWithEngine`, with request-local broker route/credential selection and no shared target mutation. Keep any broker API addition narrow: selection/authentication must be reusable without a second HTTP dispatcher.
2. Bind ingress before authentication; accept inherited subjects only from host-verified internal capability context, otherwise authenticate caller bearer tokens. Each effective attempt/child rechecks account ACL, destination and fresh credentials. Never interpret public route/account/identity fields as authority; disable redirects.
3. Use native registration setup/cleanup to own broker application listeners/jobs and teardown. Retain separate catalog/caller and service-auth enrollment listeners; supplied transport ownership stays explicit.
4. Implement conservative precommit fallback through managed full-pipeline children only for replay-safe methods with bounded retained bodies. Refuse unsafe side-effect replay; preserve host attempt/depth/admission limits, cancellation and fail-open snapshot semantics. If the available API cannot do this safely, report the precise missing seam rather than modifying core.
5. Prove actual HTTP composition with concurrent accounts, refresh/revocation fallback, scoped internal HTTP reentry and spoof denial, independent original upstream usage observation before presentation, and distinct final writer delivery. Test unknown usage, overflow, cancellation/shutdown, unchanged no-op/failed transforms, shared budgets and one-slot recursion rejection.
6. Update the requirement mapping for R01–R08/R14/R40/R43 and optional R49 with named tests and honest limits; run unit/race/vet/build, manager review and fresh worker corrections before committing only owned paths. No production OAuth/DB/dashboard, universal accounting, or all-50 claim.

## Round 2 implementation and evidence

- `examples/brokerapp/host.go`: opt-in `NewHost` / `HostConfig`, native setup/cleanup, `proxy.NewWithEngine` and the existing engine-managed transport (not `Execute` and not a second dispatcher). Whole bounded retention runs before supplied request transforms; final broker route/credential binding runs after supplied request hooks. Full configured endpoints are selected request-locally with committed ACL/destination checks and fresh expiry/refresh checks. No shared target mutation. Supplied stage/dependency contradictions reject activation.
- Ingress strips credential-bearing/identity-shaped headers before `Bind` snapshots original facts, then accepts only a trusted inherited subject or the original caller bearer. Scoped HTTP capabilities are verified before external auth. Original ingress bearer and selected upstream secrets are absent from observer request snapshots, owner metadata and ledger. Refresh callback failures become fixed broker errors, never raw callback errors. Trusted extensions/callbacks must still avoid logging secrets; arbitrary public payloads/URLs are not a general-purpose secret-redaction service.
- Native setup owns the separate catalog/enrollment listeners plus notification worker; cleanup stops the Broker/application. `Host.Close` drains the engine and closes owned transport idle connections. The external gateway HTTP server remains caller-owned. Per-request retained state is context-owned, not a global map requiring a cancellation-time Finish callback.
- `ResponseStart` fallback is precommit and uses only managed full-chain children. Both saved and effective methods must be GET/HEAD/OPTIONS; POST/other side effects are not retried. Saved pre-transform input prevents duplicate request transformation. All attempts share host budgets. A failed/denied/overflowing child preserves its parent candidate under fail-open snapshot semantics. Direct descendants exclude accounts already selected along their parent chain; scoped ordinary-HTTP reentry starts independent selection state but keeps host auth/root budgets. No private sockets are used by the adapter.

### Round 2 socket requirement mapping (in addition to standalone table below)

| Requirement | Named new evidence and remaining boundary |
|---|---|
| R01/R02/R04 | `TestHostConcurrentCompositionOriginalAndDelivery`: 40 simultaneous authenticated requests, two private caller/account routes, fresh exact upstream credentials, stripped caller credentials in independent observations. `TestHostScopedHTTPAuthAndOneSlot`: auth-reusing scoped HTTP, public identity/service-token spoof denial, forged capability denial, cross-origin and expired client denial. |
| R03/R04 | `TestHostFallbackFreshPolicyAndReplayBounds`: fresh fallback credential refresh and committed ACL/destination revocation before child dispatch. `TestHostFallbackRefreshRevocationRace`: revocation while fallback refresh is blocked cannot be overwritten or dispatched. `TestHostRefreshSingleflightAndSanitizedFailure` adds 24 concurrent real HTTP requests sharing one refresh plus an independent recorder check that a raw refresh callback error/credentials never enter request snapshots, terminal errors or ledger. Standalone singleflight/race tests remain unchanged. |
| R05/R06/R07/R14 | Existing separate-listener service auth/enrollment/transaction/crash tests remain applicable. Every host fixture uses real independent listeners through native Setup; cleanup asserts both listeners are closed. No new host enrollment endpoint or store was added. |
| R08 | `TestHostConcurrentCompositionOriginalAndDelivery`: independent original attempt usage-header evidence distinguishes 20 present versus 20 absent usage observations, and original upstream bytes remain unchanged before a Whole presentation decorator. Absence is unknown, not a synthesized zero. `TestHostFallbackFreshPolicyAndReplayBounds` additionally retains the original failed-attempt usage observation before fallback and keeps absent child usage nil, not zero. Existing `Usage == nil` / quota tests retain the typed application contract. No generic provider parser, fabricated body usage for abandoned parents or universal billing claim. |
| R40/R43 | `TestHostCancellationAndShutdown`: caller cancellation and engine shutdown cancel actual upstream sockets and await local work; native teardown closes application listeners. `TestHostScopedHTTPAuthAndOneSlot`: shared admission of one rejects recursive HTTP child work without deadlock. Real `NewWithEngine` transport/registration/lifecycle are exercised, not a metadata Dispatcher mock. |
| Optional R49 | `TestHostFallbackFreshPolicyAndReplayBounds`: shared attempt exhaustion preserves the original 503, request retention overflow dispatches nothing, POST does not retry. `TestHostReplayStartsBeforeTransformsAndRejectsEffectivePost`: saved-input replay applies a request transform once per attempt, and a GET transformed to POST is not retried. `TestHostDoesNotFollowProviderRedirect`: provider 307 is not followed by managed transport. `TestHostRejectsLateCredentialTrailers`: declared credential trailers reject before upstream dispatch. `TestHostFailedAndNoopTransformsPreserveBytes`: failed/no-op presentation mutations leave the original bytes unchanged. |
| Observation/delivery cross-check | `TestHostConcurrentCompositionOriginalAndDelivery` independently records original headers/bytes and final writer evidence (status 200, five decorated bytes, normal return). Finish is not used as delivery proof. Child reader completion is not external delivery; inherited host source/Delivery tests remain the authoritative low-level boundary regressions. |

### Round 2 verification

Executed in the shared checkout after adding the bridge/socket tests:

```text
gofmt -w examples/brokerapp/host*.go
go test ./examples/brokerapp -count=20 -timeout=120s   PASS (final suite, 3.550s)
go test ./examples/brokerapp -count=1 -timeout=60s   PASS (earlier scoped gate; final suite also included in repository gates)
go test ./... -count=1 -timeout=120s   PASS
go test -race ./... -count=1 -timeout=180s   PASS
go vet ./...   PASS
go build ./...   PASS
git diff --check   PASS
```

No OAuth, production DB/dashboard, remote-provider certification, durable host observer store, universal private-socket accounting or all-50 acceptance claim. An intermediate repetition caught a fixture that treated client body completion as synchronous Finish completion. The fixture now uses pre-delivery Observe for successful-attempt security evidence, explicit observer synchronization, and Delivery for final writer assertions; it does not demand guaranteed success Finish callbacks. The final repeated and repository race gates passed. Manager independently reviewed all new production/test files and found no blocking findings, so no correction round was needed. Independent `go test ./... -count=1 -timeout=120s`, `go test -race ./... -count=1 -timeout=180s`, `go vet ./...`, `go build ./...`, and `git diff --check` passed. Independent `go test ./examples/brokerapp -count=20 -timeout=120s` passed (4.283s); `go test -cover ./examples/brokerapp` reported 87.2%. No changes outside the owned broker example/document are part of this round.

### Precise remaining seams and limits

- The current host exposes response-triggered children only at `ResponseStart`, not recovery from a transport error in `Observe`. The adapter intentionally returns the normal proxy error instead of adding a private retry dispatcher. A future transport-failure retry would need a precommit managed recovery result at the host error boundary, with the same full-chain replay/budget contract.
- Whole bounded request retention is deliberate, including requests that will not retry; declared ingress trailers are rejected rather than forwarding late credential fields. Child response retention also uses the host body limit. Limits are per operation/root and may be explicitly disabled; they are not global memory admission or a process sandbox.
- OriginalBody observes consumed upstream bytes only. A parent abandoned at ResponseStart may have unread provider usage; it remains unknown. Header usage can be observed before fallback, but body-only failed-attempt usage needs a deliberate bounded drain/accounting policy or a protocol-specific coordinator, not a fabricated zero. Response decorators may run on both child and parent.
- Engine cancellation may suppress Finish/other callbacks. Delivery is best-effort final writer evidence, not durable accounting or peer receipt. Per-request retained state is reclaimed with the request context regardless of callback completion. Trusted native callbacks must cooperate; no forced Go termination is added.
- Endpoint/model labels do not rewrite model protocol bodies, imply model compatibility, or choose the client's requested model. Exact endpoint selection ignores public path/query; configured endpoints with credentials/query/fragment are rejected at route binding. Account revocation is checked at each new selection; an already admitted attempt retains its admission snapshot as before.

## Owned paths and implementation

Only `examples/brokerapp/` and this document were changed by Group D (standalone baseline plus round-2 additions above):

- `examples/brokerapp/store.go`: extension-owned account/label/ACL/credential state and transactional notification outbox; copy/persist/publish; atomic-file persistence; revision-checked credential replacement; fail-closed uncertain-commit handling.
- `examples/brokerapp/broker.go`: explicit caller authentication, immutable configuration copies, request-local account/provider/model/endpoint/credential decisions, bounded attempts, fallback eligibility checks, per-account singleflight refresh, separately typed quotas and provider usage, sanitized outcomes and cooperative lifecycle.
- `examples/brokerapp/application.go`: separate caller and enrollment listeners, arbitrary authenticated `GET /catalog`, service-authenticated bounded `POST /enroll`, notification retry worker, admission stop/cancel/wait and listener cleanup. Either Application or Broker close cleans up the owned application resources.
- `examples/brokerapp/broker_test.go`, `examples/brokerapp/failure_test.go`: local upstream and socket fixtures, concurrency/failure/restart/crash evidence.
- `examples/brokerapp/README.md`: API/trust/storage/lifecycle contracts and executable proof commands.

No changes to `extension/`, `proxy/`, transport, installer, workers, CLI, `ai/`, or other examples. No external dependencies, OAuth, real accounts, dashboard, private transcripts or credentials. All fixture accounts use generic labels and synthetic tokens.

## Concrete application-side requirement evidence

| Requirement | Implementation / observable tests |
|---|---|
| R01 — replace inbound authentication | Caller bearer token maps privately to an identity. Duplicate Authorization, missing/invalid tokens and identity-shaped tokens are denied before dispatch. No identity/account headers or query/body values can impersonate a caller. `TestIdentityAccountIsolationAndLocalUpstreams`, `TestCatalogQuotaIsNotUsage`, `TestSingleApplicationAndServiceCredentialSeparation`. |
| R02 — account/upstream route | `Execute` creates per-request decisions and keeps requested versus served model provenance distinct; configured served models may override requested intent. Two local paths receive their own account's credential; 80 concurrent requests alternate two identities/accounts without shared-config mutation. `TestIdentityAccountIsolationAndLocalUpstreams`, `TestConcurrentIdentitiesKeepRoutesLocal`. |
| R03 — credentials/refresh | Per-account singleflight, independent waiter cancellation, lifecycle-owned refresh cancellation, durable credential update with account revision check. Forty concurrent calls refresh once; concurrent revocation cannot be overwritten. `TestRefreshSingleflightAndWaiterCancellation`, `TestRefreshCannotOverwriteConcurrentRevocation`, `TestCloseCancelsRefreshAndActiveDispatch`, `TestCallbackErrorsAreNotObservations`. |
| R04 — catalog/labels/ACLs | Store owns labels and caller ACLs; catalog filters by authenticated identity and endpoint policy. Fallback rereads committed account ACL/destination and uses only that account's credential. `TestCatalogQuotaIsNotUsage`, `TestFallbackRechecksACLAndDestination`, `TestConcurrentIdentitiesKeepRoutesLocal`. |
| R05 — enrollment/service auth | A distinct service token authenticates enrollment; caller tokens cannot enroll and the service token cannot authenticate as a caller. Bounded JSON rejects unknown fields, trailing objects and unconfigured destinations. `TestEnrollmentAuthSeparateListenerAndLifecycle`, `TestSingleApplicationAndServiceCredentialSeparation`, `TestEnrollmentCommitFailureNeverPublishes`. |
| R06 — separate listener | Application owns two independently bound loopback listeners in tests; enrollment is absent from the catalog listener. Both listeners close during cleanup. `TestEnrollmentAuthSeparateListenerAndLifecycle`. Separate-process deployment is not implemented. |
| R07 — activation/outbox transaction | Account state, labels/ACLs and stable-ID notification are persisted in one atomic-file transaction, before eligibility publication. Injected pre-write and post-rename failures poison the running Store; no new eligibility or outbox notification is published. A real subprocess exits after recording receiver receipt but before ack; restart recovers activation plus pending outbox and redelivers the same ID, with receiver deduplication. Interrupted staging files are ignored. `TestActivationFailureAndUncertainCommit`, `TestEnrollmentCommitFailureNeverPublishes`, `TestCrashAfterNotificationBeforeAck`. |
| R08 — quota/outcomes | Timestamped quota snapshots are separate from optional actual provider usage; absent usage remains nil. Failed attempts retain attributed usage before fallback; catalog excludes credentials, endpoints, ACLs and raw errors. `TestCatalogQuotaIsNotUsage`, `TestFallbackRechecksACLAndDestination`, `TestCallbackErrorsAreNotObservations`. No real quota provider is integrated. |
| R14 — arbitrary authenticated HTTP endpoint | Extension-owned `GET /catalog` is an ordinary `net/http` handler with caller ACLs; enrollment is a separate application endpoint. No dashboard or host HTTP-mount API is required. `TestCatalogQuotaIsNotUsage`, `TestEnrollmentAuthSeparateListenerAndLifecycle`. |
| R40 — jobs/lifecycle | Startup notification retry, serialized delivery/ack, cancellation of refresh/request/job work, stop-admission and idempotent cleanup. A callback failure retries, cancellation retains the pending notification, listeners close and later requests are rejected. `TestNotificationRetryAndCleanupCancellation`, `TestCloseCancelsNotificationAndRetryHasNoLostOutbox`, `TestCloseCancelsRefreshAndActiveDispatch`, `TestEnrollmentAuthSeparateListenerAndLifecycle`. |
| R43 — application plus gateway adapter | Application owns listeners/store/job; `Execute` accepts a narrow trusted `Dispatcher` interface. Local HTTP dispatch demonstrates authenticated request-local routing plus independent endpoint/resource lifetime. **Round 2 adds native host registration/managed transport/lifecycle proof** as mapped above; whole-repository R43 completion remains an integration judgment. |
| Optional R49 — binding/protection | Exact configured endpoints, account ACL rechecks, immutable request-local endpoint/credential pairs. Local test dispatcher builds fresh outbound auth and disables redirects. `TestFallbackRechecksACLAndDestination`, `TestIdentityAccountIsolationAndLocalUpstreams`. Round 2 binds managed children freshly and does not follow provider redirects. **Not** universal private-socket enforcement or a completed generic-host R49 guarantee. |

## Standalone baseline verification (historical)

Executed from the group checkout with `go version go1.27.1 linux/amd64`, scoped only to this package:

```text
gofmt -w examples/brokerapp/*.go
go test ./examples/brokerapp
  PASS — ok github.com/NotRllyRn/proxykit/examples/brokerapp
go test -race ./examples/brokerapp
  PASS — ok github.com/NotRllyRn/proxykit/examples/brokerapp
go vet ./examples/brokerapp
  PASS — exit 0, no diagnostics
go test ./examples/brokerapp -count=20
  PASS — repeated package suite
go test -cover ./examples/brokerapp
  PASS — 86.8% of statements
```

Manager review found no blocking findings within this standalone scope and independently reran unit, race, vet and coverage checks, followed by `go test ./examples/brokerapp -count=20` (PASS, 1.032s) and `go test -race ./examples/brokerapp -count=1` (PASS, 1.211s). Vet emitted no diagnostics.

The standalone baseline had fifteen top-level tests plus subtests; round-2 host tests are additional. The subprocess crash exits with code 23 at the injected delivery-before-ack boundary; the parent test checks recovery and stable-ID retry. This is a real process-exit test and deterministic filesystem-fault injection, **not** a power-loss or every-filesystem crash certification. Those baseline commands did not establish repository-wide or host integration results; the round-2 results above now provide local managed-host evidence.

## Contracts, limitations and parent bridge needs

- **Single-process durable store:** exactly one Store/process owns a private directory, with no interprocess locking, replicas, distributed leases, encryption, migration or corruption repair. New state files are `0600`; existing directory security is an operator obligation. Atomic replace + file/directory fsync is tested locally, not certified for every filesystem/platform. Credentials are plaintext in the private store. Incomplete crash staging files are ignored, not automatically garbage-collected.
- **Uncertain commit:** an error after rename can mean the complete new transaction is already on disk. The running Store fails closed for routing/catalog/outbox delivery until reopening, without publishing the uncertain snapshot. Recovery may see either complete transaction; a failed activation response is not proof of rollback. No eligibility is published from unpersisted state. Already admitted attempts retain their admission-time snapshot; enrollment revocation does not retroactively retract an in-flight dispatch.
- **Notifications:** transactional creation plus at-least-once delivery, not exactly-once external effects. Receiver deduplicates stable ID scoped to this store. Callback/ack failure leaves pending entries; restart retry is supported. There is no outbox size bound, production backoff/dead-letter policy or globally unique identity across unrelated stores.
- **Policy:** stable account-ID ordering is deliberately simple, not quota/cost optimization. Dispatch failure/5xx permits bounded fallback; refresh failures fail the request rather than silently choosing another account. Quotas are trusted in-memory inputs, not real provider polling or inferred usage. No arbitrary OAuth/account verification, protocol adapter, conversation migration, dashboard or real-provider compatibility claim.
- **Lifecycle:** trusted dispatch/refresh/notification callbacks must honor context cancellation. A close timeout does not kill arbitrary Go code; later Close can await remaining work. Dependencies supplied by callers (for example an HTTP transport) remain caller-owned. This is not a process sandbox. Loopback plaintext HTTP is the tested deployment; remote exposure needs TLS, independent service credential provisioning and operational limits/supervision.
- **Host bridge:** implemented opt-in through `NewHost` as described above. The standalone credential-bearing `Attempt` remains a trusted Dispatcher API, not an observer payload; managed host credentials use the separate engine credential result. Host identity/source/delivery attribution is exercised without logging selected credentials or raw broker callback errors.
- **Managed transport:** round 2 uses actual host transport, bounded saved input, precommit children, cancellation, shared budgets and fresh destination-bound auth. It does not enforce constraints on arbitrary private sockets or prove universal accounting. Standalone `Execute` still has no response commitment/body ownership contract: do not use its metadata-only retry behavior to replay committed output or side effects. Transport-error retry, body-only usage on abandoned responses and protocol/model conversion remain explicitly unimplemented.

The accepted R01–R08/R14/R40/R43 application ownership is demonstrated without adding host account, enrollment, database or job subsystems. Overall requirement completion remains a manager/integration judgment after the other groups' host bridges and combined acceptance tests.
