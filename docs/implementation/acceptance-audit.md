# Extension acceptance evidence — independent audit

Status: partial acceptance, not R01–R50 completion. This is the first independent manager/worker audit at baseline `1aa85ca`, with corrections `e60e121` and `6cfba27`. Installed Node activation was subsequently merged as `8f36088`; a separate audit is examining that boundary. Combined broker + AI + recall acceptance is also still running. See [current tracker](../IMPLEMENTATION_TRACKER.md).

The audit accepts tested native execution boundaries and bounded application examples. No new blocking defect was established in the declared single-source managed-AI slice. Missing optional capabilities do not justify new mandatory core frameworks.

## Classification

- **I**: implemented, tested boundary.
- **E**: example application, not mandatory core functionality.
- **C**: conditional support or explicit rejection of an incompatible mode.
- **U**: unsupported capability.
- **M**: missing acceptance evidence.

Mixed labels do not equate explicit rejection with feature implementation. Paths are repository-relative; test names are exact. These are local fixtures, not live-provider certification.

| ID | Classification and scope | Implementation → test evidence |
|---|---|---|
| R01 | I/E — inbound authentication and scoped reentry | `extension/reentry.go` → `proxy/execution_test.go::TestExecutionSocketAuthenticatedOrdinaryHTTPReuse` |
| R02 | I/E — request-local account and upstream | `examples/brokerapp/broker.go` → `examples/brokerapp/failure_test.go::TestConcurrentIdentitiesKeepRoutesLocal` |
| R03 | I/E — replacement, refresh and revocation | `examples/brokerapp/broker.go` → `examples/brokerapp/host_security_test.go::TestHostRefreshSingleflightAndSanitizedFailure` |
| R04 | E — application catalog, labels and ACLs | `examples/brokerapp/store.go` → `examples/brokerapp/broker_test.go::TestIdentityAccountIsolationAndLocalUpstreams` |
| R05 | E — enrollment and separate service authentication | `examples/brokerapp/application.go` → `examples/brokerapp/failure_test.go::TestSingleApplicationAndServiceCredentialSeparation` |
| R06 | E/M — separate listener; standalone deployment unproven | `examples/brokerapp/application.go` → `examples/brokerapp/broker_test.go::TestEnrollmentAuthSeparateListenerAndLifecycle` |
| R07 | E/C — activation/outbox, not exactly-once effects | `examples/brokerapp/store.go` → `examples/brokerapp/broker_test.go::TestCrashAfterNotificationBeforeAck` |
| R08 | I/E — quota distinct from usage/outcome | `examples/brokerapp/broker.go` → `examples/brokerapp/broker_test.go::TestCatalogQuotaIsNotUsage` |
| R09 | I/E/C — detached edits and supported semantic subset | `ai/protocol.go` → `ai/protocol_test.go::TestRequestTranslationAndValidation` |
| R10 | I — bounded nonstream replacement/recovery | `extension/body.go` → `extension/engine_test.go::TestWholeResponseRecoveryAndInvalidAtomicCommit` |
| R11 | I/C — bounded live byte-unit edits, not arbitrary framing | `extension/body.go` → `extension/engine_test.go::TestStreamingRecoveryAndOriginalRecorder` |
| R12 | I/E/C — managed hidden calls, not private sockets | `ai/host.go` → `examples/aihost/app_test.go::TestRealSocketMixedRestartJSONAndSSE` |
| R13 | I/E/C — optional protocol interface and toy adapter | `ai/protocol.go` → `examples/aihost/custom_test.go::TestCustomProtocolRegistrationUsesSameHostPipeline` |
| R14 | E — application-owned HTTP endpoints; no dashboard | `examples/brokerapp/application.go` → `examples/brokerapp/broker_test.go::TestEnrollmentAuthSeparateListenerAndLifecycle` |
| R15 | I/C/U/M — opaque upgrades; no frame mutation or own-service WebSocket fixture | `extension/exchange.go` → `proxy/execution_test.go::TestExecutionSocketUpgradePassthroughAndExplicitTransformRejection` |
| R16 | E/C — local durable tenant-scoped recall, not distributed storage | `examples/recall/store.go` → `examples/recall/store_test.go::TestArchiveRestartAndCrossProcessConflict` |
| R17 | E — archive before reduction | `examples/recall/store.go` → `examples/aihost/reduction_test.go::TestSocketReductionArchivesBeforeChangingPrivateContext` |
| R18 | E/C — local CAS/locking, uncertain-work quarantine | `examples/recall/store.go` → `examples/aihost/durable_test.go::TestSocketCrashUncertainOwnedEffectRejectedOnRestart` |
| R19 | E/C — supported exact replay/private continuation | `ai/coordinator.go` → `ai/coordinator_test.go::TestCoordinatorMixedReplayBothWires` |
| R20 | C/U — unsupported provider-held/signed reduction rejects | `ai/protocol.go` → `ai/protocol_test.go::TestUnknownOpaqueAndUsage` |
| R21 | E — owned tools validated before effects | `ai/coordinator.go` → `ai/budget_test.go::TestAllSelectedToolValidationPrecedesEffects` |
| R22 | E — bounded nonstream tool loop | `ai/coordinator.go` → `ai/coordinator_test.go::TestCoordinatorMixedReplayBothWires` |
| R23 | E/C/U — buffered SSE; no live semantic continuation | `ai/host.go`, `ai/stream.go` → `examples/aihost/app_test.go::TestRealSocketMixedRestartJSONAndSSE` |
| R24 | E — mixed tool IDs, ordering, client handoff | `ai/coordinator.go` → `ai/coordinator_test.go::TestCoordinatorLimitsCancellationAndDuplicateIDs` |
| R25 | I/E — trusted identity separate from model arguments | `examples/aihost/app.go` → `examples/aihost/failure_test.go::TestIngressRejectsSpoofedStateAndPreservesOrdinaryLivePath` |
| R26 | I/E/C — cancellation and stopped admission; cooperative native work | `extension/invocation.go` → `extension/invocation_test.go::TestInvocationEndCancelsAlreadyAdmittedCall` |
| R27 | I/E/C/M — dimensional/durable budgets, no monetary enforcement | `ai/budget.go` → `examples/aihost/durable_test.go::TestSocketDurableLimitsCannotResetOnRestart` |
| R28 | I/E — independent original/delivery evidence | `extension/exchange.go` → `examples/brokerapp/host_test.go::TestHostConcurrentCompositionOriginalAndDelivery` |
| R29 | I/E — original evidence survives presentation edits | `ai/host.go` → `extension/source_test.go::TestOriginalSourceSurvivesEffectiveMetadataChanges` |
| R30 | I/E/C — internal/candidate accounting; ambiguous multi-source success rejects | `ai/host.go` → `examples/aihost/bridge_evidence_test.go::TestSocketNestedResponseReplacementRejectsMultipleOriginalSources` |
| R31 | I/E — attempt lineage and separate external delivery | `extension/delivery.go` → `proxy/delivery_test.go::TestDeliveryOnlyExternalRootNotConsumedChild` |
| R32 | E — unknown usage differs from zero | `ai/types.go` → `ai/protocol_test.go::TestUnknownOpaqueAndUsage` |
| R33 | I/E/C — required capture/sink failure denies selection/effects | `ai/host.go` → `examples/aihost/bridge_evidence_test.go::TestSocketRequiredRecordingFailureAtSelectedAndResumedInference` |
| R34 | I/E — policy denial before upstream | `extension/exchange.go` → `extension/engine_test.go::TestTransportFailureAndPolicyFailureTerminalContracts` |
| R35 | I/C — blocking errors fail closed; explicit fail-open policy | `extension/engine.go` → `extension/engine_test.go::TestLateFailOpenResultCannotMutateAcceptedState` |
| R36 | I/C/M — native admission/worker bounds; installed audit pending | `extension/engine.go` → `extension/invoke_order_test.go::TestInvokeReleasesAdmissionBeforePublication`; `worker/worker.go` → `worker/worker_test.go::TestDefaultTimeoutKillsUncooperativeAndRestart` |
| R37 | I — frozen deterministic ordering | `extension/engine.go` → `extension/engine_test.go::TestOrderingValidationAndFrozenRegistration` |
| R38 | I/E/C — cooperating application ownership/CAS, not arbitrary-code arbitration | `examples/recall/store.go` → `examples/recall/store_test.go::TestArchiveReduceCASIsolationAndFailure` |
| R39 | I/M — native frozen registration; installed audit pending | `extension/engine.go` → `extension/engine_test.go::TestOrderingValidationAndFrozenRegistration` |
| R40 | I/E/C — lifecycle, refresh/outbox; cooperative work | `examples/brokerapp/application.go` → `examples/brokerapp/failure_test.go::TestCloseCancelsNotificationAndRetryHasNoLostOutbox` |
| R41 | U — no untrusted sandbox | `worker/worker.go`, `extensions/install.go`; no sandbox enforcement test |
| R42 | I/C/M — local-response pipeline, no cache implementation | `extension/exchange.go` → `extension/engine_test.go::TestLocalAndChildFullChainsAndProvenance` |
| R43 | I/E/M — separate broker/AI adapters; combined acceptance pending | `examples/brokerapp/host.go` → `examples/brokerapp/host_test.go::TestHostConcurrentCompositionOriginalAndDelivery`; `examples/aihost/app.go` → `examples/aihost/app_test.go::TestRealSocketMixedRestartJSONAndSSE` |
| R44 | E/C — supported translation, unsafe envelopes reject before tools | `ai/protocol.go`, `ai/wire.go` → `ai/correction_test.go::TestWireEnvelopeRejectsBeforeOwnedTools`; `ai/rejection_test.go::TestConservativeUnsupportedAndMalformedRejection` |
| R45 | I/E/C — precommit gating, bounded safe-method status fallback | `examples/brokerapp/host.go` → `examples/brokerapp/host_replay_test.go::TestHostReplayStartsBeforeTransformsAndRejectsEffectivePost`; `proxy/proxy.go` → `proxy/informational_test.go::TestInformationalSuppressionIsExplicitAndManaged` |
| R46 | E/C — classifier/candidates and loser cancellation, not general timed hedging | `ai/routing.go` → `examples/aihost/failure_test.go::TestSocketCandidateLoserCancellationAndAccounting` |
| R47 | E/C/M — local JSON/SSE fixtures, no live certification | `ai/protocol.go`, `ai/stream.go` → `examples/aihost/app_test.go::TestRealSocketMixedRestartJSONAndSSE` |
| R48 | E/C — portable migration/affinity; incompatible state rejects | `ai/routing.go` → `ai/routing_test.go::TestPrepareAffinityCapabilitiesCacheAndCommit` |
| R49 | I/E/C — application-bound destinations/credentials and reauthorization | `examples/brokerapp/host.go` → `examples/brokerapp/host_security_test.go::TestHostRejectsLateCredentialTrailers`; `examples/aihost/app.go` → `examples/aihost/authorization_test.go::TestSocketReauthorizesPreviouslySelectedContinuation` |
| R50 | I/E/C/M — effective request/served evidence; conditional key helper, not cache certification | `ai/host.go`, `ai/routing.go` → `extension/source_test.go::TestOriginalSourceSurvivesEffectiveMetadataChanges`; `ai/routing_test.go::TestPrepareAffinityCapabilitiesCacheAndCommit` |

