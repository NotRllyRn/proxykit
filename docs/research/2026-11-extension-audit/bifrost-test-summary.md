# Bifrost local test evidence

Target commit: `6e640279b36337820f2088fb85f6e78f339063c3`; Go `go1.27.3 linux/amd64`. No live provider/account was used. Repository implementation and dependency files were not changed; only three existing test files in `/tmp/proxy-research-1` were extended. Research probes are test-only and exempt from the target's live provider harness requirement.

**All final selected runs passed: 54 named top-level tests, including eight audit-added tests.** The inherited non-verbose selection also passed and is recorded separately; it overlaps the later selection and is not added to the 54 count. Passing audit probes sometimes deliberately pin an undesirable observed behavior (result loss, fail-open policy, unbounded fan-out); they do not indicate acceptance criteria were met.

## Commands and coverage

Run from `/tmp/proxy-research-1/core` unless another module is named.

| Log | Command | Result and boundary |
|---|---|---|
| `bifrost-mcp-tests.log` | `go test ./mcp -run 'TestAgent\|Test.*Plugin' -count=1` | Inherited parent test passed in 0.155s after toolchain/dependency download. Original terminal ID was unavailable in this agent; process inspection proved it was active, and no duplicate was started while active. |
| `bifrost-mcp-expanded.log` | `go test ./mcp -run 'TestExecuteAgent\|TestParallelToolCalls\|Test.*Plugin' -count=1 -v` | 17 named tests passed; agent API/basic tool stopping, unique parallel log IDs, and selected plugin/permission protections. Not the entire MCP suite. |
| `bifrost-probes-mcp.log` | `go test ./mcp -run TestProxyKitAudit -count=1 -v` | Six added tests passed, observations below. |
| `bifrost-probes-core.log` | `go test . -run 'TestProxyKitAudit\|TestRunPreRequestHooks_CommitsRoutingPinnedKey' -count=1 -v` | Added policy probe and existing routing-pin test passed. |
| `bifrost-scenarios.log` | `go test . -run 'TestScenarioExhaustedFallbacksReportEachAttempt\|TestScenarioPinnedRuleOnStreams\|TestScenarioZeroRetriesStillWalksDeadKeys\|TestExecuteRequestWithRetries_StopsAfterInjectedToolRan\|TestResponsesRequest_GatewayToolSearch' -count=1 -v` | Five tests passed using synthetic callbacks/local mock providers. Streaming case tests fallback before first chunk and error delivery after first chunk. No live provider correctness claim. |
| `bifrost-transport-local.log` | In `transports`: `GOWORK=/tmp/proxykit-bifrost-audit.work go test ./bifrost-http/handlers ./bifrost-http/lib -run 'TestTransportPreAuth\|TestTransportResponseHeaders\|TestResponseHeadersHook\|Test.*StreamChunk\|TestSortAndRebuildPlugins' -count=1 -v` | 20 named tests passed: pre-auth credential/body mutation, response/error short circuits, 409 path rejection, header hooks before SSE, reverse order, repeated-header preservation, short-circuit header phases and plugin placement. The regex does not establish every possible stream payload case. |
| `bifrost-probes-stream.log` | In `transports`: `GOWORK=/tmp/proxykit-bifrost-audit.work go test ./bifrost-http/lib -run TestProxyKitAuditChunkSemantics -count=1 -v` | Four stream-interceptor subcases passed in one added test; direct hook pipeline, not a full network payload-abort client test. |
| `bifrost-observability.log` | In `framework`: `GOWORK=/tmp/proxykit-bifrost-audit.work go test ./tracing -run 'TestCompleteAndFlushTrace_BoundsInjectsPerPlugin\|TestCompleteAndFlushTrace_InjectTimeoutReleasesSlot\|TestSetObservabilityPlugins_HonoursDeclaredLimits' -count=1 -v` | Three tests passed: saturated exporter drops excess; configured limits honored; context-aware timeout releases slot. Does not prove forced preemption of a context-ignoring callback or durable delivery. |

In the Markdown table the `\|` escapes display a literal `|`; the actual shell patterns used ordinary `|` inside single quotes.

## Audit probes and observations

