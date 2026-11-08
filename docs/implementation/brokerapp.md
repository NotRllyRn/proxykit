# Group D — broker application proof

Status: **implemented, manager-reviewed and package-tested**, independently of the parent host bridge. This is a trusted extension-owned application example, not a mandatory host account subsystem and not a second proxy. Ready for local commit and parent integration; the host bridge remains pending.

## Owned paths and implementation

Only `examples/brokerapp/` and this document were changed by Group D:

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
| R43 — application plus gateway adapter | Application owns listeners/store/job; `Execute` accepts a narrow trusted `Dispatcher` interface. Local HTTP dispatch demonstrates authenticated request-local routing plus independent endpoint/resource lifetime. **The parent host registration/managed-dispatch bridge remains unimplemented**, so this is application-side proof, not full host R43 completion. |
| Optional R49 — binding/protection | Exact configured endpoints, account ACL rechecks, immutable request-local endpoint/credential pairs. Local test dispatcher builds fresh outbound auth and disables redirects. `TestFallbackRechecksACLAndDestination`, `TestIdentityAccountIsolationAndLocalUpstreams`. **Not** universal redirect/nested-call enforcement or a completed generic-host R49 guarantee. |

## Verification

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

Fifteen top-level tests plus subtests. The subprocess crash exits with code 23 at the injected delivery-before-ack boundary; the parent test checks recovery and stable-ID retry. This is a real process-exit test and deterministic filesystem-fault injection, **not** a power-loss or every-filesystem crash certification. No repository-wide or host integration result is claimed by this group.

## Contracts, limitations and parent bridge needs

- **Single-process durable store:** exactly one Store/process owns a private directory, with no interprocess locking, replicas, distributed leases, encryption, migration or corruption repair. New state files are `0600`; existing directory security is an operator obligation. Atomic replace + file/directory fsync is tested locally, not certified for every filesystem/platform. Credentials are plaintext in the private store. Incomplete crash staging files are ignored, not automatically garbage-collected.
- **Uncertain commit:** an error after rename can mean the complete new transaction is already on disk. The running Store fails closed for routing/catalog/outbox delivery until reopening, without publishing the uncertain snapshot. Recovery may see either complete transaction; a failed activation response is not proof of rollback. No eligibility is published from unpersisted state. Already admitted attempts retain their admission-time snapshot; enrollment revocation does not retroactively retract an in-flight dispatch.
- **Notifications:** transactional creation plus at-least-once delivery, not exactly-once external effects. Receiver deduplicates stable ID scoped to this store. Callback/ack failure leaves pending entries; restart retry is supported. There is no outbox size bound, production backoff/dead-letter policy or globally unique identity across unrelated stores.
- **Policy:** stable account-ID ordering is deliberately simple, not quota/cost optimization. Dispatch failure/5xx permits bounded fallback; refresh failures fail the request rather than silently choosing another account. Quotas are trusted in-memory inputs, not real provider polling or inferred usage. No arbitrary OAuth/account verification, protocol adapter, conversation migration, dashboard or real-provider compatibility claim.
- **Lifecycle:** trusted dispatch/refresh/notification callbacks must honor context cancellation. A close timeout does not kill arbitrary Go code; later Close can await remaining work. Dependencies supplied by callers (for example an HTTP transport) remain caller-owned. This is not a process sandbox. Loopback plaintext HTTP is the tested deployment; remote exposure needs TLS, independent service credential provisioning and operational limits/supervision.
- **Host bridge:** authenticate through this extension without permitting public caller impersonation; pass managed dispatcher decisions request-locally, never by mutating shared proxy configuration; connect lifecycle to host admission/drain/cleanup. Keep credential-bearing `Attempt` private to trusted execution, and use sanitized outcome fields for observers. Provide host request/attempt identity and original/effective observation attribution without logging credentials or callback errors.
- **Managed transport:** the parent bridge must provide actual transport, body lifetime/replayability, commitment/streaming behavior, cancellation and shared budgets. Build fresh outbound authentication for the exact effective destination, strip inherited caller/credential headers, disable redirects or reauthorize/rebind each hop, and rerun eligibility checks for participating fallback/nested calls. This library cannot enforce constraints on private sockets or prove whole-host accounting. Do not retry requests with committed client output or non-idempotent side effects merely because this standalone metadata dispatcher permits another attempt.

The accepted R01–R08/R14/R40/R43 application ownership is demonstrated without adding host account, enrollment, database or job subsystems. Overall requirement completion remains a manager/integration judgment after the other groups' host bridges and combined acceptance tests.