## Acceptance-sensitive findings

1. `ai/host.go` persists observations then rejects ambiguous multiple upstream sources with `unsupported semantics: ambiguous multiple upstream sources`. A last decoded result does not escape as successful selection. Supporting fallback requires authoritative selected-source identity, not observation order.
2. `ai/routing.go::CacheKey` requires a previously resolved target. It validates tenant/conversation/account/endpoint/version/revision but does not independently reject missing provider/model/protocol. `Prepare` validates target identity. This is not a demonstrated leak under the helper's precondition, but neither a general scope validator nor an implemented cache. There is no cache-isolation certification.
3. Required evidence failures block selection; they cannot recover evidence lost before capture. Application CAS coordinates cooperating extensions; it does not arbitrate arbitrary trusted code. Managed suppression of informational responses is explicit, not rollback of previously forwarded 1xx.
4. Local persistence, no exactly-once effect guarantee, trusted-code execution, absent publication/dashboard and no live certification remain explicit limitations.

## Remaining integration gates

- Installed declarations through the CLI/native gateway: detached values, credential translation, cancellation, crash/restart, admission lifetime, frozen registration and shutdown. Bridge implementation is merged; independent audit is pending.
- Actual `brokerapp.Host` + AI + recall + independent recorder: classifier, candidates, selected and resumed calls must agree on account, endpoint, protocol and recorded served provenance.
- Fallback must reject ambiguity or expose authoritative selected-source identity. Broker transport errors remain ordinary 502; existing fallback is bounded safe-method HTTP-status fallback.
- Selected/resumed capture/sink failure or revoked authorization must admit no new owned effects or successful selection. Uncertain work stays quarantined.
- Disconnect/shutdown stops admission, cancels/joins managed work and releases listeners/workers/transports.
- Preserve opaque endpoints, HTTP/1 full duplex, HTTP/2 duplex/trailers, WebSocket pass-through and managed duplex/trailers regressions.

The audit manager independently passed focused cache/affinity, selected/resumed recording failure, nested-source rejection and admission-publication tests across `ai`, `examples/aihost` and `extension`. This audit is evidence for ongoing integration, not final certification.
