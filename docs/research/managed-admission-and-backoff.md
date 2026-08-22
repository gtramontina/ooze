# Managed admission and monotone backoff

_Research date: 2026-08-21. This is a design input for [Set automatic admission and pressure
fallback](https://github.com/gtramontina/ooze/issues/58), not an implementation specification._

## Decision status

> **The ramp-and-midpoint proposal in this report is superseded.** Issue
> [#58](https://github.com/gtramontina/ooze/issues/58) resolved a smaller two-state policy. Automatic
> admission starts immediately at aggregate detected capacity `P`. After trustworthy hard resource
> exhaustion from shared automatic execution, or after a primary deadline with recorded peer overlap
> disappears in exclusive confirmation, the process runtime irreversibly enters single-admission
> automatic. There is no healthy ramp, midpoint/halving arithmetic, recovery controller, confirmation
> toggle, or user-visible parallelism setting.

Only a primary deadline with recorded peer overlap enters exclusive confirmation. A deadline without
recorded peer overlap becomes directly attributable after authoritative drainage. A repeated
exclusive deadline is intrinsic and does not change admission. [Choose the automatic runaway fuse](https://github.com/gtramontina/ooze/issues/60) has since resolved the fuse: a fixed 64-descendant ceiling, counted by parent identity, guarding automatic attempts only. A trip is directly attributable killed-class `Runaway` after drainage, never confirmed and never capacity pressure, because the count is contention-independent under the `GOMAXPROCS=1` automatic profile. `Serial()` attempts carry no fuse.

The source and experiment findings below remain valid design evidence. Sections describing a
`min(P, 2)` bootstrap, additive ramp, pressure epochs, or `ceil(A/2)` preserve a rejected proposal
for auditability; they are not the implementation contract. Future `OOZE_*` overrides and focused
mutant/source/test selection are explicitly outside this runner ticket.

## Capacity: use the Go runtime's answer, once

Since Go 1.25, the runtime chooses its default `GOMAXPROCS` from the logical CPU count, process CPU
affinity, and, on Linux, the cgroup CPU-throughput quota. It periodically notices allocation
changes unless an environment value or programmatic override disables that behavior
([runtime documentation](https://pkg.go.dev/runtime#GOMAXPROCS),
[Go 1.25 release notes](https://go.dev/doc/go1.25#container-aware-gomaxprocs)). Ooze already targets
Go 1.25 or newer, so this is preferable to maintaining separate cgroup-v1, cgroup-v2, affinity,
macOS, and Windows detection code.

`runtime.NumCPU()` is not equivalent. It reports logical CPUs usable at process startup and does
not reflect later allocation changes; more importantly, it does not incorporate Linux cgroup CPU
throughput limits ([`runtime.NumCPU`](https://pkg.go.dev/runtime#NumCPU)). Reading
`runtime.GOMAXPROCS(0)` has no mutation side effect, while calling `runtime.GOMAXPROCS(n)` would
change the host application's global runtime and disable automatic updates. Calling
`runtime.SetDefaultGOMAXPROCS()` would also be wrong: it explicitly ignores the embedding
application's `GOMAXPROCS` environment choice ([runtime documentation](https://pkg.go.dev/runtime#SetDefaultGOMAXPROCS)).

The captured value should be an injected production observation in the deterministic model:

```text
DetectedCapacity(P > 0)
```

Tests and simulations provide `P` directly. Production obtains it once. The resolved contract
intentionally does not chase later host changes; its only response to trustworthy pressure is the
one-way transition to single-admission automatic.

### Known rounding boundary

The runtime normally takes the minimum of logical CPUs, affinity, and Linux cgroup quota, but it
rounds fractional quota up and does not choose less than two unless logical CPU or affinity count is
already below two ([implementation details](https://pkg.go.dev/runtime#GOMAXPROCS)). A container
with a one-CPU quota can therefore yield `P=2`. Go explains that cgroup quota is a throughput limit,
whereas `GOMAXPROCS` is an integer parallelism limit, so they cannot be represented identically
([container-aware `GOMAXPROCS`](https://go.dev/blog/container-aware-gomaxprocs#slightly-different-models)).

That mismatch does not justify an OS-specific capacity subsystem in this change. An exact Linux
quota reader would still leave CPU requests, Windows job CPU-rate controls, unrelated host load,
memory, I/O, and arbitrary descendant behavior unresolved. It would also duplicate an evolving Go
runtime implementation. Cooperative child shaping plus the one-way automatic fallback provide the
portable response without claiming exact ownership.

### Why `GOMAXPROCS=1` matters to Go test commands

For a `go test` child, `GOMAXPROCS=1` constrains more than application goroutines. The `go` command's
`-p` build/test-binary parallelism defaults to `GOMAXPROCS`, and a test binary's `-parallel` default
also comes from `GOMAXPROCS` ([`cmd/go` documentation](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies),
[test-flag documentation](https://pkg.go.dev/cmd/go#hdr-Testing_flags)). This makes the resolved
one-CPU automatic execution profile directly effective against the nested Go fan-out that a mere
outer worker count misses. A configured non-Go command may ignore the environment value; that is
why Ooze's outer admission boundary remains the only portable control it owns. The separately
resolved fuse adds supervision but no capacity meaning: #58 assigns a fuse trip none, and
[#60](https://github.com/gtramontina/ooze/issues/60) confirms a trip is never capacity pressure.

## Mutation tools: useful warnings, no adaptive answer

The inspected tools choose static limits rather than learning a safe limit during one mutation
campaign:

| Tool | Primary-source behavior | Lesson for Ooze |
| --- | --- | --- |
| StrykerJS | Defaults to all logical CPUs at four or fewer, otherwise `n-1`, and exposes number/percentage overrides ([configuration](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/docs/configuration.md#L129-L137)). | A CPU-count heuristic is convenient, but it neither controls nested commands nor responds to pressure. |
| Stryker.NET | Defaults to half the logical processors and exposes a numeric concurrency option ([configuration](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/docs/configuration.md#L317-L323)). | “Half” is a safety constant, not an observation-based policy. |
| PIT | Defaults to one thread; automatic core-count threads are an opt-in feature ([Maven documentation](https://github.com/hcoles/pitest-site/blob/614bec6fd16670852e94c8cc03a002f6bf05db09/quickstart/maven.markdown#L131-L139), [thread default](https://github.com/hcoles/pitest-site/blob/614bec6fd16670852e94c8cc03a002f6bf05db09/quickstart/maven.markdown#L202-L204)). | Serial-by-default is safe but abandons Ooze's automatic-throughput goal. |
| cargo-mutants | Warns that nested build/test processes can thrash or exhaust memory, recommends manually starting at two or three jobs, and says core-proportional jobs often work poorly ([parallelism guide](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/parallelism.md#L3-L39)). It starts a one-token-per-CPU GNU jobserver, but Rust tests do not participate in it ([jobserver guide](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/jobserver.md#L1-L12)). | Nested cooperation is powerful when descendants honor it. Ooze cannot require arbitrary configured commands to speak a jobserver protocol. |

These are directly applicable cautions, not candidate algorithms. In particular, cargo-mutants
confirms the failure mode Ooze observed: multiplying an outer worker count by each build/test
command's internal parallelism can exhaust processes and memory even when the outer count resembles
the CPU count.

## Build schedulers: share leases, do not infer arbitrary child cost

GNU make's jobserver caps active jobs across cooperating recursive tools through explicit tokens,
and requires every participant to return exactly the tokens it acquired even on error
([GNU make job slots](https://www.gnu.org/software/make/manual/html_node/Job-Slots.html)). Gradle
similarly has one build-wide worker pool, defaulting to the processors visible to the JVM, so
parallel projects do not multiply that pool
([Gradle native-build documentation](https://docs.gradle.org/current/userguide/native_software.html#sec:parallel_native_compilation)).
These are strong precedent for Ooze's aggregate process-local leases.

Bazel can account for host CPU and RAM and action-declared resource needs, while its CPU-load
scheduler remains explicitly experimental
([Bazel command reference](https://bazel.build/reference/command-line-reference#flag--local_resources)).
Ooze has no resource declaration for an arbitrary `WithTestCommand`, and adding one would return the
tuning burden to users. Baseline RSS, process count, or CPU time also cannot reliably predict a
mutant that changes control flow. The broker should therefore count owned attempts, constrain Go
child parallelism, and react to strong execution observations rather than pretend every resource
has a portable cost model.

## Adaptive controllers: why the analogy was rejected

TCP begins with a small congestion window because path capacity is unknown, grows only as sent data
is acknowledged, and reduces the window after congestion. Crucially, RFC 5681 warns implementations
to base a reduction on actual `FlightSize`, not a configured window that may be larger than the work
in flight ([RFC 5681 section 3.1](https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1)). That
motivated the rejected proposal to capture shared `StartCommitted` occupancy `A`; the resolved
categorical transition does not need flight arithmetic.

Netflix's loss-based AIMD implementation supplied another shape considered here: increase the limit by one
only when there is enough in-flight demand, and multiply it down on a drop or timeout
([`AIMDLimit`](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/AIMDLimit.java#L25-L38),
[update rule](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/AIMDLimit.java#L101-L112)).
Ooze does not copy its defaults, timeout, ramp, or continuing oscillation. Mutation attempts are finite
and heterogeneous, and an Ooze trip may be the intended effect of a mutant rather than shared
congestion.

Latency-gradient controllers are less applicable. Envoy samples request latencies in wall-clock
windows, periodically restricts concurrency to remeasure minimum RTT, and supports random jitter;
its example collects fifty requests for a recalibration
([Envoy adaptive-concurrency documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/adaptive_concurrency_filter.html)).
Netflix's Vegas and Gradient2 controllers likewise depend on comparable RTT samples and moving
averages ([algorithm summary](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/README.md#limit-algorithms)). A mutation catalogue mixes
compile failures, immediately killed mutants, full-suite survivors, and deliberately pathological
control flow. Their durations are not samples from one stationary service distribution.

Adding windows, percentiles, jitter, a periodic low-concurrency phase, or a fresh unmutated probe
would therefore make replay harder and spend commands without resolving attribution. FoundationDB's
simulation guidance is the relevant precedent: deterministic seeded replay holds while work stays
on the deterministic event thread, whereas ordinary OS threads introduce nondeterministic behavior
([FoundationDB client testing](https://apple.github.io/foundationdb/client-testing.html#parallelism-and-determinism)).
Ooze's pure policy should consume explicit events and leave real time in the driver.

## Which observations may change admission

The following classification is an inference from Ooze's resolved confirmation model and the
platform sources above. It is not a claim that another mutation tool implements it.

| Observation | Admission consequence | Reason |
| --- | --- | --- |
| Shared primary completes normally and drains | No admission change | Pass versus normal test failure is mutation evidence, not capacity evidence; full automatic admission already starts at `P`. |
| Primary deadline with recorded peer overlap; exclusive confirmation terminates ordinarily | Enter single-admission automatic | The same attempt profile ceased tripping after Ooze-owned competitors drained. The transition removes all future Ooze-owned peer overlap without pretending to infer a numeric safe capacity. |
| Primary deadline with recorded peer overlap; exclusive confirmation repeats the deadline | No admission change | The authoritative isolated observation supports `TimedOut`; shared concurrency was not the differentiator. |
| Primary deadline without recorded peer overlap | No confirmation and no admission change | After authoritative drainage the observation is already attributable; no other Ooze-owned obligation coexisted during the attempt. |
| Ordinary test failure | No admission reduction | A failed assertion kills a mutant; it is not resource pressure. |
| Exclusive baseline or confirmation deadline | No admission change | No Ooze-owned peer overlapped it, so shared admission is not the differentiator. |
| Trustworthy hard resource exhaustion from shared automatic execution | Enter single-admission automatic; abort the affected campaign without retry | POSIX `EAGAIN` can mean a process/thread limit, while `ENOMEM` means insufficient creation resources; no child exists after a failed `fork` ([Linux `fork(2)`](https://man7.org/linux/man-pages/man2/fork.2.html)). The platform adapter must distinguish these from command-not-found or permission errors. |
| Process-fuse observation | Resolved | [Choose the automatic runaway fuse](https://github.com/gtramontina/ooze/issues/60) defines it as an automatic-profile descendant count against a fixed ceiling, directly attributable and never capacity pressure. |
| Elapsed-duration increase alone | No admission reduction | Mutants are heterogeneous, external load is uncontrolled, and a duration threshold would duplicate the deadline decision. |
| Host load average or Linux PSI | Diagnostics only | PSI accurately quantifies CPU, memory, and I/O stalls and can be scoped to a cgroup, but it is Linux-specific and system PSI includes unrelated workloads ([Linux PSI documentation](https://docs.kernel.org/accounting/psi.html)). |

Uncertain observation still aborts or follows the fatal-containment path; it must not silently tune
admission and continue scoring.

## Superseded transition arithmetic

> The remainder of this section records the rejected ramp-and-midpoint controller. It is retained to
> show which alternatives were evaluated; none of this arithmetic belongs to the resolved runner.

The rejected proposal would have added four values and an epoch candidate set to the broker's
existing aggregate active obligations and pending demand:

```text
maximum             P >= 1             // immutable production observation
limit               1 <= L <= ceiling  // current shared grant limit
ceiling             1 <= C <= P         // monotone non-increasing
pressure_epoch      E                   // increments after accepted pressure
pending_pressure    candidates for E    // freezes upward ramp while non-empty
```

Initial state:

```text
P = DetectedCapacity
C = P
L = min(P, 2)
E = 0
```

This two-way bootstrap is not an unexplained probe. Every non-empty automatic campaign must first
pass its full unmutated command exclusively with the same `GOMAXPROCS=1` profile, and a campaign
whose baseline does not pass cannot request a primary. That mandatory result supplies the singleton
slow-start observation without adding work. Later campaign baselines do not add further ramp credit,
and a `Serial()` baseline has a different full-`P` profile and never contributes.

A pressure-free shared completion may perform:

```text
if pending_pressure is empty,
and shared demand was eligible and saturated L,
and L < C:
    L = L + 1
```

“Saturated” must be expressed in broker state--eligible pending demand plus the active/granted shared
ledger--rather than inferred from elapsed time. Exclusive completions do not earn ramp credit.

The original shared trip records:

```text
PressureCandidate(epoch=E, flight=A, attempt=...)
```

where `A` is the aggregate count of shared `StartCommitted` execution-domain obligations at that
linearization point, including prospective launches. If exclusive confirmation later validates
contention-correlated pressure and the candidate epoch still equals `E`:

```text
C = min(C, max(1, ceil(A / 2)))
L = min(L, C)
E = E + 1
```

Use integer arithmetic `(A + 1) / 2`. Rounding up retains at least half of established flight when
`A` is odd; a later genuinely pressured epoch can reduce again. Every candidate carrying the old
epoch remains useful diagnostic evidence but cannot change capacity again. This is the normalization
that prevents a burst of five simultaneous trips from producing five multiplicative reductions.

Freezing only upward ramp at the first candidate is necessary because FIFO may contain shared
requests accepted before the exclusive barrier. They retain their existing entitlement, but clean
attempts draining after the warning must not raise `L` and admit still more pre-barrier work. The
freeze neither revokes a lease nor prejudges the mutant. If confirmations repeat every candidate's
trip, remove those candidates and resume ramping from the unchanged `L` on later qualifying
completions. Do not accumulate or replay suppressed completions as credit: a catch-up jump would
reintroduce the burst the freeze prevents.

A lower `L` never revokes active leases. The broker simply grants nothing new until the active
ledger falls within the new bound. There is no upward recovery past `C` during the lifetime of this
process runtime. A new process begins a new calibration.

## Performance consequences of the rejected controller

The rejected controller added no test command, but it withheld healthy admission while growing
cohorts approximately `2, 4, 8, ...`. That imposed a finite throughput cost on every campaign,
especially a focused or otherwise small catalogue on a many-core host.

The research did not establish that launching `P` automatic roots, each cooperatively shaped with
`GOMAXPROCS=1`, is itself unsafe. The ramp also could not prove an opaque later command safe. Issue
#58 therefore chose full admission immediately and pays reduced throughput only after trustworthy
pressure. Cross-platform acceptance should measure makespan and peak process count for `P=1,2,4,8`
and larger synthetic values without introducing probes, worker knobs, or latency sampling.

## Resolved frontier

Issue #58 accepts `runtime.GOMAXPROCS(0)` as the injected, process-respecting maximum and rejects an
OS-specific capacity detector. It also pins the two trustworthy triggers above while leaving exact
platform error normalization to the supervisor work and process-fuse meaning to #60. Unknown errors
never become feedback.

The resolved policy uses no capacity arithmetic: `FullAutomatic(P)` has one idempotent transition to
`SingleAdmissionAutomatic`. This categorical state is sufficient for deterministic simulation and
does not require a capacity-policy interface, pressure generations, probes, timers, recovery, or a
user setting.
