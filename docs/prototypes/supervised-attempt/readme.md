# Supervised attempt prototype

Throwaway artifacts backing [Define the supervised attempt
contract](https://github.com/gtramontina/ooze/issues/61). **The ticket is not resolved** — these are
working notes, and the numbers below are the ones that survived an adversarial pass. Several earlier
figures did not; they are listed under "Corrections" so the record is honest rather than tidy.

Two nested modules, so neither reaches the repository's own `go test ./...`, `go build ./...`, or
mutation catalogue. Verified: `go build ./...` clean and `go list ./...` returns the same **51**
packages with these present.

## `attempt.go` — the contract

The claim under test: the whole lifecycle *policy* — deadline, descendant census, stop escalation,
drain budget, and the mapping to a typed observation — can be written once over a per-operating-system
interface small enough to be worth having.

    cd docs/prototypes/supervised-attempt && go test ./...

**428 code lines** (non-blank, not comment-only), with **43 tests and 17 subtests, all passing**, none
of which sleeps or starts a process. For scale, the three existing platform adapters hold 901 code
lines — darwin 304, linux 273, windows 324 — over a 65-line portable shell. Those files do not
disappear; they keep their syscalls. Whether this is a net reduction is for
[#67](https://github.com/gtramontina/ooze/issues/67) to demonstrate, not a claim made here.

The decision is a pure function: `advance(state, spec, observed)` reads no clock and makes no syscall,
so budget expiry — untestable today on all three platforms — is an ordinary table row.

An adversarial review found **18 correctness defects** in the first draft of this file, eleven of them
demonstrated by failing tests. All are now fixed and regression-tested. The four that would have
produced a wrong mutation score are worth naming, because they are the reason this shape needed a
prototype at all:

- An external stop produced `Settled{Exit: zero}`, whose `Passed()` is true — a campaign abort would
  have reported **every in-flight mutant as a passing survivor**. The four-observation set had no slot
  for "stopped before it concluded", so this was a gap in the type set, not a coding slip.
- A stop arriving with an expired deadline **erased the deadline trip**, turning a killed mutant into
  a survivor. The test named for that case asserted only the drain deadline, so it passed while the
  trip was silently lost.
- Broadcasting an abort the idiomatic way — `close(ch)` on a `chan time.Time` — yields the zero time,
  which is before everything, so every attempt reported `DrainUnconfirmed` **having sent zero kills**.
- A root exiting during a census was decided against a stale observation and reported as a deadline
  trip: a **survivor reported as killed**.

Two were architectural. `Release` was called on undrained domains, and on Windows the job handle
carries `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, so releasing it kills the residual a fatal seed exists to
preserve; on Linux it orphans the guardian's descendants and destroys the subreaper drainage proof.
And `Empty()` hung off `Domain` rather than `Snapshot`, so the process-table read could not be
amortized across draining domains the way the census read is — measured at 5,000 signal rounds and
5,001 emptiness checks for a single attempt.

## `probes/census` — which census sees which descendant, while the root is alive

    cd docs/prototypes/supervised-attempt/probes && go run ./census

Plants one descendant of each escape shape and samples the tree both ways. Reproduced identically
across four runs here and independently across nineteen more, including a three-deep live chain, a
zombie-exclusion check and a distinctness check:

| shape | parent-identity walk | process-group census |
| --- | --- | --- |
| plain direct child | sees it | sees it |
| double fork, no `setsid` | **misses it** | sees it |
| direct child that calls `setpgid` | sees it | **misses it** |
| double fork + `setsid` | misses it | misses it |

Each view alone saw 2 of 4; their union saw 3 of 4. The fourth is the platform limit
[#60](https://github.com/gtramontina/ooze/issues/60) already documents. The walk loses the orphan
because on darwin a process whose parent exits is reparented to `launchd`, leaving a walk rooted at the
attempt while keeping its process group.

## `probes/postexit` — and which census still sees it *after* the root exits

    cd docs/prototypes/supervised-attempt/probes && go run ./postexit

This is the drainage question, and it is not the same question. Three identical runs:

| shape | walk, root alive | group, root alive | walk, **root exited** | group, **root exited** |
| --- | --- | --- | --- | --- |
| plain direct child | yes | yes | **MISSED** | yes |
| double fork, no `setsid` | MISSED | yes | **MISSED** | yes |
| direct child that calls `setpgid` | yes | MISSED | **MISSED** | MISSED |
| double fork + `setsid` | MISSED | MISSED | **MISSED** | MISSED |

Four descendants alive after root exit; the parent walk sees **0**, the process-group census sees
**2**. Every descendant's `ppid` became 1, so a walk from an exited root is blind — but reparenting
does not change `pgid`, so the group census survives what the walk does not.

**The instruments therefore swap roles by question.** While the root is alive the walk is
load-bearing and the group adds nothing; once the root has exited the group is load-bearing and the
walk is worthless. There is no single best census, and no union worth building — each question has
exactly one instrument that works, and they are different instruments.

One refinement not implemented here: the strictly stronger drain predicate is the group census plus a
walk seeded from each *live group member* rather than from the dead root. That reaches a descendant
which called `setpgid` while its parent is still alive and still in the group. Nothing reaches the
`setsid` case.

## `probes/peaks` — does a union change #60's ceiling?

    cd docs/prototypes/supervised-attempt/probes && go run ./peaks -runs 3 -src <repo> -- <go> test -count=1 ./...

Both counts come from one `kern.proc.all` read per sample, so the two instruments see the same
instant; run-to-run variance on a build-heavy workload is far larger than the difference being
measured. The single-read shortcut is cross-checked against the verbatim census functions.

| scenario | walk | group | union | union − walk |
| --- | --- | --- | --- | --- |
| Ooze default, clean checkout (2 runs) | 3 | 1–2 | 3 | **0** |
| Ooze default, working tree (3 runs) | 18 | 2 | 18 | **0** |
| `go test -count=1 -p 32` (2 runs) | 49 | 32–33 | 49 | **0** |

**A union adds nothing to the live count on any real workload**, and the process group never once saw
a descendant the walk had missed. `-p 32` measures 49 against #60's published 50, and the clean
checkout 3 against #60's 4, so the instrument agrees with #60's and its 64 ceiling is undisturbed.

**Measure a clean checkout, and say which tree you measured.** The default command reports 18 against
the working tree because 17 of the 18 peak processes are `cmdtestrunner` helpers in their own process
groups — [#66](https://github.com/gtramontina/ooze/issues/66)'s uncommitted adversarial fixtures. That
number was nearly published as a defect in #60.

## `probes/cadence` — how often to take a census

    cd docs/prototypes/supervised-attempt/probes && go run ./cadence

Host facts, independently confirmed: `kern.maxprocperuid` 6000, `RLIMIT_NPROC` soft 6000 / hard 9000,
`kern.maxproc` 9000. Creation rate **1,225–1,352 live processes/second** for real Go `fork`/`exec` with
`Setpgid`; `docs/research/runaway-fuse-observability.md:125` records **1,747/s** for a trivial spawner,
which is the conservative figure for an overshoot bound.

The transient peak is roughly `ceiling + rate × cadence`. At 1,747/s a 50 ms cadence holds the worst
case to 151 descendants per attempt and 2,114 across 14 peers, against per-uid headroom of about
5,479 — a 2.6× margin — for well under one percent of one core.

## Corrections

Recorded because the reasoning is public and these were wrong in an earlier draft.

- **The census cost figures were not reproducible.** Published as 98–102 µs (walk), 15–16 µs (group)
  and 115–118 µs (union) from "medians of 200"; independent instruments measured 118–142, 17–19 and
  135–156 µs, and `testing.Benchmark` higher still. The published values sat at the measured minima.
  Only the **union-over-walk ratio of about 16%** reproduced, and that is the sole cost figure the
  census decision ever depended on.
- **The claimed self-check did not exist.** An earlier readme said this probe "checks its own
  arithmetic — `walk + group` must equal `union`". It did not: the code was two `Printf`s with no
  comparison and no non-zero exit, and the invariant genuinely inverted in 4 of 11 runs. It cannot be
  checked that way either — the map merge that makes the union exceed its parts costs about 42
  nanoseconds against a walk whose interquartile range is 41.7 microseconds, so the noise is roughly a
  thousand times the signal. Any such check must be paired inside one call, or not claimed.
- **The two probes contradicted each other.** `census` put the walk at 98–102 µs while `cadence` put
  the bare process-table read — which the walk contains — at 115 µs. A walk cannot be cheaper than the
  read inside it. `census` reported a median and `cadence` a mean.
- **`cadence`'s percentage column was hardcoded**, not measured, so it printed identical figures
  across runs whose measured costs differed by 18%. Its overshoot rows were also hand-rounded rather
  than being the program's output.
- **The headroom denominator mixed scopes**, dividing a system-wide live count by a per-uid cap.
- **Test counts were wrong.** "37 deterministic test cases" was the number of `PASS` lines in a
  *failing* suite.
- **Three numbers had no source and are not repeated here**: "21–25 termination rounds to empty a
  forking group", "42 of 43 processes appearing between two samples", and the `NOTE_EXIT` delivery
  latencies. The first existed only as a comment inside this prototype asserting it had been measured.
  `probes/drain` exists to measure the first properly and has not been run.
