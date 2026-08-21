# Backoff after confirmed pressure: what the evidence does and does not prove

_Research date: 2026-08-21. Independent design input for
[Set automatic admission and pressure fallback](https://github.com/gtramontina/ooze/issues/58), not an
implementation specification._

> **Decision status:** Issue #58 selected this report's safety-first result and simplified it further
> by removing the assumed healthy ramp. Automatic admission starts at aggregate `P` and has one
> irreversible transition to `SingleAdmissionAutomatic` after trustworthy hard shared exhaustion or
> after a primary deadline with recorded peer overlap disappears under exclusive confirmation. There
> is no midpoint, recovery controller, capacity generation, or confirmation toggle. A deadline
> without recorded peer overlap is classified directly after authoritative drainage; process-fuse
> normalization and future `OOZE_*`/selection work remain outside this decision.

## Executive finding

`ceil(A/2)` is a coherent **search heuristic**, but it is not a safe capacity inference and it is
not justified by the evidence currently available to Ooze.

After one shared attempt trips under an actual committed overlap of `A`, then terminates ordinarily
in exclusive confirmation, Ooze knows only this:

```text
the confirmed attempt with no Ooze-owned peer: did not trip
the observed cohort at A:                         did trip
```

Even in the unrealistically favourable model where all attempts have equal cost and the host has a
stable scalar capacity `K`, this establishes only `1 <= K < A`. The integer midpoint of that
interval is exactly `ceil(A/2)`. A midpoint minimizes the worst-case number of *future binary-search
queries* only when each query reliably says whether its chosen value is below or above a stable
threshold, and both safe and unsafe answers refine the interval. The proposed permanent half-limit
does not perform that search. If half works it never explores the unused upper interval; if half is
still unsafe it creates another pressure wave.

Real mutation attempts are finite and heterogeneous. A clean cohort of cheap mutants does not prove
that the same concurrency is safe for a later cohort of expensive mutants, and one attempt may spawn
many more descendant processes than another. There need not be any stable scalar `K` to discover.

The smallest policy that satisfies the stronger product statement “after Ooze has confirmed that
its own overlap contributed to dangerous pressure, Ooze will not deliberately create another such
overlap in this process runtime” is:

```text
confirmed contention-correlated pressure -> degraded automatic mode, L = 1
```

That is the recommendation if pressure is allowed to have catastrophic cost. It is deterministic,
adds no probes, never restarts healthy commands, and costs nothing on the healthy path. Its real
trade-off is potentially severe remaining-campaign slowdown after one transient pressure event.
No arithmetic can remove that trade-off without accepting another pressure risk or importing more
observations.

If the product instead accepts bounded repeated pressure in exchange for recovery, a two-value
controller (`operating L`, monotone upper bound `U`) is the minimal honest design. It should be
recognized as a later policy experiment, not disguised as a consequence of the current evidence.

## Fixed premises

This analysis originally held several mechanics fixed while comparing reduction rules. The resolved
contract now keeps only these premises:

- production captures positive `P` once; simulation injects it;
- the automatic baseline runs exclusively, then full automatic admission begins at `P`;
- only a primary deadline with recorded peer overlap uses exclusive confirmation;
- a primary deadline without recorded peer overlap is classified directly after authoritative drainage;
- normal test failure is mutation evidence, not pressure;
- no healthy command is restarted or revoked;
- no capacity-only command, sleep, random choice, load sample, or user tuning knob is added;
- commands are opaque and their resource costs may differ;
- a pressure episode may exhaust process slots, memory, or another resource badly enough that
  repeating it is not a harmless measurement.

The rejected ramp, wave-freeze, and midpoint assumptions remain in later comparisons only as design
history. The resolved question is categorical: trustworthy pressure moves future automatic admission
from `P` to one.

## What `A` means

`A` should remain the actual number of shared `StartCommitted` execution-domain obligations at the
linearized original trip, including prospective launches. It is better evidence than a configured
limit that might not have been used. TCP makes the analogous distinction between its configured
congestion window and actual `FlightSize`: RFC 5681 explicitly warns against calculating a decrease
from the former when the latter is smaller
([RFC 5681, section 3.1](https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1)).

But `A` is still only a count of Ooze-owned roots. It is not:

- CPU usage;
- process or thread count below those roots;
- memory consumption;
- a count of interchangeable units of work;
- proof that `A-1` is safe; or
- proof that half of `A` is safe.

Exclusive confirmation strengthens attribution: removing Ooze-owned peers changed the candidate's
result. It does not measure which resource was scarce or how far the cohort exceeded it.

## A deterministic adversarial model

Start with a deliberately favourable abstraction:

```text
A = observed unsafe concurrency
K = unknown stable maximum safe concurrency, K in {1, ..., A-1}
b = permanent limit selected after pressure

if b <= K: remaining unit-cost work completes in about N / b time
if b > K:  another pressure event occurs
```

This is an exhaustive threshold model, not Monte Carlo. It gives midpoint policies every advantage:
costs are identical, capacity is stable, and every pressure observation is perfectly classified.
The production problem is harder.

For `A=8`:

| Actual `K` | `b=1` | `b=4` (`ceil(A/2)`) | `b=7` (`A-1`) |
| ---: | --- | --- | --- |
| 1 | safe, optimal | pressure again | pressure again |
| 3 | safe, 3x ideal makespan | pressure again | pressure again |
| 4 | safe, 4x ideal makespan | safe, optimal | pressure again |
| 7 | safe, 7x ideal makespan | safe, 1.75x ideal makespan | safe, optimal |

No fixed `b` dominates. With pressure cost treated as unbounded, `b=1` is the unique minimax-safe
choice. If pressure has a finite cost, the best `b` depends on that cost, the number and cost of
remaining mutants, and a prior distribution for `K`. None of those quantities is established by
the confirmation.

### Bounds that are real

If every new pressure wave applies `ceil(A/2)` to its own observed flight, admission reaches one
after at most `ceil(log2(A))` validated waves. Decrementing by one can require `A-1` waves. This is a
real advantage of multiplicative decrease when repeated overload is acceptable.

If the midpoint is safe and the true `K=A-1`, its asymptotic remaining-work slowdown relative to
the unknown optimum is less than two:

```text
(A - 1) / ceil(A / 2) < 2
```

That is the strongest performance claim available for permanent halving in the scalar-threshold
model. Its matching safety limitation is equally important: every `K < ceil(A/2)` produces another
pressure event.

| `A` | midpoint | maximum halving waves to 1 | maximum `A-1` waves to 1 | slowdown when `K=A-1` |
| ---: | ---: | ---: | ---: | ---: |
| 2 | 1 | 1 | 1 | 1.00x |
| 3 | 2 | 2 | 2 | 1.00x |
| 4 | 2 | 2 | 3 | 1.50x |
| 5 | 3 | 3 | 4 | 1.33x |
| 8 | 4 | 3 | 7 | 1.75x |
| 16 | 8 | 4 | 15 | 1.88x |
| 64 | 32 | 6 | 63 | 1.97x |

The logarithmic wave count is not a safety guarantee. Six process-exhaustion episodes can still be
six too many.

## Policy comparison

### 1. Permanent `ceil(A/2)`

Benefits:

- strict progress toward one;
- at most logarithmically many distinct pressure waves in the threshold model;
- retains at least roughly half the observed root-command concurrency;
- one small, deterministic transition with no timing windows or user option.

Limitations:

- the factor `1/2` is not inferred from Ooze's evidence;
- it can immediately repeat catastrophic pressure;
- it permanently leaves performance unused when the safe frontier is near `A-1`;
- because it does not recover upward, it is not AIMD and not binary search;
- heterogeneous descendants make “half the roots” unrelated to “half the scarce resource”.

Verdict: defensible only as an explicitly chosen risk/performance heuristic, not as the safe or
evidence-determined answer.

### 2. Permanent `A-1`

Benefits:

- smallest immediate throughput reduction;
- the previous additive ramp may have completed some work at `A-1`.

Limitations:

- a clean earlier cohort at `A-1` does not make a later cohort safe;
- it can need `A-1` pressure waves to reach one;
- with a catastrophic signal this is the most aggressive policy still described as “backoff”.

Adversarial trace:

```text
L=7: seven cheap commands overlap and one completes cleanly
L=8: one heavy command plus seven mixed siblings exhaust process slots
exclusive: the heavy command completes ordinarily
L=7: seven later heavy commands exhaust process slots again
```

The historical clean completion proves only that its particular cohort made progress. It is not a
portable safe lower bound.

Verdict: reject under the stated failure severity.

### 3. Permanent largest defensible self-contention-safe value: `1`

Benefits:

- Ooze creates no peer overlap after confirming that peer overlap mattered;
- no second Ooze-induced shared-pressure wave is needed;
- simplest state machine: `Calibrating(L)` transitions to `SingleAdmissionAutomatic`;
- no backoff constant, interval state, or re-ramp rules;
- a new Ooze process naturally starts a fresh calibration against its then-current environment.

Limitations:

- can lose up to a factor of `A-1` versus a favourable stable `K=A-1` for all remaining work;
- a transient external disturbance can therefore penalize a long campaign for the rest of the
  process lifetime;
- “safe” is scoped narrowly: a lone mutant can still be intrinsically runaway or can exhaust the
  host by itself. Existing exclusive classification and fatal-containment rules still apply.

Verdict: recommended if avoiding repeated Ooze-owned catastrophic pressure is a hard invariant.

### 4. Other fixed midpoints or multiplicative factors

`floor(A/2)`, `0.7A`, `0.9A`, geometric means, and resource-specific constants merely choose a
different point on the same unknown trade-off. For odd `A`, the arithmetic midpoint of the known
endpoints `1` and `A` is:

```text
floor((1 + A) / 2) = ceil(A / 2)
```

Choosing below it trades more potential throughput for fewer unsafe threshold values; choosing
above it does the reverse. Without a cost model or a stable threshold distribution, none is
optimal.

Verdict: changing the constant does not solve the epistemic problem.

## Rejected severity split

> This section records the smaller compromise considered before #58 selected single admission for
> both trustworthy pressure kinds. The midpoint branch below is not current policy.

Ooze already needs normalized pressure kinds for classification, so branching on an existing kind
does not require a new sensor, public option, or recovery controller:

```text
typed hard resource-exhaustion launch failure -> SingleAdmissionAutomatic
confirmed, contained deadline contention     -> permanent midpoint
```

This is materially better grounded than universal halving. A typed process/thread/memory creation
failure says the host crossed a hard boundary and may already be unable to start the cleanup or
diagnostic work Ooze needs. Repeating it has potentially unbounded consequences, so retaining peer
overlap is difficult to defend. The affected campaign already aborts without retry; single-admission
degradation protects other work in the same process runtime.

A shared deadline whose exclusive confirmation terminates ordinarily is different **if** the
supervisor guarantees that the first command tree was terminated and reaped. In that case another
deadline is expensive but bounded, and permanent midpoint backoff is a reasonable explicit
throughput/risk policy: it keeps the logarithmic distinct-wave bound and the less-than-two slowdown
bound in the favourable threshold case. It is still a heuristic, not inferred safe capacity.

The automatic runaway fuse should not be assigned to either branch in this ticket. Its meaning is
owned by the later fuse decision: a descendant/resource escape belongs with hard exhaustion; a
contained liveness expiry may belong with deadlines. Treating every future fuse as “soft” now would
prejudge the observation #60 still needs to define.

Normalization must also prefer a trustworthy hard cause when the same collapsing subtree emits a
resource-exhaustion error and later reaches the outer deadline. The macOS-style failure mode can
surface through several secondary timeouts; the last event observed is not necessarily the most
informative cause.

Verdict: #58 chose the hard no-repeat invariant and uses `SingleAdmissionAutomatic` for both
trustworthy hard exhaustion and exclusively disproved overlap-correlated deadlines. The process fuse
still awaits its own normalization decision.

## Similar mutation tools do not supply the missing inference

The inspected mutation tools use static policy rather than learning a decrement from pressure:

- StrykerJS derives a default from logical CPUs and accepts an explicit number or percentage
  ([configuration](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/docs/configuration.md#L129-L137)).
- Stryker.NET defaults to half the logical processors and exposes numeric concurrency
  ([configuration](https://github.com/stryker-mutator/stryker-net/blob/40b1c720c302b2d3d1a174297fea67415c15de05/docs/configuration.md#L317-L323)).
- PIT defaults to one thread and makes automatic core-count parallelism opt-in
  ([Maven documentation](https://github.com/hcoles/pitest-site/blob/614bec6fd16670852e94c8cc03a002f6bf05db09/quickstart/maven.markdown#L131-L139),
  [thread default](https://github.com/hcoles/pitest-site/blob/614bec6fd16670852e94c8cc03a002f6bf05db09/quickstart/maven.markdown#L202-L204)).
- cargo-mutants explicitly warns that nested build and test processes can thrash or exhaust memory,
  recommends manually starting with two or three jobs, and says CPU-proportional jobs often perform
  poorly
  ([parallelism guide](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/book/src/parallelism.md#L3-L39)).

These projects confirm the nested-concurrency risk and demonstrate several safety/performance
preferences. None of these inspected mechanisms supplies a pressure classifier followed by an
adaptive decrease factor that Ooze could inherit. In particular, Stryker.NET's “half” is an initial
CPU heuristic, not evidence that halving actual flight after process exhaustion is safe.

## What the networking literature actually supports

TCP supplies useful vocabulary and invariants, not a transferable constant.

RFC 5681 records `ssthresh = FlightSize/2` after the first retransmission timeout, but it separately
sets the live congestion window to the one-segment loss window and then uses slow start to grow back
to the threshold ([section 3.1](https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1)). Therefore:

```text
TCP timeout response: operating window = 1, future threshold = half flight, then recovery
proposed Ooze rule:    operating limit  = half flight, no recovery
```

The proposed rule borrows neither TCP's conservative landing nor its recovery mechanism.

The factor is not universal even among standardized congestion controllers. CUBIC specifies a
multiplicative factor of `0.7`, notes that a factor greater than `0.5` can produce loss for more than
one round, and says a more adaptive factor would require detailed analysis and large-scale
evaluation ([RFC 9438, section 4.6](https://www.rfc-editor.org/rfc/rfc9438.html#section-4.6)).

Netflix's loss-based concurrency limiter defaults to a `0.9` backoff ratio, permits ratios from
`0.5` to below `1`, and resumes additive increase after successes
([`AIMDLimit.java` at the inspected revision](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/AIMDLimit.java#L34-L112)).
Those are service requests sampled continually, not finite heterogeneous mutation commands.

The foundational Chiu--Jain analysis shows convergence properties for additive-increase,
multiplicative-decrease feedback under a synchronous shared-bottleneck model; it does not establish
one universal decrease constant for arbitrary workloads
([Chiu and Jain, 1989](https://doi.org/10.1016/0169-7552(89)90019-6)). Ooze has one process-local
controller rather than competing distributed senders seeking network fairness, and its pressure
observations are sparse and expensive.

The field evidence therefore supports:

- decrease actual flight rather than an unused configured limit;
- make a strict decrease after trustworthy pressure;
- coalesce a correlated wave;
- separate an operating value from a recovery threshold if recovery is desired;
- validate any factor in its own workload.

It does not support declaring `1/2` correct for Ooze.

## Two-state recovery: operating `L` versus monotone `U`

If throughput recovery after pressure is a requirement, keep these meanings separate:

```text
U = smallest excluded concurrency observed unsafe, monotonically non-increasing
L = current operating concurrency, always 1 <= L < U
```

At pressure observed at `A`:

```text
U = min(U, A)
L = a chosen landing point below U
```

Useful shared completions can then raise `L` while `L+1 < U`. No extra command is created; remaining
mutants do useful work. A later pressure below `U` tightens `U` and begins a new generation.

This state has a genuine purpose: unlike permanent halving, it can remember “do not revisit the
known-unsafe point” while permitting recovery below it. It is the minimum structure for an honest
interval-search policy.

It still has three problems here:

1. **Landing remains arbitrary.** Landing at one prioritizes safety but may take many completions to
   recover; landing at the midpoint retains the original immediate-repeat risk.
2. **Clean is cohort-specific.** A completion at `L` does not establish that `L` is safe for later
   heterogeneous commands. A purported “known-safe lower bound” is historical evidence, not an
   invariant.
3. **Recovery intentionally retests pressure.** If the true scalar threshold were `K`, additive
   recovery eventually tries `K+1`; in production a different heavy cohort may pressure even below
   that. The extra attempts are useful work, but the extra pressure episode is still caused by the
   policy.

For a long-lived homogeneous request service, continuous recovery is appropriate. For a finite
mutation catalogue with expensive confirmation and potentially catastrophic process exhaustion,
the benefit may not amortize and the learned interval may not generalize.

Verdict: do not add `U` plus recovery in the first managed-execution policy. Keep the transition
function internal and pure so a later evidence-backed policy can introduce it without public API
change. If the product explicitly accepts repeated pressure risk, this is more conceptually honest
than a permanent half-limit.

## Why provisional count or healthy-sibling count is not a capacity measure

One tempting rule is:

```text
k = candidates from the sealed wave whose exclusive confirmations terminate ordinarily
new limit = max(1, A - k)
```

This appears evidence-based: one affected outlier loses one slot; four affected attempts lose four.
It consumes confirmations already required for classification rather than adding probes.

The mapping is nevertheless invalid. `k` counts visible victims, not excess resource units.

- One process-launch failure may be the only visible victim even when the process table is hundreds
  of descendants over a sustainable level.
- One long-running heavy mutant may cross the deadline while all siblings finish just below it;
  `k=1` says nothing about whether `A-1` is safe for another heavy cohort.
- Start offsets and deadline phases determine how many siblings trip before the wave drains.
- A candidate can consume far more CPU, memory, processes, ports, or file descriptors than another;
  subtracting one root per victim assumes equal weights that Ooze explicitly does not know.
- Typed launch exhaustion may abort before any useful `k` can be confirmed.

The healthy-sibling variant has the same flaw. Siblings that finish normally prove only that those
specific attempts survived their own overlapping intervals. Some may have completed before peak
pressure, and their success does not prove that the same count of different descendants is safe.

There is also a deterministic-design cost. To use final `k`, admission must remain frozen until the
whole sealed wave settles and every provisional is exclusively classified. The answer becomes more
sensitive to OS completion ordering and deadline phase than a first linearized pressure event. A
pure simulation can replay a supplied event order exactly, but the normalization has amplified
production scheduling noise into policy state.

Worst-case repeated-wave bound:

```text
if k=1 in every distinct wave, A-k is just A-1 -> up to A-1 pressure waves
```

A hybrid such as `min(ceil(A/2), A-k)` restores logarithmic descent and reacts more strongly when
many victims appear, but it adds aggregation state without making `k` a resource measurement.

Verdict: record wave composition for diagnostics if it is already available; do not drive admission
from `k` in this ticket.

## Why the selected fallback needs no capacity generation

Every numeric policy needs pressure generations to keep a sealed cohort from applying the same
reduction several times:

```text
A=8, epoch=0
four shared candidates trip
first exclusive confirmation validates pressure
```

Without coalescing, a half policy could apply `8 -> 4 -> 2 -> 1` from one physical episode. That
pretends three independent experiments occurred. The selected transition to single-admission
automatic is instead naturally idempotent. It needs no capacity epoch or generation: later accepted
pressure observations leave the same state unchanged. Stable candidate and attempt identities remain
necessary for deterministic mutant classification and diagnostics, not for capacity arithmetic.

## Performance consequences

All compared policies have exactly zero healthy-path command overhead. The choice matters only
after a pressure event has already caused at least one exclusive confirmation.

For `N` equal-duration remaining attempts under a stable safe threshold `K`:

```text
ideal remaining time       ~= N / K
single-admission time      ~= N
safe permanent-half time   ~= N / ceil(A/2)
```

The finite catalogue matters. If pressure happens near the end, recovery machinery has almost no
time to pay for itself. If it happens near the beginning of a large catalogue and was transient,
single-admission degradation can dominate total runtime. Those are real opposing cases; neither can be ruled
out by `A` or exclusive confirmation.

A production benchmark can quantify likely cost for representative repositories, but it cannot
turn a heuristic into a guarantee. Deterministic simulation should therefore cover the entire
adversarial surface rather than a random workload distribution:

- pressure at the start, middle, and end of the catalogue;
- stable thresholds `K=1`, midpoint-minus-one, midpoint, and `A-1`;
- alternating cheap and heavy cohorts with no stable `K`;
- one visible victim despite severe overcommit;
- every member of one sealed wave becoming provisional;
- multiple old-epoch confirmations arriving after the policy transition;
- `P` values `1`, `2`, powers of two, and odd values;
- concurrent campaigns sharing the same process runtime.

FoundationDB's deterministic simulator is useful precedent for the testing architecture: external
nondeterminism is abstracted into explicit events and the production implementation is a shim,
allowing each discovered trace to replay exactly
([FoundationDB paper, section 4](https://www.foundationdb.org/files/fdb-paper.pdf)). It does not tell
Ooze which trade-off to select; it tells Ooze how to make the selection exhaustively testable.

## Recommendation for “just engineering”

Issue #58 adopts the least machinery supported by the analysis:

1. begin automatic admission at aggregate detected capacity `P`;
2. confirm a deadline only when the primary has recorded peer overlap;
3. after an ordinary exclusive confirmation, enter `SingleAdmissionAutomatic` without revoking
   active work or changing the mutant classification implied by that confirmation;
4. enter the same state after trustworthy hard resource exhaustion from shared automatic execution,
   while aborting the affected campaign without retry;
5. classify a deadline without recorded peer overlap directly after authoritative drainage; and
6. express the idempotent transition as a pure function over normalized ordered events.

No public strategy interface, worker-count option, timeout-confirmation toggle, pressure generation,
or numeric controller is warranted. Fuse meaning remains deferred rather than guessed. Future
focused selection and environment overrides can become resolved campaign inputs later; this runner
does not add speculative hooks for them.

## Facts that remain unknowable from the current signal

- which resource caused the pressure;
- how many units of that resource each attempt consumed;
- whether external load contributed;
- whether host capacity will remain stable;
- whether later mutants have comparable costs;
- whether `A-1`, half of `A`, or any value above one avoids recurrence;
- how much remaining useful work can amortize a recovery policy;
- the relative product cost of a long single-admission tail versus another potentially catastrophic
  wave.

The last item is the decision boundary. Mathematics and field precedent can expose it, but cannot
choose it on the project's behalf.
