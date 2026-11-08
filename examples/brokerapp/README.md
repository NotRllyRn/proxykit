# Trusted broker application proof

An independent Go library/example, **not a second HTTP proxy or a mandatory host account service**. Run its executable proof with:

```sh
go test ./examples/brokerapp
go test -race ./examples/brokerapp
go vet ./examples/brokerapp
```

The tests use local HTTP upstreams, loopback listeners, generic labels and synthetic credentials only. There are no external services or dependencies.

## Small integration surface

1. `OpenStore(privateDirectory)` loads extension-owned committed accounts and pending notifications. Exactly **one process / Store instance** may own that directory, including during recovery. Existing directory permissions are the operator's responsibility. Files are private (`0600`), but credentials are plaintext, not encrypted.
2. `Store.Activate(Account)` atomically commits account state, labels/ACLs and an outbox notification. Trusted service-authenticated `POST /enroll` uses exactly this operation, including activation and deactivation. It is not OAuth or an account-verification flow.
3. `New(store, Config)` copies caller-token identity mappings and exact allowed endpoints. `Refresh` and `Notify` are trusted cancellation-aware callbacks. Refreshed credentials are persisted with a revision check, without overwriting concurrent enrollment changes.
4. `Broker.Execute(request, requestedModel, maxAttempts, Dispatcher)` authenticates the original request; selects by stable account-ID order; binds a fresh request-local account/provider/model/endpoint/credential; and returns sanitized per-attempt outcomes. Fallback uses only previously untried accounts and **rechecks committed ACLs and destination policy**. Requested model is provenance, not authority; configured served models may differ. Selection policy is intentionally simple, not quota optimization. A dispatch error, invalid status or 5xx permits another attempt within the explicit limit; no client delivery occurs here. Streaming, replayable request bodies, response commitment and retry safety remain host bridge responsibilities.
5. `Start(broker, catalogListener, enrollmentListener, serviceToken, retryInterval)` takes ownership of two separate listeners and starts a notification retry job. `GET /catalog` requires caller authentication and shows only eligible labels/routes/quota. `/enroll` requires a distinct service credential; caller credentials cannot enroll, and the service credential cannot impersonate a caller. Enrollment accepts bounded JSON and rejects unknown fields/trailing values. No caller-supplied identity header or body field is trusted.
6. `Application.Close(ctx)` **or** `Broker.Close(ctx)` stops admission, cancels requests/refresh/jobs, closes owned listener connections, and waits for owned work. Only one Application may start per Broker. Retry after a close timeout is supported; cancellation cannot forcibly terminate trusted callbacks. Caller-owned dependencies, such as a dispatcher's HTTP transport, remain the caller's cleanup responsibility.

`Dispatcher` is deliberately a minimal **trusted managed-dispatch bridge**, not an ordinary observer. It alone receives the credential-bearing `Attempt`. A real bridge must use host-managed execution/observations; build fresh authentication for the exact selected destination; not clone inbound auth/identity headers; disable redirects or reauthorize each hop; and propagate context cancellation. `TestIdentityAccountIsolationAndLocalUpstreams` demonstrates a local `net/http` bridge with fresh requests and redirects disabled. The library cannot police arbitrary sockets opened by trusted Go code.

`Outcome` contains attributed caller/account/requested/served model, status and optional provider usage. It has no credential, raw callback error, request body, inbound headers or URL. Callback errors become fixed sentinel errors. The library does not log. Callbacks must not log or retain secrets. `QuotaSnapshot` has its own observation timestamp and remaining counter; it is never converted into `Usage`. Unknown provider usage is `nil`, not zero. Quotas are in memory in this proof and updated through a trusted application method, not an implemented provider polling service.

## Durability and notification semantics

Each transaction writes a private staging file, fsyncs it, atomically renames it over `broker.json`, then fsyncs the directory. The in-memory snapshot is published **only after success**. A persistence failure poisons the current Store: routing, catalog and notification delivery fail closed until recovery. In particular, an error after rename but before successful directory fsync is an **uncertain commit**, not a guaranteed rollback: recovery may find the complete new transaction. Do not retry uncertain activation as though it definitely did not happen.

Restart loads either complete committed state; interrupted staging files are ignored (not automatically garbage-collected). The file/directory must reside on storage with the tested atomic-rename/fsync semantics; no Windows/network-filesystem or power-loss certification is claimed. There is no multi-process lock, distributed lease, multi-replica consistency, migration or corruption repair.

Activation and notification creation are one transaction. Notification delivery and durable acknowledgement are necessarily separate: a crash or ack failure can cause duplicates. `Notification.ID` is stable and monotonic within one store, not globally across independent stores. Receivers must deduplicate by store identity + notification ID. The subprocess crash test records receiver receipt, exits before ack, reopens the store, verifies the same ID is retried, and demonstrates receiver deduplication. Pending entries remain on callback failure/cancellation and retry at the configured interval. No exactly-once delivery claim is made.

There is no OAuth, real account integration, dashboard, process sandbox, production secret store, notification queue bound, or production multi-replica store. Protect remote listeners with TLS and service credential provisioning; the shipped tests exercise loopback HTTP only. See `docs/implementation/brokerapp.md` for requirement evidence and host bridge needs.
