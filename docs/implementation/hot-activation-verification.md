# Hot activation integration verification

## Reviewed scope

Implementation: `d50710d`, `4460b18`, `6e77978`, `462c39b`; final CLI store-ownership correction: `ae061aa`. The existing broker trailer-presence correction `ade6cc9` is included, not duplicated.

Independent review identified premature CLI store-lock release after a timed-out runtime close. `ae061aa` transfers release to eventual teardown completion, with a deterministic blocked-stderr CLI regression. A second independent review approved that correction without remaining findings.

On integrated `ae061aa`, the parent independently passed:

- `go test -race -count=1 -timeout 180s ./...`
- `go vet ./...`
- CLI build
- All three SDK tests, including TypeScript compilation (no skips)

## Actual Broker Proxy compatibility

A separate tester archived committed Broker Proxy `51a6c45fcc320fca46867fdc2abd9a9b0914dbba` into a temporary directory, without changing its active checkout. Compatibility evidence was pinned to ProxyKit `462c39b` (before the CLI-only teardown correction); do not interpret this as a broker-system rerun at `ae061aa`.

Only contract/harness adaptations were required:

1. Add `"apiVersion": 1` to its manifest.
2. Explicitly enable after installation; startup alone no longer activates newly installed packages.

No broker application-code changes were required. Its system harness, demo and README must adopt the enable step. The original missing-version package was correctly rejected.

Results: 75 broker tests passed; three adapted installed-system runs passed, including one with installer source and SDK pinned to the same core revision. The real broker retained its worker PID, both listener sockets, store-lock inode, authenticated in-memory admin session and account/pulse state while an unrelated extension was enabled and disabled. That extension's response header appeared/disappeared as expected.

The system test exercised disabled startup and online activation, authentication, credentials and trailers, opaque SSE, cancellation, account selection, safe-method fallback, fixture enrollment, pulses, persisted restart and cleanup. Installation used the real Installer with a controlled archive transport; live GitHub downloading and live-provider compatibility were not certified. Credentials were synthetic. Temporary processes were cleaned up.

## Remaining boundaries

See [hot activation contracts](../HOT_ACTIVATION.md). Finite retirement can interrupt sessions; stateful replacement requires disable/drain before reinstall. Arbitrary setup side effects are not transactional. The broker companion repository still needs to commit the manifest and activation migration and rerun against the published upstream revision; this evidence does not claim that migration is already published.
