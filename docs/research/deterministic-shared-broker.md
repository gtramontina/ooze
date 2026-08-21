# Deterministic simulation and the process-local admission broker

_Research date: 2026-08-21. This is a design input, not an implementation specification._

> **Resolution note:** The later [campaign transition-algebra decision](https://github.com/gtramontina/ooze/issues/57)
> pins FIFO exclusive-barrier semantics and composes the pure broker with a pure process-runtime core.
> [Managed-admission decision #58](https://github.com/gtramontina/ooze/issues/58) subsequently pins
> only two automatic admission states: start at aggregate detected capacity `P`, then irreversibly
> enter single-admission automatic after trustworthy hard shared exhaustion or after a primary
> deadline with recorded peer overlap disappears in exclusive confirmation. There is no
> ramp, fractional backoff, recovery, or confirmation toggle. A deadline without recorded peer
> overlap is classified directly after authoritative drainage;
> process-fuse normalization remains deferred. This report preserves the comparative research while
> using those resolved constraints below.

## Conclusion

Concurrent `Release` calls can share one process-local broker without creating nondeterministic campaign failures and without weakening deterministic replay.

The smallest sound design is:

1. model the broker as a small, deterministic state machine;
2. linearize every broker command at one explicit boundary;
3. let the broker own admission ordering, shared/exclusive arbitration, and starvation prevention;
4. let the simulation driver choose the order of genuinely concurrent input events, while the production adapter records the order it observed;
5. make contention wait for admission rather than become a campaign outcome; and
6. keep the process-wide production instance outside the pure broker, so each simulated world can construct an isolated broker state.

The determinism contract should be **same initial state plus same ordered input trace produces the same state, effects, and invariant diagnostics**. It should not promise that two independent real executions with concurrent callers will happen to choose the same schedule. Different legal linearizations may change admission timing, but must not manufacture a different kind of campaign failure.

Process-local exclusivity is therefore state to simulate, not an obstacle to simulation.

## Mutation-tool comparison: peers own one invocation

The mutation-testing peers inspected here generally follow the same shape: perform an initial/control phase, then create a worker pool owned by that invocation. Their baseline is phase-ordered before _their own_ mutant work; it is not an exclusive lease against another campaign in the same host process.

| Tool | Initial phase and mutant scheduling | Timeout/retry behavior relevant to Ooze | Coordination scope |
| --- | --- | --- | --- |
| PIT | Coverage/control completes before mutation analysis; each analysis then creates its own fixed-size executor and launches minion JVMs ([control phase](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/tooling/MutationCoverage.java#L158-L193), [executor](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/execute/MutationAnalysisExecutor.java#L22-L63)). | Timeout is a terminal mutant status, not an uncontended confirmation ([statuses](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/DetectionStatus.java#L20-L105)). | Per analysis invocation; no shared same-JVM campaign scheduler was found. |
| StrykerJS | A dry run is one awaited item in a newly constructed run-local pool; every public run builds and later disposes a root dependency graph ([dry run](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/process/3-dry-run-executor.ts#L80-L105), [run lifecycle](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/stryker.ts#L25-L53)). | A timeout restarts its runner and returns `Timeout`; rejected/crashed runners have a separate in-pool retry decorator ([timeout](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/timeout-decorator.ts#L20-L70), [retry](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/retry-rejected-decorator.ts#L17-L78)). | Per public run. Worker IDs and the documented `STRYKER_MUTATOR_WORKER` resource-partitioning convention are also run-local ([worker guidance](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/docs/parallel-workers.md)). |
| Stryker.NET | Mutant groups execute through a run's `Parallel.ForEachAsync` up to configured concurrency ([process](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core/MutationTest/MutationTestProcess.cs#L84-L100), [configuration](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Configuration/Options/Inputs/ConcurrencyInput.cs#L8-L53)). Initial runs for multiple source projects may themselves overlap. | If a multi-mutant batch times out or crashes without attribution, it reruns one mutant per test session. That group remains one task in the outer parallel loop, so other groups continue to compete ([executor](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core/MutationTest/MutationTestExecutor.cs#L35-L125)). | Per Stryker process/invocation. Its retry gives logical single-mutant attribution, not capacity isolation. |
| cargo-mutants | One baseline job completes before worker threads and build directories are created ([design](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/DESIGN.md#L278-L286), [lab](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/lab.rs#L51-L113)). A fresh GNU jobserver constrains that campaign and its Cargo descendants. | Timeout is terminal; no confirmation lane was found. | Per invocation. An output-directory lock serializes runs sharing the same artifact directory, but a different `--output` bypasses it; this is an artifact guard, not a capacity broker ([output lock](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/output.rs#L51-L138)). |
| Infection | The initial suite completes synchronously before mutation analysis; its parallel runner owns an instance-local queue and slots ([engine](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Engine.php#L105-L163), [runner](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Process/Runner/ParallelProcessRunner.php#L74-L150)). | Timeout is recorded directly. Follow-up analysis applies to escaped mutants, not timeout confirmation ([completion](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Process/Runner/ParallelProcessRunner.php#L174-L220)). | Per engine invocation. Its `TEST_TOKEN` convention asks tests to partition external resources per worker; it does not coordinate another campaign ([official guidance](https://github.com/infection/site/blob/e95e69f9a639a6da7981635cac8e26d27b414661/src/guide/how-to.md#L92-L121)). |
| Mull | Warm-up and baseline tasks are serial; mutation tasks then use a freshly built Rayon pool ([runner phases](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tools/mull-runner.rs#L91-L127), [pool](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tasks/src/lib.rs#L22-L87)). | Timeout kills and waits for that process, then records `Timedout`; no confirmation was found ([runner](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tools/runner.rs#L74-L100)). | Per runner invocation. |

The “per invocation” entries are source-level findings, not claims those projects explicitly advertise as guarantees. Each top-level path constructs its own executor, pool, or runner; no automatically shared same-process campaign scheduler was found. Absence of such a mechanism is not evidence that one is impossible—it means cross-invocation isolation is not part of those tools' visible contract.

These tools also expose worker-count controls (for example [StrykerJS](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/concurrent/concurrency-token-provider.ts#L23-L104), [cargo-mutants](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/main.rs#L271-L288), [Infection](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Configuration/ConfigurationFactory.php#L555-L579), and [Mull](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-core/src/config.rs#L105-L124)). StrykerJS and Infection additionally document ways for users to partition external resources by worker. That is reasonable for their contracts. It is not the experience Ooze is choosing: users should not have to make two concurrent `Release` calls agree on parallelism or resource tokens.

Most importantly, Stryker.NET shows why “retry one mutant alone” is ambiguous. It means alone _inside that test session_, while other mutation groups continue. Ooze's confirmation means no other attempt admitted by the same Ooze process is running. The latter is a stronger evidentiary claim and requires a shared broker barrier.

## Scheduler comparison: exclusivity is semantic and scope-bound

Test schedulers provide direct precedent for representing shared and exclusive work in the scheduler instead of asking tasks to race for ordinary locks.

| Scheduler | Semantic mechanism | Scope and lesson for Ooze |
| --- | --- | --- |
| Bazel | A test tagged `exclusive` runs with no other test at the same time ([Bazel Test Encyclopedia](https://github.com/bazelbuild/bazel/blob/5f76ff86cda0bd5b3e22053fd2ebe88eb0386f2e/docs/reference/test-encyclopedia.mdx#L746-L756)). | The Bazel invocation's scheduler owns the barrier. It is not a host-wide lock. |
| cargo-nextest | A test can reserve `num-test-threads`, making it mutually exclusive with every other test in that run. Nextest explicitly notes that this often reduces throughput but may improve reliability ([`threads-required`](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/site/src/docs/configuration/threads-required.md#L23-L39)). Named test groups are narrower logical semaphores and do not affect tests outside the group ([test groups](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/site/src/docs/configuration/test-groups.md#L7-L21)). | This is almost exactly Ooze's distinction between consuming the whole process broker for an exclusive attempt and sharing numbered capacity for primary attempts. Scope is one nextest run. |
| Go `testing` | `T.Parallel` pauses until all non-parallel tests finish, then runs only with other parallel tests ([Go source](https://github.com/golang/go/blob/b2b97b94965fc7eede9664da1e07117215232ef6/src/testing/testing.go#L1914-L1919)). | The barrier is scoped to one test binary; `go test` may still run other package binaries concurrently ([command documentation](https://github.com/golang/go/blob/b2b97b94965fc7eede9664da1e07117215232ef6/src/cmd/go/internal/test/test.go#L300-L311)). Process-local rather than machine-global exclusivity is therefore established precedent. |
| JUnit Jupiter | `@ResourceLock` gives named resources `READ`/`READ_WRITE` scheduling, and `@Isolated` runs a class without any other test concurrently ([JUnit parallel execution](https://github.com/junit-team/junit-framework/blob/9cd9a3cfb6cd98aec355bd49fc8d801058762441/documentation/modules/ROOT/pages/writing-tests/parallel-execution.adoc#L292-L333)). `@Isolated` itself is implemented as a lock on the global scheduler resource ([annotation](https://github.com/junit-team/junit-framework/blob/9cd9a3cfb6cd98aec355bd49fc8d801058762441/junit-jupiter-api/src/main/java/org/junit/jupiter/api/parallel/Isolated.java#L23-L41)). | This is a concrete reader/writer-style scheduler: shared readers coexist; a semantic writer forms a barrier. Scope is one Jupiter execution. |
| Gradle | `maxParallelForks` controls processes inside one `Test` task and warns users to isolate files/resources ([Java testing](https://github.com/gradle/gradle/blob/e4cbefb9908b0dba31b816eed486be3a3a11b103/platforms/documentation/docs/src/docs/userguide/reference/platforms/jvm/java_testing.adoc#L64-L79)). A shared build service, separately, can impose a build-wide maximum on all tasks that declare use of it ([build services](https://github.com/gradle/gradle/blob/e4cbefb9908b0dba31b816eed486be3a3a11b103/platforms/documentation/docs/src/docs/userguide/reference/task-development/build_services.adoc#L15-L28), [concurrency bound](https://github.com/gradle/gradle/blob/e4cbefb9908b0dba31b816eed486be3a3a11b103/platforms/documentation/docs/src/docs/userguide/reference/task-development/build_services.adoc#L165-L173)). | An inner pool cannot coordinate sibling owners. Broader coordination requires a common scheduler object and explicit scope—the role of Ooze's process broker. |

The common design is not machine-global locking. It is a semantic scheduler barrier over all work the scheduler owns. Ooze should make the same bounded claim: an exclusive baseline or confirmation excludes attempts admitted through this process-local broker. It does not exclude unrelated `go test` package processes, another terminal's Ooze process, test-internal goroutines, or arbitrary host load.

## Why Ooze deliberately provides the stronger guarantee

Peer mutation tools establish that phase-ordering a baseline before a run-local pool is conventional. Ooze gives the baseline the stronger process-local exclusive boundary so Ooze-owned peer work cannot manufacture a false baseline failure. The baseline gates mutant admission; it is not a capacity probe and does not drive a ramp.

Confirmation has an even stronger reason. Once a primary's accepted execution-domain obligation
coexists with another Ooze-owned attempt obligation, the runtime latches recorded peer overlap for
that primary. The fact remains true if the peer drains before the primary's deadline. Such a deadline
cannot yet be attributed safely. An exclusive confirmation removes every competing attempt that Ooze
itself admitted in this process. A run-local retry like Stryker.NET's cannot support that statement
when other groups remain active. A primary deadline without recorded peer overlap is already
attributable after its execution domain drains and does not enter the confirmation lane. Process-fuse
observations remain outside this resolved timeout rule until their later normalization decision.

The performance cost is narrow and intentional:

- automatic primary mutant attempts remain shared across campaigns, while `Serial()` attempts are
  exclusive;
- a non-empty campaign pays one exclusive baseline;
- only primary deadlines with recorded peer overlap pay an exclusive confirmation;
- an exclusive request stops _new_ admissions and lets active leases drain; it does not preempt or retry healthy work; and
- one broker decision is tiny compared with repository materialization and child execution.

Concurrent campaigns may therefore delay one another at evidence boundaries, but users do not coordinate worker counts and healthy primary throughput remains pooled. This is the same reliability-for-overlap trade that nextest documents for a globally exclusive test, scoped only to the work Ooze can honestly control.

## Why the broker needs an explicit state-machine seam

FoundationDB runs production database logic in a deterministic, single-threaded discrete-event simulator. It abstracts network, disk, time, and randomness, and avoids multithreaded concurrency in the simulated core ([FoundationDB paper, section 4](https://www.foundationdb.org/files/fdb-paper.pdf)). Its client-testing documentation is unusually direct about the boundary: deterministic seeded replay works while work remains on the simulated actor thread, while adding operating-system threads introduces nondeterminism and requires separate testing ([FoundationDB client testing](https://apple.github.io/foundationdb/client-testing.html#parallelism-and-determinism)).

TigerBeetle applies the same pattern. VOPR runs a cluster on one thread, controls time and injected faults, and reproduces failures from a seed ([TigerBeetle architecture](https://github.com/tigerbeetle/tigerbeetle/blob/main/docs/ARCHITECTURE.md#simulation-testing)). TigerBeetle also keeps time out of deterministic state-machine logic and injects an ordered timestamp instead ([TigerBeetle architecture: Time](https://github.com/tigerbeetle/tigerbeetle/blob/main/docs/ARCHITECTURE.md#time)).

Ooze does not need either project's runtime. It needs the architectural consequence:

```text
(RuntimeState, BrokerState) + NormalizedEvent
    -> (RuntimeState, BrokerState) + ordered Effects
       or invariant diagnostic
```

That transition must perform no I/O, read no wall clock, start no goroutine, block on no channel, and consult no package global. A thin production shell may serialize calls with a mailbox or mutex. A simulation shell invokes the same transition directly. The expensive work—materializing repositories and running child processes—remains parallel; only the small control-plane decisions are serialized.

This also keeps Ooze's intended deterministic runner implementations possible. A full simulation supplies attempt completions, trips, drain observations, and time advances as events rather than starting real processes. Native process supervision remains a separate integration-test surface.

## Concurrent calls need a linearization order, not a universal arrival order

A concurrent broker is naturally specified as a linearizable object: each operation appears to take effect at one point between invocation and response, and non-overlapping operations preserve their real-time order ([Herlihy and Wing, _Linearizability_](https://cs.brown.edu/people/mph/HerlihyW90/p463-herlihy.pdf)). Two overlapping `Release` calls have no pre-existing total order. Either linearization is legitimate.

For Ooze, the linearization point should be the instant the shared process coordination boundary
accepts a command and assigns its trace sequence. Consequences:

- If campaign A's registration completes before campaign B is invoked, A is earlier.
- If A and B overlap, the production runtime may accept either one first.
- Once accepted, the assigned order is data. Later scheduling must not rediscover an order from goroutine wake-up timing.
- A simulation may choose either ordering deliberately. A seeded scheduler can reproduce that choice; a bounded exhaustive scheduler may explore both later.
- Replaying an observed production incident requires its recorded ingress order as well as its external observations. A seed alone cannot reconstruct unsimulated operating-system goroutine arrival.

This is deterministic replay, even though production concurrency is not physically repeatable. The same trace always replays the same way, and simulations can cover alternative valid traces.

## Ownership of arbitration and fairness

Arbitration belongs entirely to the broker state machine. It must not be an emergent property of:

- which `Release` goroutine acquires a mutex next;
- which blocked goroutine an `RWMutex` wakes;
- a `select` across separate request channels;
- wall-clock request timestamps; or
- Go map iteration.

Go documents that a waiting `RWMutex.Lock` excludes new readers, but it does not give Ooze a FIFO admission contract ([`sync.RWMutex`](https://pkg.go.dev/sync#RWMutex)). A Go `select` chooses pseudo-randomly when several communications can proceed ([Go specification: select statements](https://go.dev/ref/spec#Select_statements)). Those primitives can implement transport and synchronization, but their unspecified or randomized choices must not become campaign policy.

The minimum starvation-prevention rule is a deterministic shared/exclusive barrier queue:

1. Give each accepted admission request a monotonically increasing ticket.
2. Shared requests may be granted in ticket order while capacity remains, but never overtake an earlier exclusive request.
3. Once an exclusive request is the queue barrier, admit no later shared request.
4. Let active shared leases drain, then grant that exclusive lease alone.
5. On exclusive release, continue from the next ticket.

This is starvation freedom, not equal-share scheduling: a request eventually reaches the front if capacity remains nonzero and every earlier active lease eventually terminates. Supervision, rather than queue fairness, handles an attempt that does not terminate.

This policy does not require weighted fair queuing, campaign priorities, aging, or user-visible
coordination knobs. FIFO exclusive-barrier semantics are part of the resolved campaign model; replacing
them with per-campaign round-robin or another policy requires revisiting that algebra rather than an
implementation-only substitution.

The campaign engine should request admissions lazily for its current execution window; it should not enqueue the entire mutant catalogue. An automatic campaign has at most `P` shared requests or leases outstanding, while a `Serial()` campaign has at most one primary request or lease. That keeps FIFO latency proportional to active demand rather than catalogue size, without adding a second fairness algorithm.

## Minimal state, inputs, and effects

The broker needs only enough identity to correlate events and make order explicit. The composing
process-runtime core retains the cross-module state required to make a broker grant safe to use.

### State

```text
broker:
  detected capacity P and automatic admission state FullAutomatic(P) | SingleAdmissionAutomatic
  ordered pending requests
  active shared leases
  optional active exclusive lease
  installed, sealed, and bound confirmation barriers
  next ticket and lease IDs

process runtime:
  Open | FatalClosing(epoch) | ClosedDrained(epoch) | ClosedUnconfirmed(epoch, nonempty residual)
  fatal epoch's stable ingress-ordered causes and elected reporting owner
  registered campaigns and open/closed primary gates
  pending-start, prospective-domain, and owned-domain obligations plus recorded peer-overlap facts
  terminal-candidate and accepted-commit correlations
  next campaign, effect, event, and fatal-epoch IDs
```

Ordered state must use tickets or another stable order; decisions must never depend on map iteration.

### Stable identities

- **Campaign ID:** assigned when the process runtime accepts registration.
- **Attempt ID:** stable within a campaign and identifies baseline, catalogue mutant plus attempt ordinal, or confirmation.
- **Request ID/ticket:** identifies one admission request and its position in arbitration.
- **Lease ID:** identifies the permission that must be returned exactly once.
- **Event sequence:** identifies the production linearization or simulated event order for diagnostics and replay.
A durable broker-replacement epoch, globally unique IDs, durable sequence log, and cross-process
identity are unnecessary unless a later design actually permits messages to survive broker
replacement. The required in-memory process-runtime fatal epoch is different: it orders emergency
causes, terminal commits, and permanent closure inside one process. Separate processes remain separate
coordination domains.

### Inputs

At minimum:

```text
RegisterCampaign
RequestAdmission(campaign, attempt-generation, Shared | Exclusive)
ClosePrimaryGateAndInstallConfirmationBarrier(campaign, wave)
SealAndBindConfirmationBarrier(campaign, wave, first-mutant)
CancelAdmission(campaign, request)
ReturnLease(campaign, lease)
RequestStartCommit(campaign, attempt-generation, lease, phase-authorization)
ProposeTerminalCandidate(campaign, outcome)
CloseRuntime(cause)
SettleRuntimeEmergency(epoch, obligation, observation)
EnterSingleAdmissionAutomatic(reason)
```

Under the agreed campaign contract, an automatic primary mutant attempt requests `Shared`; a
`Serial()` primary, baseline, or confirmation requests `Exclusive`. Every start commitment must match
the granted lease for that attempt generation, the relevant primary gate or sole phase slot, and an
open runtime. Automatic mode preserves `GOMAXPROCS=1` for baseline, primary, and confirmation children;
`Serial()` preserves full `P` for all three classes. Whenever accepted live execution-domain
obligations coexist, the shared coordination boundary latches recorded peer overlap for each affected
primary. At a primary deadline, it consults that retained fact rather than the active set at the trip
instant. With recorded peer overlap, it atomically closes that campaign's primary-start gate and
installs an attempt-independent confirmation barrier in the FIFO before the provisional trip reaches
the campaign reducer. Start commitment or
closure wins at that one linearization point; installation cannot lag in an asynchronous effect
queue. The barrier is not grantable until the pre-closure committed set and associated resources
settle, the stable queue is sealed, and the barrier binds to its catalogue-first confirmation. With
no recorded peer overlap, no confirmation barrier is installed: authoritative drainage permits
direct timeout classification.

Automatic admission starts in `FullAutomatic(P)`. A trustworthy hard resource-exhaustion observation
from shared automatic execution, or a deadline with recorded peer overlap whose exclusive
confirmation terminates ordinarily, emits the idempotent `EnterSingleAdmissionAutomatic` transition.
Existing leases drain; future automatic grants cannot overlap. The transition does not classify the
affected mutant, infer a new numeric capacity, or alter the automatic `GOMAXPROCS=1` execution
profile. Hard exhaustion aborts the affected campaign unscored and is not retried. A repeated
exclusive deadline supports an intrinsic timeout and leaves admission unchanged.

Cancellation is explicit and correlated. A cancellation acknowledgement settles an ungranted request;
a known late grant is returned. Neither cancellation nor a terminal proposal may erase an active
lease, and waiting remains the absence of a grant rather than a hidden outcome.

Baseline observations, attempt completion, overlap facts, provisional trips, confirmation results,
execution-domain drainage, normalized hard-exhaustion observations, and deadline expiry belong to
the campaign engine and runner model. They become explicit driver events when they can cause a later
broker or campaign transition. The broker should not inspect test results.

### Effects

At minimum:

```text
CampaignRegistered(campaign)
AdmissionGranted(request, lease)
AdmissionCancelled(request)
ConfirmationBarrierInstalled(campaign, wave)
ConfirmationBarrierBound(campaign, wave, first-mutant)
SingleAdmissionAutomaticEntered(reason)
StartCommitAccepted(campaign, attempt-generation, lease)
StartCommitRejected(campaign, attempt-generation)
TerminalCommitAccepted(campaign)
TerminalCommitRejected(campaign, fatal-epoch)
RuntimeEmergency(fatal-epoch)
ForcedAbortAuthorized(campaign, fatal-epoch)
FatalAuthorized(campaign, fatal-epoch, nonempty-residual)
```

These names are conceptual; concrete call shapes remain downstream. Waiting is represented by the
absence of `AdmissionGranted`; it is not rejection, timeout, pressure, or mutation evidence. Invalid
correlations—unknown campaign, duplicate lease return, exclusive grant while any lease is active—are
impossible normalized states. They produce a typed `InvariantViolation`, enter the runtime-wide
emergency path, and re-panic only after settlement; deterministic negative-contract tests assert that
diagnostic rather than modeling an immediate reducer panic. `FatalAuthorized` is available only to a
containment-only epoch with a non-empty residual. If an invariant joins, its diagnostic dominates and
attaches every other cause and any residual instead of emitting that fatal authorization.

## Ordering and time rules

The pure process-runtime/broker core processes one event at a time and emits an ordered effect list.
The driver owns all nondeterministic choices outside that core.

For simulation:

- represent time as logical-time events;
- order scheduled events by logical time plus an explicit tie-break choice;
- stable-sort every generated option before a seeded chooser selects one;
- record the chosen event IDs in the failure trace; and
- offer a fixed-order chooser first, a seeded chooser for fuzzing next, and bounded systematic exploration only if evidence justifies it.

For production:

- serialize process-runtime and broker commands through one coordination boundary;
- assign an event sequence there;
- preserve per-caller program order;
- use monotonic time only to create explicit deadline/observation events, never as a fairness tie-break; and
- include accepted ordering and relevant external observations in failure diagnostics.

Real time may cause a real deadline event. It must not decide whether a waiting admission is itself a failure. Likewise, goroutine arrival may determine which of two overlapping commands is linearized first, but after that point it has no further semantic authority.

## What deterministic testing tools do and do not provide

Go's `testing/synctest` supplies scoped fake time and a way to wait until goroutines in a bubble are durably blocked. Its own contract advises avoiding external processes and real network I/O, and mutex acquisition is not considered durably blocking ([`testing/synctest`](https://pkg.go.dev/testing/synctest)). It is useful for tests of the production runtime/broker shell, deadlines, and goroutine cleanup. It is not an Ooze scheduler, does not choose every goroutine interleaving for replay, and cannot simulate native child-process supervision.

Loom demonstrates the complementary technique: replace concurrency primitives and repeatedly explore valid thread executions. Its documentation warns that uninstrumented primitives are invisible and that models with many threads still suffer combinatorial explosion, often requiring a pre-emption bound ([Loom documentation](https://docs.rs/loom/latest/loom/)). Ooze should borrow the lesson, not recreate Loom: keep the shared runtime/broker core sequential and pure, then use race tests, stress tests, and focused adapter tests around its thin concurrent shell.

The resulting testing layers are:

1. table/property tests over the pure broker transition;
2. seeded campaign-plus-runtime/broker simulation using deterministic runner, materializer, clock, and reporter implementations;
3. bounded permutations of only the small number of meaningful simultaneous broker events;
4. race/stress and `synctest` tests for the production shell; and
5. platform integration tests for real child processes and execution domains.

FoundationDB explicitly uses separate end-to-end multithreaded tests for behavior outside its single-threaded simulator ([FoundationDB API tester](https://apple.github.io/foundationdb/client-testing.html#api-tester)). TigerBeetle similarly describes VOPR as exercising a controlled single-threaded environment while using real-infrastructure tests to cover OS and client interactions ([TigerBeetle, Vortex](https://tigerbeetle.com/newsletters/2024-12-06-november-in-tigerland/)). Ooze should keep the same honest boundary.

## Does one process-local broker undermine isolation?

Only if it is hard-wired as invisible global state.

Production needs one shared instance per Go process so concurrent `Release` calls cannot each assume ownership of all available capacity. The pure design should still be an ordinary constructible value. The production entry point obtains the process instance from a small runtime owner; tests and simulations construct one isolated runtime/broker world and never touch that owner.

```text
production Release -> shared runtime/broker shell -> pure runtime/broker state

simulation world   -> deterministic driver -----------> pure runtime/broker state
```

This preserves the production coordination contract while making reset, isolation, snapshotting, invariant checking, and replay straightforward. A simulation may also instantiate several process worlds to model independent brokers if cross-process resource pressure later matters; under the current contract, that pressure is an external observation rather than cross-process admission coordination.

## Invariants worth asserting from the first slice

1. A new shared grant never makes active shared admission exceed `P` in full automatic mode or one in
   single-admission automatic. The one-way transition may leave temporary overcommit only while
   existing leases drain.
2. If an exclusive lease exists, it is the only active lease; if any shared lease exists, no exclusive lease exists.
3. Every granted lease belongs to exactly one registered campaign, one request, and one attempt
   generation.
4. Every lease is returned at most once; a finished campaign owns no active lease.
5. A later shared ticket never overtakes an earlier exclusive ticket.
6. A closed campaign receives no new grant, and a non-open runtime accepts no registration, grant,
   start commitment, or normal terminal commit.
7. Entering single-admission automatic never revokes an existing lease; it only constrains future grants.
8. Contention alone cannot produce a campaign result or mutation evidence.
9. A start commitment is accepted only against the matching lease, generation, gate, and phase slot;
   acceptance creates the prospective execution-domain obligation before launch.
10. The same state and ordered event trace produce the same canonical logical state, effects, terminal
    decisions, and invariant diagnostics.

These assertions belong in the pure state-machine tests (and can use the proposed `internal/assert` package at runtime boundaries). They are more valuable than asserting a particular wall-clock completion order.

## Overengineering risks

- **Do not build a general deterministic goroutine runtime.** The campaign and process-runtime/broker cores need an explicit event driver; Go scheduling and native processes remain integration surfaces.
- **Do not require production runs with concurrent callers to derive the same schedule from a seed.** Record their accepted event order; use seeds inside a fully simulated world.
- **Do not expose campaign IDs, fairness policy, process capacity, or broker coordination knobs to users merely to make tests easier.** Keep them internal and injectable at the driver seam.
- **Do not add weighted fairness, priorities, aging, work stealing, or a distributed broker without measured need.** Ticket order plus an exclusive barrier is enough for the current contract.
- **Do not persist an event-sourced broker or implement duplicate-delivery consensus.** This is an in-process object. IDs and an in-memory trace are sufficient until a concrete recovery requirement appears.
- **Do not enumerate all goroutine interleavings.** Enumerate meaningful domain events; Loom's state-space warnings apply directly.
- **Do not run real child processes inside the deterministic simulator.** Supply a deterministic runner and verify native supervision separately.
- **Do not make a package global the only way to reach broker state.** That would couple tests, hide lifecycle, and turn process-local exclusivity into nondeterministic test pollution.
- **Do not add a generic capacity-policy plug-in, `OOZE_*` override, focused mutant selector, or
  timeout-confirmation switch to this runner slice.** Future configuration and catalogue selection
  can resolve into campaign input before execution; they do not justify speculative broker states or
  public seams now.

## Explicitly deferred questions

1. The automatic process-fuse observation and its normalization remain owned by the later fuse
   decision; #58 does not treat an unspecified fuse trip as capacity pressure or mutation evidence.
2. Whether production failure diagnostics persist a complete replay trace or only a bounded tail can wait until the simulation format exists.
3. Whether bounded exhaustive exploration adds enough value beyond seeded traces should be measured on the pure reducer before adopting a model-checking framework.
4. Equal throughput between simultaneous campaigns is not currently a requirement. The required fairness property is no starvation behind later arrivals.

## Recommended decision text

> Ooze coordinates concurrent campaigns through one process-local admission broker whose arbitration
> is a deterministic state machine. Broker commands are linearized into an explicit ordered trace;
> contention waits and cannot itself become a campaign failure. Shared admissions follow stable ticket
> order, and an earlier exclusive admission forms a barrier that later shared work cannot overtake.
> Automatic admission starts at aggregate detected capacity `P` and has one irreversible fallback to
> single-admission automatic after trustworthy capacity pressure; it has no intermediate numeric
> controller. Production goroutine arrival may choose the linearization of overlapping calls, but real
> time and runtime wake order do not otherwise decide policy. Simulations construct isolated broker
> state and choose or replay the same explicit events, while the production entry point alone owns the
> shared process instance.
