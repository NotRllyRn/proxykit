# Safe installed-extension activation

## Implemented contracts

One server/listener and one stable reverse proxy publish immutable native engines.
The host/author HTTP API remains v1; the author handler signature, snapshots, hooks,
managed-call helper and IPC messages are unchanged. **`apiVersion: 1` is required**
in every new manifest, independently of `manifestVersion: 1`, package SemVer and
IPC `v:1`. Missing/unsupported API majors fail install before code executes. SDK
file names or copied helpers never imply compatibility.

```
proxykit extension install https://github.com/example/repository --trust
proxykit extension status
proxykit extension enable example-extension
proxykit extension disable example-extension
proxykit extension status example-extension
proxykit extension remove example-extension
# Explicit convenience on a running server:
proxykit extension install https://github.com/example/repository --trust --activate
```

- New installs are **disabled**, including when a server is running. `--activate`
  performs install, then enable. Activation failure leaves the installed package
  disabled; it does not uninstall it. Offline `--activate` installs disabled and
  returns an online-required error, rather than queueing activation.
- Online enable/disable prepares and validates the **complete** pipeline, persists
  intent, and publishes it at one ingress-gated commit point. Responses confirm
  commit, not completion of old work. Failure before commit leaves prior intent
  and pipeline unchanged; required dependency/security hooks are never silently
  removed. A malformed enabled-state file or a missing installation with enabled
  intent fails closed; restore missing packages before activation.
- Offline enable/disable persists intent only. Full composition validation occurs
  on the next startup, before serving. `status [ID]` is a readable table separating
  installed, enabled, active, draining, incompatible, failed and cleanup state.
  Offline runtime fields are `unknown`, not `false`. `list` retains the legacy
  offline JSON inventory; online it reports the diagnostic status table.
- Decisions in `.enabled.json` are keyed by ID **and immutable commit**. An old
  enabled decision cannot authorize different code. Removal first persists a
  disabled decision, preventing stale authorization if reinstall crashes.
- Installed incompatible/malformed records remain visible to diagnostic status
  and never execute. An installation without an explicit matching decision is
  **unresolved legacy**, not implicitly enabled or harmlessly omitted: startup
  refuses to serve. Inspect offline status, explicitly disable/quarantine those
  records, then restart or remove/reinstall compatible code. Explicitly disabled
  incompatible records do not block other compatible extensions.

## Lifetimes and stateful resources

Every external request selects and leases its generation **before** authentication,
binding and rewrite. It retains it through body forwarding, Finish, Delivery and
upgraded-connection lifetime. Native descendants and capability-scoped ordinary
HTTP reentry select the original generation too. Missing/expired/forged capabilities
cannot fall back to a new external root. A parent handler's terminal reply is not
proof that its callback or native invocation has actually settled.

Unchanged healthy workers share one process across all referencing generations:
setup runs once; PID, module state, fixed ports, store locks and jobs survive
unrelated activation. Shared native invocation admission (default 64) bounds work
across engines, rather than multiplying capacity per generation. Worker invocation
and callback capacities remain process-wide. A canceled invocation retains admission
until the result/error or process reaping **and** callback settlement; transport
completion alone does not release all of those owners.

Worker cleanup runs only after no current/draining generation references it and
old generation work has settled. There is no automatic worker restart, call replay,
dependency removal, state migration or overlapping replacement. For changed code:
**disable → wait for no draining/cleanup/reaping owner → remove → install → enable**.
A healthy identical worker can be re-enabled/reused during drain; different code or
configuration under a still-owned ID is rejected. Resource migration and partial
setup cleanup remain the author's responsibility. Arbitrary setup side effects
are not a transaction: register cleanup promptly and unwind partial acquisition.

## Bounds, expiry and shutdown

CLI defaults (positive overrides only):

| Bound | Default / behavior |
|---|---|
| `-extension-drain` | 30s per retired generation |
| `-extension-cleanup` | 5s engine settlement phase, then 5s parallel worker cleanup phase |
| `-extension-max-draining` | 4 old generations; reject further activation at the limit |
| Live workers | 64 across current, draining, preparing and stopping/reaping owners |
| Published pipeline hooks | 512 per immutable engine |
| Enabled-state file | 1 MiB |