1. `TestProxyKitAuditMixedHandoff`: one `gateway_tool` plus `client_tool` in one model response. Gateway runs once, no continuation model call, client call ID retained, finish reason `stop`, gateway result appears as text summary.
2. `TestProxyKitAuditRepeatedToolSummaryLoss`: two gateway `lookup` results with distinct call IDs. The first unique result disappears and second survives in one name-keyed text summary. This intentionally confirms a defect/limitation rather than failing the test harness.
3. `TestProxyKitAuditCanceledContextStillDispatches`: already-canceled context still invokes mock tool and continuation callbacks; cancellation remains visible to the continuation. Bounds the loop-level claim; real tool-manager/provider callers can independently enforce cancellation.
4. `TestProxyKitAuditToolFanout`: 64 gateway callbacks wait on a barrier; all enter before any completes. Demonstrates no cap at this loop boundary. This does not prove a downstream MCP server lacks its own concurrency limit.
5. `TestProxyKitAuditAgentUsageIncludesContinuation`: initial 12-token usage plus 23-token continuation produces total 35; continuation input preserves gateway tool call/result pairing. This is synthetic accounting arithmetic, not a billing/provider validation.
6. `TestProxyKitAuditRepeatedToolResponsesSummaryLoss`: same duplicate-name result loss on Responses output; pending `client-id` survives. No client-side replay or restart test.
7. `TestProxyKitAuditPolicyErrorAndShortCircuit`: returned Go policy error preserves mutation and runs later pre-hook; post order reverses. Explicit deny prevents later pre-hook and pairs only the entered policy post-hook. Does not test Go panic recovery.
8. `TestProxyKitAuditChunkSemantics`: pass => reverse `[last first]`; drop => nil/no error and stop chain; error+chunk => retain chunk/error and stop chain; abort => nil/error and stop chain. Handler source establishes warn/send versus error/cancel/drain behavior; existing SSE header tests separately validate actual HTTP header timing.

## Reproducibility and setup history

`bifrost-probes.patch` applies to the exact clean target commit and contains only additions to `core/bifrost_test.go`, `core/mcp/agent_test.go`, and `transports/bifrost-http/lib/config_test.go`. Probe fixtures are inline synthetic structs/callbacks; no account secrets or external test data are needed. Apply it only in a disposable checkout, then run the commands above.

The checkout has no active checked-in `go.work`. Cross-module tests initially started without a workspace would resolve published `bifrost/*` module versions, which is unsuitable evidence for this exact commit. That early transport invocation was canceled without using any result. A temporary `/tmp/proxykit-bifrost-audit.work` was generated referencing the checkout's local module directories, and the tests rerun against local sources. Its exact content is preserved as `bifrost-audit.go.work.txt`; when reproducing elsewhere, replace `/tmp/proxy-research-1` with the disposable checkout path. This explicit workspace can affect module version selection; its contents are part of the evidence. No `go mod tidy`, upgrades, or production dependency-file edits were made. Go downloaded the required toolchain/modules into normal caches.

One draft audit probe initially referenced `BifrostRequest.Model`; compilation showed that the actual request envelope carries `ChatRequest.Model`. The test-only probe was corrected before the final successful run. This was a probe construction error, not a target regression. No unfinished compilation/test blockers remain.

`bifrost-evidence-manifest.json` records target identity, toolchain, raw log SHA-256 values, named test counts and patch hash. Logs include dependency download lines where applicable.

## Not executed / not established

- Full repository test suite, live provider harness, production load benchmarks, or race detector.
- Live Codex/ChatGPT, enrollment/token rotation, account ACL administration, enterprise RBAC or real provider billing.
- Durable context archive implementation, restart/replay, cross-tenant isolation, concurrent archive revisions, signed/opaque compaction preservation.
- Streaming autonomous MCP mixed-tool loop (no such loop in reviewed source), full stream payload edit/terminal-object reconciliation, arbitrary protocol or plugin-owned route integration.
- Native `.so` binary build/load, WASM runtime, hot-reload teardown races, Sidekiq durable-job failover.

These gaps are retained in the findings; no passing unit test is presented as evidence for them.
