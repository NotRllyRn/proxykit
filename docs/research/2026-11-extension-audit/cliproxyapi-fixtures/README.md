# CLIProxyAPI audit fixture reproduction

Use a disposable checkout at `91e0dee827abf476f9f86246f68c500a112f7155`. Do not install these fixtures as production plugins. This plugin deliberately accepts synthetic test requests, rewrites usage and returns policy errors to expose host behavior. It performs no real account login or provider calls.

The C file is a minimal native ABI probe. It logs native method requests to `CPA_AUDIT_LOG`, accepts/denies frontend auth based on a synthetic header, implements error/termination hooks, rewrites a response, drops stream chunks, publishes management/resource fixtures, and issues one nested host model callback. The Python fixture starts the compiled CLIProxyAPI server and an ephemeral loopback mock with two synthetic upstream credentials; it captures all test observations and terminates both processes. Native capability wire names differ from Go interface field names.

From the temporary CLIProxyAPI checkout:

```sh
mkdir -p /tmp/cpa-audit/plugins /tmp/cpa-audit/auths
go build -o /tmp/cpa-audit/cli-proxy-api ./cmd/server
gcc -shared -fPIC -pthread -o /tmp/cpa-audit/plugins/audit.so /code/proxykit/docs/research/2026-11-extension-audit/cliproxyapi-fixtures/audit_plugin.c
python3 /code/proxykit/docs/research/2026-11-extension-audit/cliproxyapi-fixtures/runtime_probe.py
```

Outputs go to `/tmp/cpa-audit`; the fixture chooses available loopback ports. To reproduce temporary added contract probes, copy `audit_contract_test.go` into `internal/pluginhost/` in that same disposable checkout, then run:

```sh
go test ./internal/pluginhost -run '^TestAudit' -count=1 -v
```

The policy regression proves error return ignores even a simultaneous termination value, retains an earlier plugin's compaction, and lets a later observer see the modified body. The cancellation regression synchronizes 24 concurrent calls, cancels their callers, and shows the inner work has not terminated; it releases all work before test cleanup. It does not intentionally hang a native library or rely on wall-clock sleep for correctness.

`runtime-results.jsonl` and `observations.json` distinguish expected 401/403/404 from failures. All credentials and user messages are synthetic. The fixture uses `--local-model` and disables management panel downloads, but does not establish a whole-process network sandbox for unrelated background metadata updates.
