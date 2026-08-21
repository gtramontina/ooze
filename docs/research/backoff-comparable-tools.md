# What comparable tools do after execution pressure

_Research date: 2026-08-21. This is field research for
[Set automatic admission and pressure fallback](https://github.com/gtramontina/ooze/issues/58), not an
implementation specification._

> **Decision status:** Issue #58 accepted the field warning against a speculative numeric controller.
> Automatic admission starts at aggregate detected capacity `P` and has one irreversible fallback to
> single-admission automatic after trustworthy hard shared exhaustion or a deadline with recorded peer
> overlap that disappears in exclusive confirmation. No ramp, half-step, recovery loop, or
> confirmation toggle was adopted. A deadline without recorded peer overlap is classified directly
> after authoritative drainage; process-fuse normalization and future `OOZE_*`/selection work remain
> outside this decision.

## Executive answer

No mutation runner, test runner, build scheduler, or CI/batch controller inspected here reduces its
active concurrency by one half after a resource-exhaustion failure and then preserves that reduced
ceiling for the remainder of a finite run.

The established tools use three other patterns:

1. **Prevent overload with a fixed or declared pool.** GNU make, Ninja, Bazel, Gradle, Go, nextest,
   PIT, Stryker, and cargo-mutants determine a concurrency limit before work starts. The stronger
   systems share tokens with cooperating descendants or reserve declared resource weights.
2. **Recover the failed unit without changing the pool.** PIT can start another minion after a
   minion exits, StrykerJS restarts a crashed test runner a bounded number of times, nextest can
   retry a failed test with a delay, and Kubernetes can recreate a failed Pod after an increasing
   delay. Their concurrency limit does not decrease.
3. **Abort or require explicit tuning.** A subprocess launch failure aborts cargo-mutants; failed
   Go build actions propagate failure; make aborts unless keep-going is selected; StrykerJS tells a
   user who encounters OOM to lower its static concurrency.

Exact halving does have a primary-source precedent in TCP congestion control, but it is materially
different. TCP sets a slow-start threshold from half the actual flight size; after a retransmission
timeout it sets the current congestion window to one segment and then grows again. Netflix's
general-purpose AIMD limiter continuously grows again too, and its default multiplicative decrease
is 0.9 rather than 0.5. Neither is precedent for a permanent half-ceiling in a heterogeneous,
finite mutation campaign.

Therefore:

> **No inspected field precedent supports Ooze's exact `ceil(A/2)` permanent ceiling.**

That arithmetic was evaluated as a small, deterministic engineering heuristic, but issue #58 did not
choose it: it trades a logarithmic bound on repeated overload waves against potentially discarding
almost half of the concurrency that might have been safe, without providing an industry-derived
safety guarantee.

## Mutation and test runners

### cargo-mutants: static workers, cooperative child cap, abort on launch failure

cargo-mutants calculates `n_threads` once and creates exactly that many worker threads. Each worker
keeps consuming mutants from one queue; the count is not changed in response to scenario outcomes
([worker creation](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/lab.rs#L90-L113),
[worker loop](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/lab.rs#L230-L253)).

A mutation-induced timeout is an ordinary scenario result: the process tree is terminated, the
outcome is recorded, and the same fixed worker set continues. By contrast, `Command::spawn()`
returns an error, that error propagates out of the scenario, and the scoped worker join aborts the
run; there is no launch retry or admission change
([spawn path](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/process.rs#L69-L102),
[internal-error propagation](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/lab.rs#L279-L310)).

The tool's main protection is preventive. It now starts a CPU-sized GNU jobserver shared by
cooperating Cargo build descendants, while explicitly noting that Rust tests do not participate
([jobserver documentation](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/jobserver.md#L1-L12)).
Its parallelism guide warns that nested build and test fan-out can thrash or exhaust memory and asks
users to start at two or three mutation jobs
([parallelism guide](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/parallelism.md#L1-L37)).

**Recovery or re-ramp:** neither. Timeouts continue at the original count; an infrastructure launch
error aborts.

### PIT: fixed executor; replacement minions do not reduce concurrency

PIT creates a `ThreadPoolExecutor` whose core and maximum sizes are both the configured thread
count, submits every mutation-analysis unit, and shuts the pool down after submission
([`MutationAnalysisExecutor`](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/execute/MutationAnalysisExecutor.java#L20-L59)).

Within one analysis unit, PIT starts a minion, collects its results, marks unfinished work according
to the minion exit status, and starts another minion while mutations remain. A minion that dies can
therefore be replaced, but the enclosing executor's thread count is unchanged. The source explicitly
calls out insufficient memory as one possible reason a minion did not start or died
([`MutationTestUnit`](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/build/MutationTestUnit.java#L70-L129)).
A thrown `IOException` from `worker.start()` escapes the unit and ultimately fails result
collection; there is no lower-cap retry.

PIT's optional automatic thread heuristic runs once before execution. Its own source calls the
formula simplistic, says reported cores can be wrong in virtual environments, and keeps it disabled
by default "to ensure build is consistent"
([`AutoSetThreads`](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/autoconfig/AutoSetThreads.java#L11-L50)).
The documented default remains one mutation thread
([PIT Maven options](https://pitest.org/quickstart/maven/#threads)).

**Recovery or re-ramp:** a dead minion can be replaced at the same pool size. The pool never adapts
downward or upward.

### StrykerJS: bounded worker restart at the same static concurrency

StrykerJS computes concurrency once from configuration and available parallelism, creates a fixed
number of checker and test-runner tokens, and does not remove tokens after pressure
([`ConcurrencyTokenProvider`](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/concurrent/concurrency-token-provider.ts#L23-L82)).
It does add checker tokens to the test-runner pool when the checking phase has completed, but that is
a planned phase transition, not feedback from execution pressure
([phase handoff](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/concurrent/concurrency-token-provider.ts#L85-L95)).

A rejected or OOM test-runner operation enters a bounded two-attempt loop that recovers the process
without changing its resource slot. After those attempts, Stryker reports an error result
([`RetryRejectedDecorator`](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/retry-rejected-decorator.ts#L17-L77)).
Its OOM troubleshooting guidance asks the user to lower the configured concurrency; the example
changes eleven workers to four
([StrykerJS troubleshooting](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/docs/troubleshooting.md#L156-L182)).

**Recovery or re-ramp:** bounded process replacement at the same concurrency. OOM does not teach
the pool; persistent OOM requires manual configuration.

### cargo-nextest: fixed weighted slots; retry backoff means delay, not fewer slots

nextest resolves `test_threads` once while building the runner and schedules tests through a future
queue with that fixed global capacity. Per-test `threads-required` values consume weighted slots
from the same pool
([runner construction](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/nextest-runner/src/runner/imp.rs#L343-L383),
[queue admission](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/nextest-runner/src/runner/imp.rs#L744-L815)).
The heavy-test guide explicitly handles CPU and memory pressure through declared weights and warns
that reliability may cost throughput
([`threads-required`](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/site/src/docs/configuration/threads-required.md#L7-L39)).

nextest's "backoff" is a fixed or exponentially increasing _delay between attempts of one failed
test_. The retry loop does not alter `test_threads`
([retry loop](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/nextest-runner/src/runner/executor.rs#L215-L344),
[delay iterator](https://github.com/nextest-rs/nextest/blob/b2a481b1ab77c9615742e5afbb8d831e3e1f6b0b/nextest-runner/src/runner/executor.rs#L1154-L1220)).

**Recovery or re-ramp:** configured test retry at the same fixed global slot count. No capacity
learning.

## Build schedulers

### Go: a fixed `-p` worker count; an exec failure is an action failure

The Go command copies `cfg.BuildP` into a local `par`, starts that many action workers, and waits for
them. An action error is recorded and propagated to dependants; the worker count is never recomputed
([Go 1.26.6 action executor](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/work/exec.go#L73-L248)).
The subprocess path calls `cmd.Run()` once and returns its error
([Go 1.26.6 shell runner](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/work/shell.go#L600-L686)).

**Recovery or re-ramp:** neither for a local process-launch failure. The build fails at the same
configured `-p`.

### GNU make and Ninja: fixed cooperative slots plus optional load gating

GNU make's jobserver shares a fixed number of slots across cooperating descendants. Every client
must return exactly the tokens it acquired even on error
([GNU make job slots](https://www.gnu.org/software/make/manual/html_node/Job-Slots.html)).
Its optional load-average limit temporarily stops starting new recipes while system load is above a
user-set threshold; it does not rewrite `-j`. A recipe failure aborts by default or continues
independent work under `--keep-going`
([GNU make parallel execution](https://www.gnu.org/software/make/manual/html_node/Parallel.html)).

Ninja similarly uses a fixed CPU-derived/default `-j`, fixed-depth pools for known expensive rules,
and, when available, GNU jobserver slots. An explicit `-j` disables jobserver participation
([Ninja manual](https://ninja-build.org/manual.html#_gnu_jobserver_support),
[Ninja pools](https://ninja-build.org/manual.html#ref_pool)).

**Recovery or re-ramp:** load gating can pause and later resume launches at the original limit, but
neither tool learns a smaller numerical cap from a command failure.

### Gradle and Bazel: declare capacity or cost before execution

Gradle's `--max-workers` is a fixed maximum, defaulting to the processor count
([Gradle command-line interface](https://docs.gradle.org/current/userguide/command_line_interface.html#sec:command_line_performance)).
Shared build services can declare a fixed `maxParallelUsages`, and test tasks can set a fixed
`maxParallelForks`
([shared build services](https://docs.gradle.org/current/userguide/build_services.html#sec:concurrent_access_to_the_service),
[test performance guidance](https://docs.gradle.org/current/userguide/performance.html#execute_tests_in_parallel)).
The official performance guide suggests manually disabling problematic parallelism or adjusting the
fork count; it does not document failure-driven adjustment.

Bazel's local `ResourceManager` acquires declared CPU, RAM, test-count, worker, and named-resource
capacity before an action starts. It can either fail a request that exceeds total capacity or allow
one oversized action when no peer owns that resource
([resource-manager contract](https://github.com/bazelbuild/bazel/blob/5f76ff86cda0bd5b3e22053fd2ebe88eb0386f2e/src/main/java/com/google/devtools/build/lib/actions/ResourceManager.java#L55-L77),
[availability rule](https://github.com/bazelbuild/bazel/blob/5f76ff86cda0bd5b3e22053fd2ebe88eb0386f2e/src/main/java/com/google/devtools/build/lib/actions/ResourceManager.java#L626-L689)).
Bazel's local crash retry is an off-by-default workaround for a specific OSXFUSE bug, not a capacity
controller
([`experimental_local_retries_on_crash`](https://github.com/bazelbuild/bazel/blob/5f76ff86cda0bd5b3e22053fd2ebe88eb0386f2e/src/main/java/com/google/devtools/build/lib/exec/local/LocalExecutionOptions.java#L81-L94)).

**Recovery or re-ramp:** neither system changes its pool from an arbitrary action's OOM or launch
failure. Prevention depends on declared/cooperative cost; retries address narrowly classified
transient failures.

## CI and batch controllers

GitHub Actions exposes a static matrix `max-parallel`. `fail-fast` cancels queued and running matrix
siblings after a qualifying failure; it does not lower and resume the matrix limit
([GitHub Actions matrix controls](https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/run-job-variations#defining-the-maximum-number-of-concurrent-jobs)).

Kubernetes Jobs retain a requested `.spec.parallelism`. Failed Pods may be recreated after
exponential delays of 10, 20, and 40 seconds up to six minutes; too many failures fail the Job.
The controller may throttle new Pod creation because of previous failures, but the specified
parallelism is not multiplied down
([parallelism semantics](https://kubernetes.io/docs/concepts/workloads/controllers/job/#controlling-parallelism),
[Pod failure backoff](https://kubernetes.io/docs/concepts/workloads/controllers/job/#pod-backoff-failure-policy)).

These controllers reinforce an important terminology distinction: **retry backoff usually means
waiting longer before repeating a failed unit, not decreasing concurrency.** Ooze's proposed policy
is an admission decrease, not retry backoff in that conventional sense.

## Adaptive limiters: the only exact-half precedent is not a batch scheduler

### TCP halves actual flight, drops the current window to one, and grows again

RFC 5681 specifies, after loss, a slow-start threshold no greater than half `FlightSize`, not half a
possibly larger configured window. After a retransmission timeout, the current congestion window is
set to one full-sized segment and then slow-start grows it toward the new threshold
([RFC 5681 section 3.1](https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1)).

This supports one part of the Ooze proposal: if a multiplicative decrease is used, base it on actual
committed occupancy `A`, not a configured ceiling that demand did not fill. It does **not** support
setting the live limit to half and forbidding recovery for the remainder of the run.

TCP has evidence Ooze lacks: a long stream of comparable units, protocol-defined loss signals,
acknowledgements that clock new work, and repeated opportunities to relearn path capacity. A
mutation catalogue is finite and intentionally heterogeneous; a timeout may be the mutant's correct
outcome rather than congestion.

### Netflix AIMD uses a configurable ratio and deliberately re-ramps

Netflix's loss-based concurrency limiter increases by one on a healthy, sufficiently occupied
sample and multiplies by a configurable `backoffRatio` after a drop or timeout. The accepted ratio
range starts at 0.5, but the default is 0.9
([`AIMDLimit`](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/AIMDLimit.java#L25-L112)).
It therefore neither establishes half as a universal constant nor preserves a monotone ceiling.
Continuous re-ramping and oscillation are part of its intended steady-state service behavior.

## Alternatives considered for Ooze's Q4

After Ooze has confirmed that a shared trip disappears when the same mutant runs exclusively, its
evidence is only:

```text
one automatic command tree completed ordinarily
A overlapping automatic command trees correlated with pressure
```

It does not know the largest safe value between one and `A-1`. The field evidence does not fill that
gap.

| Choice after confirmed pressure | What it guarantees | Worst practical cost |
| --- | --- | --- |
| `C = 1` | Uses the only directly demonstrated safe occupancy. | Can discard almost all remaining throughput after one pressure event. |
| `C = A - 1` | Makes the smallest performance concession. | May require linearly many further pressure/confirmation waves before reaching a safe value. |
| `C = ceil(A/2)` | Needs at most logarithmically many further halvings to reach one. | May permanently discard almost half of an actually safe `A-1` capacity for this process run; it is not guaranteed safe. |
| Abort the campaign | Does not continue scoring under an environment Ooze cannot bound. | Gives up automatic recovery and useful completed work. |
| Reduce, then re-ramp | Can recover throughput using ordinary future work rather than synthetic probes. | Reintroduces oscillation and more state; the comparable adaptive controllers do this, but mutation/build tools do not. |

All five can be represented as deterministic event transitions. Deterministic simulation testing
can prove that a selected rule is replayable, monotone, bounded, and free of double-decrease; it
cannot prove that the selected numeric limit matches an opaque machine's actual resource threshold.

### Field-research judgment

Do not present `ceil(A/2)` as an established or evidence-derived constant. Issue #58 consequently
does not use `A`, a multiplier, or a pressure generation to calculate capacity. It selects the only
categorical fallback that prevents future Ooze-owned peer overlap:

```text
FullAutomatic(P) -> SingleAdmissionAutomatic
```

The transition is idempotent, affects only future grants, and adds no command on the healthy path.
It can make the remaining catalogue slower after one pressure event; that cost is the explicit
safety trade, not a learned-capacity claim. A fresh Ooze process starts from `P` again.

Deterministic simulation should validate the state transition, overlap-gated confirmation, lease
drainage, and invariants. It need not simulate midpoint arithmetic or recovery behavior that the
product did not choose.
