# Timeout confirmation cost in mutation-testing tools

_Research date: 2026-08-21. This is field research for
[Set automatic admission and pressure fallback](https://github.com/gtramontina/ooze/issues/58), not an
implementation specification._

> **Decision status:** Issue #58 deliberately chooses stronger score attribution than the field norm,
> but gates its cost on recorded peer overlap. A primary deadline is confirmed exclusively only when
> the primary's live accepted obligation had coexisted with another Ooze-owned attempt obligation. A
> deadline without recorded peer overlap becomes final after authoritative drainage. An ordinary
> exclusive confirmation irreversibly changes future automatic admission from `P` to one,
> while a repeated deadline is intrinsic and leaves admission unchanged. There is no confirmation
> toggle. [Choose the automatic runaway fuse](https://github.com/gtramontina/ooze/issues/60) has since resolved the fuse: a fixed 64-descendant ceiling, counted by parent identity, guarding automatic attempts only. A trip is directly attributable killed-class `Runaway` after drainage, never confirmed and never capacity pressure, because the count is contention-independent under the `GOMAXPROCS=1` automatic profile. `Serial()` attempts carry no fuse. Future `OOZE_*`/focused-selection work remains outside this
> runner decision.

## Executive answer

No: **re-running every timed-out mutant for another complete deadline is not the usual behavior in
the mutation-testing tools inspected here.** PIT, StrykerJS, cargo-mutants, Infection, and Mull make
the first attributable timeout final. They terminate or recover the runner, record a timeout, and
continue with a different mutant. A genuinely hanging mutant therefore consumes one timeout
allowance, not two.

Stryker.NET is the important exception, but only conditionally. It can combine multiple mutants
whose covering tests do not overlap into one test session. When that _mixed_ session times out
without identifying a mutant, it reruns the still-pending mutants individually. The actual culprit
can therefore consume a batch-session timeout and then a singleton timeout. If Stryker.NET was
already testing one mutant, however, that first timeout is immediately final and the mutant is not
retried.

That distinction matters for Ooze:

- another tool does establish precedent for repeating an **ambiguous** timeout in isolation;
- no inspected tool establishes a convention of confirming an **already attributable** mutant
  timeout merely because other independent mutation commands were running nearby; and
- the resolved Ooze confirmation intentionally provides stronger evidence against
  contention-induced false kills than most mutation tools provide, at the real cost of roughly
  `2 * deadline` for every intrinsically hanging mutant whose primary has recorded peer overlap.

## Results at a glance

`T` below means the timeout assigned to the mutant execution that hangs. Baseline and runner-startup
work are separate from the number of full timeout waits.

| Tool | Default timeout basis | What the first attributable timeout does | Same mutant run again? | Full timeout waits for a true hang |
| --- | --- | --- | --- | ---: |
| PIT | Per-test baseline: `round(normal * 1.25) + 4000 ms` | Ends the minion and records the current mutant `TIMED_OUT` | No; a new minion handles only unstarted mutants | `1T` |
| StrykerJS | Relevant-test baseline: `net * 1.5 + 5000 ms + overhead` | Restarts the runner and returns `Timeout` for that mutant | No | `1T` |
| Stryker.NET, singleton | Selected-test baseline plus 1.5 margin and 5000 ms default addition | Records the mutant `Timeout` | No | `1T` |
| Stryker.NET, ambiguous mixed batch | Same, calculated for the session's tests | Leaves the batch pending, then splits it into singleton runs | Yes, as part of attribution | `T_batch + T_single` |
| cargo-mutants | Whole-suite baseline: `max(20 s, ceil(baseline * 5))` | Kills the process tree, records `Timeout`, takes the next mutant | No | `1T` |
| Infection | Per-mutant covered-test timing, capped by a configured 10 s default | Marks that process `TIMED_OUT` | No | `1T` |
| Mull | Whole executable baseline: `max(baseline * 10, 30 ms)` | Kills the process and records `Timedout` | No | `1T` |

The tools use different timeout scopes. PIT times each selected test, StrykerJS and Stryker.NET
estimate the tests selected for a mutant or session, Infection estimates the covering test suites,
and cargo-mutants and Mull time the whole test command. The `1T` comparison means none of those
tools repeats the same known hanging execution for a second complete allowance.

## PIT: final timeout for the current mutant; restart only for later mutants

PIT measures every test without mutation and gives that test

```text
round(normal test duration * timeoutFactor) + timeoutConstant
```

with defaults `1.25` and `4000 ms`
([timeout strategy](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/build/PercentAndConstantTimeoutStrategy.java#L20-L39),
[official options](https://pitest.org/quickstart/maven/#timeoutFactor)). PIT's own FAQ explicitly
acknowledges both genuine infinite loops and false timeout classifications caused by execution-order
and class-loading variation; its documented response is to increase the timeout constant, not to
confirm every timeout with another run
([PIT timeout FAQ](https://pitest.org/faq/#im-seeing-a-lot-of-timeouts-whats-going-on)).

The timeout decorator schedules one timer for the selected test. If it expires, the timeout side
effect tells the minion to finish with `ExitCode.TIMEOUT`
([timer](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/execute/MutationTimeoutDecorator.java#L45-L72),
[side effect](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/execute/TimeOutSystemExitSideEffect.java#L6-L17)).
`TIMED_OUT` is immediately a detected status
([status definition](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/DetectionStatus.java#L22-L38)).

PIT does batch a range of mutants into a minion process. A timeout kills that process, the outer
unit maps the one `STARTED` mutant to `TIMED_OUT`, and a replacement minion receives only mutations
still marked `NOT_STARTED`
([unit recovery](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/build/MutationTestUnit.java#L70-L129),
[status partitions](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/MutationStatusMap.java#L63-L78)).
This is runner replacement, not confirmation: the timed-out mutant is not placed back in the work
set.

## StrykerJS: recover the runner, but return the timeout immediately

StrykerJS calculates a mutant's timeout from the initial-run timings of the relevant tests:

```text
timeout = netTime * timeoutFactor + timeoutMS + overhead
```

The defaults are `timeoutFactor = 1.5` and `timeoutMS = 5000`
([official configuration](https://stryker-mutator.io/docs/stryker-js/configuration/#timeoutMS),
[planner source](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/mutants/mutant-test-planner.ts#L169-L218)).

On expiry, its timeout decorator calls `recover()` to restart the test-runner process, then returns
`MutantRunStatus.Timeout` for the current mutant
([timeout decorator](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/timeout-decorator.ts#L37-L70)).
The mutation executor invokes `mutantRun` once for each plan and reports that returned result
([executor](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/process/4-mutation-test-executor.ts#L150-L166)).

StrykerJS does have a two-attempt recovery decorator, but it catches _rejected operations_, such as
a crashed or out-of-memory runner. A timeout is a successfully returned `Timeout` value after the
inner recovery, so it does not enter that retry loop
([rejection retry](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/retry-rejected-decorator.ts#L17-L77),
[decorator composition](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/index.ts#L37-L72)).
Thus a true hang costs one full mutant deadline plus process-recovery overhead, not another mutant
deadline.

## Stryker.NET: split an ambiguous mixed batch, but do not confirm a singleton

Stryker.NET is the closest direct precedent for Ooze's proposed confirmation. By default, it can
combine mutants with disjoint assessing tests into one test run to save startup time
([official `disable-mix-mutants` contract](https://stryker-mutator.io/docs/stryker-net/configuration/#disable-mix-mutants),
[group builder](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core/MutationTest/MutationTestProcess.cs#L172-L230)).

The official documentation summarizes the timeout as initial test time plus an additional timeout,
whose default is `5000 ms`
([configuration](https://stryker-mutator.io/docs/stryker-net/configuration/#additional-timeout)).
Current source uses a more precise selected-test calculation:

```text
timeout = 1.5 * (initialization overhead + estimated selected-test time)
        + additionalTimeout
```

([calculator](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core/Initialisation/TimeoutValueCalculator.cs#L6-L24),
[initial timing inputs](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core/Initialisation/InitialTestProcess.cs#L34-L51)).

The result logic deliberately distinguishes two cases:

1. If one mutant was under test, a session timeout is attributed to it immediately and it becomes
   `Timeout`.
2. If a mixed session times out or has a runtime issue and no mutant receives a conclusive result,
   the batch remains pending, `forceSingle` is enabled, and every pending mutant is run separately.

The complete transition is visible in `MutationTestExecutor`: a multi-mutant session timeout is not
passed to individual mutant analysis, while a singleton session timeout is; an inconclusive batch
then enters the one-by-one loop
([executor](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core/MutationTest/MutationTestExecutor.cs#L35-L125)).
The focused test uses two mutants and verifies exactly three runner calls—one batch plus two
singletons
([dubious-timeout test](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/src/Stryker.Core/Stryker.Core.UnitTest/MutationTest/MutationTestExecutorTests.cs#L119-L138)).

A genuine culprit in such a batch can therefore wait for `T_batch + T_single`. This is not
necessarily exactly twice the same number: the batch timeout is calculated from the union of the
batch's selected tests and can exceed the singleton timeout. Total batch recovery can cost even
more wall time because unrelated pending mutants are also run individually. Conversely, disabling
mixed mutants or receiving a timeout from a group of one gives a conclusive timeout after one
deadline.

## cargo-mutants: one whole-command timeout, then the next mutant

cargo-mutants runs one baseline test scenario, then derives the default mutant test timeout as

```text
max(20 seconds, ceil(baseline test duration * 5))
```

with explicit timeout and multiplier overrides
([official timeout guide](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/timeouts.md#L17-L34),
[calculation](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/timeouts.rs#L56-L77)).

Each worker removes one mutant from the shared queue and calls `run_one_scenario` once. A process
whose deadline expires is terminated and produces `Exit::Timeout`; that scenario is summarized as
`Timeout`, after which the worker takes the next mutant
([worker loop](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/lab.rs#L230-L253),
[process timeout](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/process.rs#L105-L138),
[outcome mapping](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/outcome.rs#L262-L289)).
The guide states this behavior directly: kill the build or test after the timeout and continue to
the next mutant. There is no timeout confirmation or batch splitting.

Its parallelism documentation accepts that high parallelism can cause spurious timeouts and tells
users they may need to set the timeout manually
([parallelism and timeouts](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/parallelism.md#L17-L21)).

## Infection: one process per mutant and immediate classification

Infection's configured `timeout` is a maximum per mutated process and defaults to 10 seconds
([official configuration](https://infection.github.io/guide/usage.html#configuration),
[default source](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Configuration/ConfigurationFactory.php#L88-L94)).
For each mutant, current source narrows that cap using coverage timing:

```text
actual timeout = min(5 seconds + 5 * nominal covering-test time,
                     configured timeout)
```

([process factory](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Process/Factory/MutantProcessContainerFactory.php#L53-L86)).
The nominal time is the summed, de-duplicated timing of the covering test suites from the initial
coverage data
([nominal-time calculation](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Mutation/Mutation.php#L167-L182),
[suite-time aggregation](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/TestFramework/Tracing/TestTotalTimeCalculator.php#L47-L90)).
If the nominal time already reaches the configured cap, Infection skips the mutant rather than
spending a predictably insufficient timeout
([pre-execution check](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Process/Runner/MutationTestingRunner.php#L151-L176)).

For a process that does run, the parallel runner catches the process timeout, marks that
`MutantProcess` timed out, and yields its final container; the result factory immediately maps the
flag to `DetectionStatus::TIMED_OUT`
([runner](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Process/Runner/ParallelProcessRunner.php#L171-L209),
[classification](https://github.com/infection/infection/blob/2580fecfe503778535a97d04eb23668efa521a5c/src/Mutant/TestFrameworkMutantExecutionResultFactory.php#L96-L123)).
There is no retry or confirmation.

Infection also exposes the field's correctness tradeoff unusually clearly. By default it counts a
timeout as killed, but `--with-timeouts` can count timed-out mutants as escaped because slow CI can
turn a would-be survivor into a timeout and inflate the score
([official timeout scoring guidance](https://infection.github.io/guide/command-line-options.html#--with-timeouts)).
It addresses the ambiguity through an explicit scoring policy, not a second execution.

## Mull: baseline times ten, one execution per mutant

Mull 0.34.0 runs the unmutated executable once, then assigns every covered mutant

```text
max(baseline running time * 10, minimum timeout)
```

where the default minimum is 30 milliseconds
([runner](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tools/mull-runner.rs#L247-L269),
[CLI contract](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/docs/command-line/generated/mull-runner-cli-options.rst#L18-L40)).

The parallel map calls `run_program` exactly once for each mutant and stores the returned result
([mutant map](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tools/mull-runner.rs#L270-L310)).
On timeout, `run_program` kills and waits for the child, then returns
`ExecutionStatus::Timedout`
([process runner](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tools/runner.rs#L74-L100)).
No retry or confirmation path follows.

## What this evidence does and does not decide for Ooze

### Source findings

1. Immediate timeout classification is the dominant mutation-tool behavior.
2. Tools commonly spend generous baseline-derived headroom—1.25x plus 4 seconds, 1.5x plus 5
   seconds, 5x with a 20-second floor, or 10x—before accepting that first timeout.
3. Runner restart after timeout does not usually mean mutant retry. PIT and StrykerJS replace or
   recover a poisoned runner but preserve the first mutant's timeout result.
4. Stryker.NET repeats work only when the first mixed execution cannot attribute the timeout. It
   does not repeat a timeout that was already observed with one mutant under test.
5. The field knows that timeouts can be false positives. PIT documents timing variation;
   cargo-mutants documents parallelism-induced spurious timeouts; Infection lets users exclude
   timeouts from the detected score. Most tools accept or expose that uncertainty instead of
   automatically resolving it.

### Ooze-specific engineering choices

There is no zero-cost observation that distinguishes these two executions:

```text
mutant truly hangs
mutant would finish, but Ooze-owned overlap starves it until the deadline
```

If Ooze requires a scored answer and refuses to let its own overlap manufacture a kill, a fresh
exclusive execution is direct evidence and the second deadline for a true hang is irreducible. The
Stryker.NET batch split supports this general **ambiguous observation -> isolated rerun** pattern.
It does not make the cost conventional across mutation tools.

The smallest coherent policies are therefore:

| Policy | True-hang latency | Evidence and consequence |
| --- | ---: | --- |
| First timeout is final | `1T` | Matches most tools. Ooze must not infer that shared admission caused pressure from this observation alone; it accepts possible contention-induced false kills. |
| Ambiguous shared timeout is confirmed exclusively | about `2T` | Strongest score attribution. A pass or ordinary test failure alone proves overlap contributed; a repeated timeout proves the mutant trips even without Ooze overlap. |
| Ambiguous shared timeout aborts unscored | `1T` | Avoids both a false kill and a second deadline, but gives up automatic completion and score continuity. |

Two simple boundaries should hold under any policy:

- If the primary attempt had no overlapping Ooze-owned command, it was already an exclusive
  observation and must never be run again merely to confirm its timeout.
- A typed hard resource-exhaustion failure is an infrastructure observation, not a mutant timeout;
  it can drive the separately agreed automatic fallback without spending another mutant deadline.

### Research judgment

Do not claim that a mandatory second timeout is established mutation-testing practice. It is not.
Ooze records overlap-gated confirmation as a deliberate score-integrity guarantee that is stronger
than the field norm.

Conversely, do not add a shortened confirmation timeout, probabilistic sampling, or a timeout
severity heuristic merely to avoid `2T`. Those approaches cannot provide the evidence the
exclusive rerun is meant to provide and would add policy states without resolving the ambiguity.
The just-engineering resolution is: accept a timeout without recorded peer overlap after drainage,
and pay once for exclusive evidence only when the latched overlap fact made attribution ambiguous. A
performance toggle would change score semantics and is therefore not added to this runner.