At most current + four draining pipelines and one transient preparing pipeline exist
under defaults. Activation serializes; setup occurs off ingress's selection lock.
At the drain deadline old ingress/child work is canceled and status records forced
retirement and interruption count. An SSE stream or upgraded connection can be
interrupted: finite drain does **not** promise infinite old-session continuity.
Uncooperative Node cancellation can kill a worker shared with the current pipeline;
that failure is visible in status and follows the existing required/failOpen hook
policy, never an activation-induced silent bypass. Automatic recovery is not implied.
Workers still reaping (including aborted candidates) retain registry ownership and block removal/replacement. If engine cleanup expires before actual invocation/callback settlement, that generation remains quarantined against the draining bound until settlement; a timeout never frees hidden capacity. A caller deadline still bounds `Close`, which can return a timeout instead of claiming complete cleanup.

Server shutdown stops HTTP ingress (existing server HTTP drain is 10s), closes
upgraded connections and retires runtime generations. Runtime teardown uses bounded
drain/cleanup; ownership remains held through teardown. Abnormal process exit
releases the OS lock. `SIGKILL` cannot run author cleanup or undo setup side effects.

## Ownership, control and persistence

Supported management platforms: Linux, macOS and FreeBSD on a local filesystem
supporting Unix sockets and `flock`. Other platforms explicitly reject store
ownership; network-filesystem/distributed locking is not supported. The configured store's ancestor directories must also be trusted (not replaceable by an untrusted account). Embedders of
`nodehost.OpenRuntime` must hold `extensions.Lock(store)` for the full lifetime
(including control shutdown and worker cleanup). `Installer.Owned` is an explicit
caller assertion, not lock acquisition. Single-generation `nodehost.Open` also
respects persisted enabled decisions.

The server owns `.owner-lock` for its lifetime; offline CLI mutation needs the
same nonblocking OS lock. Never unlink that lock file. A busy CLI uses only the
owner's private `.control/runtime.sock`; failed online communication **never**
falls back to offline mutation. Invalid store ownership/permissions fail immediately.
The store/control directory are private (0700), socket is 0600, symlink or nonsocket
control paths are rejected, stale sockets are removed only under store ownership,
and Linux additionally checks peer UID. Management accepts only bounded strict
JSON commands over the Unix socket, never a public proxy route. Lost replies may
represent committed operations: reconcile with status, not blind install retries.

**Trust boundary:** extensions run as the server identity. Same-UID trusted code
can access the socket, filesystem and host resources; neither the socket nor
`--trust` is a sandbox against malicious installed code. Protect the service account.

State persistence syncs a private temporary file then atomically renames it; rename
is the commit point and publication has no subsequent fallible step. This supports
process-crash recovery toward committed intent, not a multi-file installation or
power-loss transaction (directory metadata is not fsynced). A crash between package
rename and disabled-decision commit yields an unresolved, nonexecuting record.
Do not edit installed files, provenance or state by hand.

## Observable verification

`nodehost/runtime_test.go` exercises repeated stateful PID/port/lock/job reuse,
required-dependency rollback, setup and injected persistence rollback, native and
ordinary-HTTP descendants across activation, SSE drain, upgrade lifetime, shared
admission, forced retirement and required worker failure. `extensions/state_test.go`
covers API rejection, incompatible/legacy inventory and cross-process ownership /
crash release. `control/control_test.go` covers private paths, stale sockets, strict
commands and ownership. The compiled CLI system test verifies disabled install,
online enable/disable/re-enable on one listener, second-owner refusal and SIGTERM
cleanup. These synthetic fixtures do not claim validation against external broker
services or transactional migration of arbitrary extension-owned resources.

Implementation verification in the isolated Linux checkout (Go 1.27.1, Node
24.16.0): `go test ./...`, `go test -race ./...`, `go vet ./...`, and
`node --test sdk/*.test.mjs` passed. Race tests for nodehost/control/extensions/CLI
also passed five repetitions. Linux CLI build passed; macOS arm64 and FreeBSD
amd64 CLI cross-builds passed (compile evidence only, not runtime testing there).
