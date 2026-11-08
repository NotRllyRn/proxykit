# Extension requirement status

Current local implementation: `13c23ab` and its ancestors. All 11 Go packages passed normal and race tests, vet and build; all 3 SDK tests passed with no skips. These are local fixture results, not live-provider certification.

**Implemented** = tested and working within the stated boundary. **Implemented (example)** = working optional application code, not a default CLI service. **Partial** = working subset, with unfinished work named below. **Not implemented** = no implementation of that capability. No requirement is currently being actively worked on; unfinished rows are follow-up work, not “in progress.”

The [detailed audit](implementation/acceptance-audit.md) links each row to source and exact tests. The [tracker](IMPLEMENTATION_TRACKER.md) records verification commands; [combined-system evidence](implementation/system.md) covers the later integration tests. A working subset is not a checkmark for an entire broad requirement.

| ID | Requirement | Status | Tested scope / remaining work |
|---|---|---|---|
| R01 | Replace inbound authentication | Implemented | Trusted scoped reentry and authentication replacement. |
| R02 | Choose account / upstream | Implemented | Request-local selection; combined broker credentials verified on wire. |
| R03 | Replace credentials / refresh | Implemented (example) | Broker replacement, refresh, revocation and credential isolation. |
| R04 | Account catalog, labels, ACLs | Implemented (example) | Broker catalog and caller/account isolation. |
| R05 | Enrollment POST / separate service auth | Implemented (example) | Application-owned endpoints with separate authentication. |
| R06 | Separate enrollment listener / process | Partial | Separate listener works; standalone deployment/process proof remains. |
| R07 | Activation and notification outbox | Implemented (example) | Durable activation and crash recovery; not exactly-once notification. |
| R08 | Quota snapshots / attempt outcomes | Implemented (example) | Quota, measured usage and attempt outcomes remain distinct. |
| R09 | Rewrite requests / instructions / tools | Partial | Detached edits and supported AI subset work; arbitrary protocol semantics do not. |
| R10 | Modify nonstream responses | Implemented | Bounded atomic replacement and last-accepted-input recovery. |
| R11 | Modify live streams | Implemented | Bounded byte-unit edits/recovery; not whole-answer rollback or arbitrary semantic framing. |
| R12 | Output hooks on hidden tool loops | Implemented | Managed internal calls traverse hooks; private extension sockets are outside the contract. |
| R13 | Add wire protocols via plugins | Implemented | Optional protocol interface and custom-adapter fixture; each real adapter needs its own tests. |
| R14 | HTTP endpoints / dashboard | Partial | Application endpoints work; dashboard not implemented. |
| R15 | Own WebSocket service / mutate gateway frames | Partial | Opaque pass-through works; gateway frame mutation absent, own-service fixture missing. |
| R16 | Durable tenant-scoped storage | Implemented (example) | Local recall persistence and isolation; not distributed storage. |
| R17 | Archive before context reduction | Implemented (example) | Archive failure prevents reduction/inference. |
| R18 | Context revisions / concurrency | Implemented (example) | Local locking/CAS and uncertain-work quarantine. |
| R19 | Private continuation / client replay | Implemented (example) | Supported mixed-tool replay, including fresh-process continuation. |
| R20 | Compact provider-held / signed state | Not implemented | Unsupported state rejects safely; no signed/provider-held reduction or migration. |
| R21 | Executable internal tools | Implemented (example) | Owned tools and validation before effects. |
| R22 | Nonstream model/tool continuation | Implemented (example) | Bounded coordinator and managed dispatch. |
| R23 | Streaming model/tool continuation | Partial | Buffered split-SSE continuation works; live incremental semantic continuation absent. |
| R24 | Mixed client/internal calls and IDs | Implemented (example) | Ordering, IDs, private results and client handoff/replay tested. |
| R25 | Trusted caller/session for tools | Implemented | Trusted identity separated from model arguments; spoofed ingress rejected. |
| R26 | Cancellation through internal tools | Implemented | Managed cancellation stops admission and propagates; native callbacks must cooperate. |
| R27 | Bound turns, spend and parallel work | Partial | Call/tool/round/concurrency/output/lifetime limits work; monetary spend enforcement absent. |
| R28 | Independent observation extension | Implemented | Original and delivery observations; combined independent recorder tested. |
| R29 | Actual usage before presentation edits | Implemented | Original usage 7 remains distinct from decorated client usage 900. |
| R30 | Internal-call / attempt accounting | Partial | Managed classifier/candidates/continuations recorded; ambiguous multi-source selection rejects, not attributed successfully. |
| R31 | Lifecycle / attempt correlation | Implemented | Attempt lineage and separate external delivery evidence; not peer acknowledgment. |
| R32 | Unknown usage versus zero | Implemented | Missing usage stays unknown, including failure/cancellation evidence. |
| R33 | Durable telemetry / pre-capture loss | Partial | Required synchronous recording gates selection/effects; lost pre-capture evidence cannot be recovered. |
| R34 | Policy denial before upstream | Implemented | Blocking policy runs before dispatch. |
| R35 | Fail closed on mandatory-policy errors | Implemented | Blocking errors deny; late fail-open output cannot mutate accepted state. |
| R36 | Bound hooks / workers | Implemented | Native/worker admission, timeout lifetime and bounded error frames tested; not forced native termination. |
| R37 | Deterministic extension order | Implemented | Frozen ordering and invalid-registration rejection. |
| R38 | Cooperating extensions / compactor choice | Implemented (example) | Explicit cooperating ownership/CAS; not arbitration of arbitrary trusted code. |
| R39 | Stable configuration during active work | Implemented | Frozen startup registration, restart-only activation; active-stream package-replacement stress fixture remains. |
| R40 | Background jobs / lifecycle | Implemented (example) | Refresh/outbox lifecycle, cleanup and cooperative shutdown; not a generic job scheduler. |
| R41 | Untrusted extension sandbox | Not implemented | Extensions run trusted code with server privileges; no isolation enforcement. |
| R42 | Cache / local short-circuit | Partial | Local response follows response hooks; cache storage, lookup and invalidation absent. |
| R43 | Full application + gateway adapter | Implemented (example) | Broker/AI/recall composition and installed Node adapter tested; applications are opt-in. |
| R44 | Target capabilities / settings translation | Partial | Supported Chat/Messages subset works; unsupported envelopes reject; broader settings need adapters/tests. |
| R45 | Inspect outcome then reroute precommit | Partial | Precommit gating, explicit rerouting and safe-method status fallback work; general multi-source/transport-error retry absent. |
| R46 | Classifiers, candidates, hedging, selection | Partial | Managed classifier/candidates, loser cancellation and selection work; general timed hedging absent. |
| R47 | Preserve client protocol across substitution | Partial | Local Chat/Messages JSON and buffered SSE fixtures work; broader protocols and live providers uncertified. |
| R48 | Conversation affinity / state migration | Partial | Supported portable history and affinity work; opaque/provider-held/signed migration rejects. |
| R49 | Destination credentials / authorization | Implemented (example) | Exact broker selection, wire credentials and continuation reauthorization; not atomic revocation with external effects. |
| R50 | Requested/served provenance / cache partition | Partial | Actual served provenance and conditional cache-key helper work; no implemented cache or cache-isolation certification. |

## Follow-up checklist

- [ ] Implement the missing portions of **Partial** rows; retain existing rejection and isolation tests.
- [ ] Decide whether to implement the explicitly unsupported R20/R41 capabilities; neither is required in the generic core.
- [ ] Add missing evidence: standalone enrollment deployment, own-service WebSocket fixture, active-stream configuration replacement, setup-failure injection and contradictory authoritative-route evidence.
- [ ] Validate real client/provider versions and deployment platforms before claiming compatibility.
- [ ] Separately scoped distribution work: dashboard and published npm/npx installation are not delivered. GitHub CLI installation and restart activation are implemented.

Update this table and the detailed evidence together when a follow-up lands. Do not promote a row based only on a plan, safe rejection, helper function or test name.
