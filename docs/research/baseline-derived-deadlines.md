# Calibrating a baseline-derived mutation deadline

_Measurement date: 2026-08-22. This is the measurement record behind
[Calibrate baseline-derived mutation deadlines](https://github.com/gtramontina/ooze/issues/59),
not an implementation specification. The harness that produced it is at
[`docs/prototypes/deadline-calibration`](../prototypes/deadline-calibration)._

## Executive answer

The wall clock of a legitimate mutant attempt, relative to the campaign's single baseline
attempt, is proportional to **the number of concurrently permitted attempts** and to nothing
else that Ooze can observe:

```
R = attemptWallClock / baselineWallClock  ~=  1 + alpha * (peers - 1),   alpha ~= 0.66..0.70
```

It is **not** proportional to package count, not driven by CPU contention, and not attributable
to any open Go toolchain bug. The exclusive case — `Serial()`, `SingleAdmissionAutomatic`, an
automatic tail attempt with no live peer — measures `R = 1.01`, because there the baseline runs
under exactly the conditions the primaries run under.

## Environment

Apple M3 Max, 10 performance + 4 efficiency = 14 logical cores, 36 GB, macOS, APFS,
Go 1.26.6 (darwin/arm64), `kern.maxprocperuid` 6000 with ~521 processes already live.
Command under test throughout: `go test -count=1 ./...`, which is Ooze's own default
(`release.go:51`).

## Method

One invocation is one campaign, reproducing the shape
[#57](https://github.com/gtramontina/ooze/issues/57) fixes: materialize a workspace from an
immutable snapshot, run the unmutated command once and keep its wall clock, then run mutant
attempts at the profile's concurrency.

Each attempt appends a unique comment to one non-test source file, chosen round-robin across
the mutable set. That changes the file's content hash — forcing a recompile and invalidating
its cached test results — while leaving behaviour untouched, so the whole suite runs. That is
the worst case a deadline must tolerate: a **surviving** mutant. A killed one exits early and
is therefore never the binding constraint.

Fixtures, chosen to vary package count over two orders of magnitude while holding per-package
content constant by construction:

| fixture | packages | shape |
| --- | ---: | --- |
| `tiny` | 1 | one function, one table test |
| `cpubound` | 12 | a xorshift digest per package |
| `plain` | 30 | two functions, two table tests per package |
| `many` | 200 | byte-identical packages apart from their clause |

## Results

Every workspace path unique, baseline included; one wave per campaign.

| fixture | packages | peers | baseline | slowest mutant | `R_max` | `alpha` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| plain | 30 | 14 | 15.36 s | 152.81 s | 9.95 | **0.689** |
| plain (repeat) | 30 | 14 | 15.16 s | 152.97 s | 10.09 | **0.699** |
| plain | 30 | 7 | 15.86 s | 76.53 s | 4.83 | **0.638** |
| cpubound | 12 | 14 | 6.72 s | 63.87 s | 9.51 | **0.654** |
| tiny | 1 | 14 | 0.58 s | 5.59 s | 9.59 | **0.660** |
| plain | 30 | 1 (serial) | 11.24 s | 11.33 s | **1.01** | — |

`alpha` spans 0.638-0.699, a 1.10x spread across fixtures of 1, 12 and 30 packages and two
peer counts. The repeat campaign reproduced to 1.4%.

Linearity in `peers` is the one relationship with out-of-sample confirmation. The 200-package
fixture gave `R_max` 5.46 at `peers = 7` and 10.81 at `peers = 14` against near-identical
baselines (92.28 s, 92.72 s) — **1.98x for 2x the peers**. No saturation appears anywhere
between 2 and 14.

Baseline stability, once paths are unique: 15.16 / 15.36 / 15.86 s across three campaigns of
one fixture, a 1.05x spread.

## The bottleneck is not CPU

In the 14-way waves the machine used roughly **0.5 of 14 cores**. A single attempt of
`go test -count=1 ./...` at `GOMAXPROCS=1` took 19.1 s wall for 4.8 s of CPU — idle 75% of the
time. The reason is that `-p` defaults to `GOMAXPROCS`
([`cmd/go/internal/cfg/cfg.go`](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/cfg/cfg.go),
`BuildP = runtime.GOMAXPROCS(0)`), so the `GOMAXPROCS=1` profile fixed by
[#58](https://github.com/gtramontina/ooze/issues/58) also pins `-p=1`: every compile, link and
test-binary exec inside an attempt is strictly serialized. On the 200-package fixture that pin
alone cost 92.28 s against 27.06 s at `GOMAXPROCS=14` — 3.4x — though it cancels in `R`,
because the baseline is measured under the same profile.

Note that `ProcessState.UserTime()+SystemTime()` covers only the waited-for child tree, so
kernel and daemon work on the serializing resource is structurally invisible to it. "Never CPU
saturated, therefore headroom exists" does **not** follow from this number.

## The artifact that inflated the first round by 2-3x

[`cmd/go/internal/work/exec.go`](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/work/exec.go)
writes the absolute package directory into the build action ID whenever `-trimpath` is absent:

```go
} else if !strings.HasPrefix(p.Dir, b.WorkDir) {
        // -trimpath is not set and no other rewrite rules apply,
        // so the object file may refer to the absolute directory
        // containing the package.
        fmt.Fprintf(h, "dir %s\n", p.Dir)
}
```

Ooze's default command sets no `-trimpath`. So **a workspace at a fresh path is a full
recompile and relink of every non-GOROOT package**, whichever file was mutated. An earlier
version of the harness reused one baseline directory while giving every mutant a fresh one; the
baselines silently collected cache hits the mutants never got. The 200-package fixture shows
the magnitude: baseline **27.06 s** at a reused path, **73.65 s** cold, and fresh-path attempts
at **70.86-73.02 s in both runs**. A one-comment change to a fixture with zero internal imports
cannot cost 44 extra seconds; a full 200-package rebuild can.

Three conclusions drawn from the contaminated round are withdrawn: that the penalty is
proportional to package count (`C ~= 0.35 s/package/peer` looked stable across N = 1..200), that
baselines carry an irreducible 2x drift, and that the `cpubound` shape needs a 19.55x
multiplier. Under symmetric paths package count drops out, baseline spread falls to 1.05x, and
`cpubound` needs 9.51x like everything else.

Production is symmetric, because #57 already requires it: "Every baseline, primary, and
confirmation workspace is materialized from that same snapshot." Had the baseline been allowed
to run in the repository root, the required multiplier would have been 2-3x larger.

## Two mechanisms ruled out

**`DiskCache.Trim` contention is not the cause.**
[golang/go#76314](https://github.com/golang/go/issues/76314) is **closed**, and the comment in
`cmd/go/internal/cache/cache.go` citing it *is that fix*, present in the toolchain used here.
`trimInterval` is 24 hours and `mtimeInterval` 1 hour, so neither can produce a stable
per-attempt cost. The only `lockedfile` use in that file guards `trim.txt`; `go help build`
states the cache is safe for concurrent invocations.

**Efficiency cores are not the cause.** With ~0.5 of 14 cores demanded, nothing forces
efficiency-core placement. They do show up in one place: exclusive `GOMAXPROCS=1` baselines were
visibly bimodal (18.27-18.82 s against 21.55-22.94 s, a 1.26x peak-to-peak gap with nothing
between), which is the signature of a single-threaded action chain being parked on an
efficiency core. The `GOMAXPROCS=14` equivalents were tight, 5.12-5.50 s.

## Host safety, for the record

A bounded probe started 250 children in 200 ms — **1248 processes/second** via `os/exec`,
cross-checking the 1747/second recorded on
[#60](https://github.com/gtramontina/ooze/issues/60) with a cheaper spawner. With ~5,479
processes of headroom on this host, a runaway that keeps children alive exhausts the per-uid
process table in **3-4 seconds** regardless of the deadline. Under the automatic profile the
64-descendant fuse stops it in roughly 37 ms. So the deadline does not bound the *peak* of a
runaway; it bounds how long the host stays saturated. That is a real cost, but a duration cost,
and it is why tightening the deadline is not a sound way to buy host safety.

## Field comparison

Ooze times a whole opaque command, so it cannot use the per-covering-test baseline that PIT,
StrykerJS, Stryker.NET, Infection and mutmut use. Among whole-command tools:

| tool | formula | floor |
| --- | --- | --- |
| cargo-mutants | `max(20s, ceil(5 * baseline))` | 20 s |
| Mull | `max(10 * baseline, 30ms)` | 30 ms |
| gremlins | `max(baseline, 1s) * 5` | 1 s on the baseline |
| dextool | `max(1s, (2 + 2*sqrt(i)) * baseline)`, 3-sample rolling mean | 1 s, "OS jitter and load" |

Two cautions, both established while calibrating. gremlins tests a **single package** per mutant
in its default mode against a whole-repo coverage baseline, and cargo-mutants defaults to **one
mutant at a time** with its build phase unbounded and the 5x applying to the test phase only.
Neither is like-for-like with Ooze's concurrent whole-command attempts, and their multipliers
cover incremental work rather than a full module rebuild.

gremlins also passes `go test -timeout (outer + 2s)` so its own deadline always fires first,
explicitly "because the Go test command doesn't make it easy to distinguish failures from
timeouts". Ooze cannot do this, since `WithTestCommand` is opaque.

Only **dextool** re-tests a timed-out mutant under a larger deadline, and only it samples the
baseline more than once. No peer-reviewed work measures how often a mutation-testing timeout is
a false positive; the canonical flakiness study (Shi, Bell and Marinov, ISSTA 2019) explicitly
excludes timeout-classified mutants.

## Interaction with `go test -timeout`

Go's own per-test-binary timeout defaults to **10 minutes** and *panics*, producing a non-zero
exit. Whichever deadline is smaller decides whether a hanging mutant is reported `TimedOut` by
Ooze or `Killed` through the command's exit status. Both are killed-class under #57, so the
score is unaffected — but for any suite whose baseline exceeds roughly 24 s at `peers = 14`, the
derived deadline exceeds 10 minutes and `TimedOut` becomes unreachable for in-test hangs.

`internal/cmdtestrunner/cmdtestrunner.go` treats any non-zero exit as a kill without inspecting
output, so nothing currently distinguishes a genuine test failure from a `panic: test timed out`.

## Changing the constants changes reported scores

The constants are internal — [#52](https://github.com/gtramontina/ooze/issues/52) forbids
exposing a multiplier — so adjusting one breaks no API. It does change results. The deadline
decides which mutants land killed-class `TimedOut` and which are `Survived`, so tightening it
raises the reported score and loosening it lowers it. Users gate CI on
`WithMinimumThreshold`, so anyone sitting near their threshold sees a build flip on upgrade
with nothing in the API to explain it.

Adjustment is also asymmetric in risk. Raising a constant is safe: fewer false deadlines.
Lowering one risks a false deadline, and a false deadline under overlap consumes the
process-wide confirmation net described on
[#74](https://github.com/gtramontina/ooze/issues/74), after which every later trip in that
process scores unconfirmed. So the constants ratchet up more easily than down.

**mutmut has the clean answer, for whenever Ooze caches mutant verdicts across runs.** It
fingerprints the timeout configuration separately from everything else, so changing
`timeout_multiplier` or `timeout_constant` invalidates **only** timeout-classified verdicts
and keeps every other cached verdict intact — its own comment reads "Timeout config only
reclassifies timeouts; keep every other verdict"
([`__main__.py`](https://github.com/boxed/mutmut/blob/cd2f73da310c3fc90cffcb3e6c768cdeac14e18c/src/mutmut/__main__.py#L1096-L1098),
[`configuration.py`](https://github.com/boxed/mutmut/blob/cd2f73da310c3fc90cffcb3e6c768cdeac14e18c/src/mutmut/configuration.py#L207-L208)).
Ooze caches nothing today and result caching is outside the managed-execution destination, so
this is a pointer rather than a requirement.

Two consequences that are *not* deferred:

- Whoever writes the release notes for this change should say that mutation scores may move,
  and in which direction, rather than leaving users to discover it from a red build.
- [#64](https://github.com/gtramontina/ooze/issues/64) should record the resolved deadline in
  the trace. A stored trace replayed after a constant change otherwise produces different
  outcomes with nothing to indicate why, which quietly weakens the replay guarantee
  [#57](https://github.com/gtramontina/ooze/issues/57) asks for.
