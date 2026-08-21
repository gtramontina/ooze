# Backoff after confirmed shared-execution pressure

_Research date: 2026-08-21. Scope: issue
[#58](https://github.com/gtramontina/ooze/issues/58), specifically the decrease after an
automatic attempt trips under shared execution but terminates ordinarily when confirmed alone._

## Decision status

> **The midpoint recommendation originally explored by this report is superseded.** Issue
> [#58](https://github.com/gtramontina/ooze/issues/58) resolved the smaller categorical policy:
>
> ```text
> FullAutomatic(P) -> SingleAdmissionAutomatic
> ```
>
> The transition occurs after trustworthy hard resource exhaustion from shared automatic execution,
> or after a primary deadline with recorded peer overlap disappears in exclusive confirmation. It is
> irreversible for that process runtime and affects only future grants. There is no ramp,
> multiplier, pressure-generation arithmetic, recovery, or confirmation toggle. A deadline without
> recorded peer overlap is classified directly after authoritative drainage; process-fuse
> normalization remains deferred.

The source findings below remain useful: no cited controller establishes a correct Ooze multiplier,
adaptive controllers recover or re-probe, and opaque mutation attempts do not provide the dense,
comparable samples those controllers require. Those findings ultimately argue for the categorical
fallback rather than the earlier `ceil(A/2)` compromise. Future `OOZE_*` overrides and focused
selection are not part of this runner decision.

## What is actually known

After exclusive confirmation, Ooze has only this evidence:

```text
the confirmed attempt completed at Ooze shared flight 1
the original attempt tripped while Ooze shared flight was A
```

This supports `1` as a demonstrated point for that attempt profile and `A` as a
contention-correlated bad point. It does not reveal the largest safe integer between them, prove
that every future mutant is equivalent, or distinguish CPU, memory, process-table, I/O, and
external-host contention.

Consequently, no choice above `1` is guaranteed safe:

| Next limit | Benefit | Failure mode |
| --- | --- | --- |
| `A-1` | Discards only the observed bad point. | With a true safe limit of one, it can require linearly many further pressure/confirmation waves. |
| `floor(A/2)` | At most half remains; slightly more conservative for odd `A`. | Can permanently discard one more safe slot than `ceil(A/2)`; no source resolves the rounding choice. |
| `ceil(A/2)` | Integer midpoint; keeps at least half and reaches one in logarithmically many pressure waves. | May still be unsafe, and permanently discards the untested upper half. |
| `1` | Prevents further Ooze-owned overlap if singleton safety transfers. | Potentially serializes a long remaining catalogue after one transient event. |
| midpoint plus re-ramp | Can recover untested capacity below the known bad point. | Deliberately approaches pressure again and adds a second controller. |

The asymmetry matters: another timeout, runaway, or launch-exhaustion wave costs far more than one
unused slot. That argues for the selected transition to one. A transient host event can consequently
dominate the remainder of a large campaign; #58 accepts that explicit exceptional-path cost rather
than inventing a multiplier from an unmeasured cost ratio.

## Transferable evidence from congestion controllers

### Reno: actual flight and episode coalescing transfer; the full response does not

RFC 5681 sets `ssthresh` to no more than half of `FlightSize` and explicitly warns against using
the potentially larger configured congestion window. It also holds the threshold constant for
retransmissions of the same segment and treats a later loss window as a new congestion indication
([RFC 5681 section 3.1](https://www.rfc-editor.org/rfc/rfc5681.html#section-3.1),
[section 4.3](https://www.rfc-editor.org/rfc/rfc5681.html#section-4.3)).

Those were relevant precedents while evaluating arithmetic based on committed `A` and a pressure
epoch. The resolved categorical fallback needs neither. On a retransmission timeout Reno sets the
operating congestion window to one segment, then slow-starts back to the half-flight threshold and
continues additive growth. Ooze's superseded permanent-half proposal therefore copied neither Reno's
immediate timeout response nor its recovery loop.

### CUBIC and BBR: different constants expose the workload dependency

CUBIC deliberately uses `0.7`, rather than Reno's `0.5`, to balance utilization and convergence.
Its specification warns that a factor above `0.5` can cause loss for more than one round and says a
more adaptive factor would require detailed analysis and large-scale evaluation
([RFC 9438 section 4.6](https://www.rfc-editor.org/rfc/rfc9438.html#section-4.6)). CUBIC then probes
upward again.

The current BBRv3 working-group draft also uses a `0.7` bound, preserves a measured delivered or
transmitted-flight floor, reacts only once per bandwidth probe, and later performs explicit
precautionary probing. It additionally discusses how delayed loss detection can make flight at
detection time underestimate flight at the causal point
([BBRv3 draft sections 5.3 and 5.5](https://datatracker.ietf.org/doc/draft-ietf-ccwg-bbr/)). This
supports event coalescing and careful flight semantics, not importing BBR's state machine into
Ooze.

### Netflix and Envoy: continuous comparable samples are a prerequisite

Netflix's `AIMDLimit` defaults to a `0.9` backoff ratio, permits configuration from `0.5` up to
but excluding `1.0`, multiplies the current limit rather than observed flight, and resumes
additive growth when demand reaches half the limit
([source](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/AIMDLimit.java#L25-L112)).
Its `Gradient2Limit` instead uses short- and long-term RTT averages, clamps the gradient, adds queue
headroom, and smooths every update
([source](https://github.com/Netflix/concurrency-limits/blob/78a74b9878d38c4c048b0304ce12a162ab7b7222/concurrency-limits-core/src/main/java/com/netflix/concurrency/limits/limit/Gradient2Limit.java)).

Envoy similarly calculates `new = gradient * old + headroom` from periodic latency samples and
periodically lowers concurrency to remeasure minimum RTT
([adaptive-concurrency documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/adaptive_concurrency_filter.html),
[configuration API](https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/adaptive_concurrency/v3/adaptive_concurrency.proto)).

These systems receive abundant requests from a service distribution and intentionally oscillate or
recalibrate. A mutation catalogue is finite and mixes compile failures, quick kills, full-suite
survivors, and pathological mutants. Their constants and latency windows are not transferable.

## Why `A - confirmedAffectedInWave` is unsound

An affected count is not a measure of how much capacity caused pressure. A sibling that completes
normally may still have consumed the CPU, process slot, memory, file descriptor, or external
service capacity that made a different sibling trip. Which member becomes the visible victim also
depends on command shape and completion ordering.

DCTCP is the relevant counterexample, not a precedent. It can scale its decrease by congestion
fraction because every acknowledged byte carries comparable ECN information over a defined
round-trip observation window; it computes the fraction of marked bytes and updates
`cwnd = cwnd * (1 - alpha/2)` once per window
([RFC 8257 section 3.3](https://www.rfc-editor.org/rfc/rfc8257.html#section-3.3)). Ooze has sparse,
heterogeneous terminal outcomes, not per-resource-unit marks. Counting `k` confirmed victims and
setting `A-k` would commonly degenerate to `A-1` and make the result sensitive to incidental event
ordering.

Proportional Rate Reduction does not help: it paces delivery during recovery toward a target chosen
by another congestion algorithm; it does not infer the target from the number of losses
([RFC 9937 section 6](https://www.rfc-editor.org/rfc/rfc9937.html#section-6)).

Therefore candidate count must not drive admission. The selected transition to single-admission
automatic is naturally idempotent, so it needs neither capacity arithmetic nor pressure generations;
stable candidate identity remains necessary only for deterministic outcome classification and
diagnostics.

## More analogous schedulers avoid this inference

Build schedulers generally use fixed shared resource budgets rather than learning a new budget from
failed actions. GNU make's jobserver shares an exact token pool with cooperating descendants
([job slots](https://www.gnu.org/software/make/manual/html_node/Job-Slots.html)); Gradle uses one
build-wide worker pool, by default processor-count sized
([parallel native compilation](https://docs.gradle.org/current/userguide/native_software.html#sec:parallel_native_compilation));
and Bazel accounts against configured CPU, RAM, and named action resources
([command reference](https://bazel.build/reference/command-line-reference#flag--local_resources)).
An action failure is not treated as a capacity sample.

CockroachDB is a useful middle case. Its admission design explicitly notes extreme work-size and
CPU/I/O heterogeneity. Where it has completion and high-frequency scheduler telemetry it adjusts
KV concurrency with additive increments/decrements; where it lacks a reliable completion indicator
it does not dynamically adjust token burst sizes
([admission-control design](https://github.com/cockroachdb/cockroach/blob/master/docs/tech-notes/admission_control.md#slot-adjustment-for-kv)).
Its separate elastic-CPU controller continuously samples scheduling latency and changes a CPU-time
allowance in small steps
([controller description](https://www.cockroachlabs.com/blog/rubbing-control-theory/#35-experimentation-and-analysis)).

Ooze lacks declared command costs, dense telemetry, and a portable subtree resource meter. This
evidence favors a small categorical loss response, not a new estimator.

## Why recovery was not selected

Every cited adaptive congestion controller can grow again. Any monotone process-local reduction makes
the cost of a false positive or transient host condition last for all remaining campaigns in that
runtime. A midpoint without recovery would not be a bracket search; it would simply abandon the
upper half.

A principled future recovery design would need to distinguish:

```text
observed bad bound B = min(B, A)
operating limit L = midpoint below B
```

and permit useful work to raise `L` only below `B`. That can reclaim capacity, but it adds a
calibration regime and makes another expensive pressure episode an intentional possibility. A fresh
Ooze process already starts at `P`. Issue #58 therefore selected no recovery state at all: the
process-local transition to one is the smaller runner and the stronger no-repeat guarantee.

## Deterministic validation required before shipping

Field precedent cannot validate mutation workloads, but deterministic simulation can validate the
selected runner contract over injected `P`, catalogue lengths, heterogeneous attempt durations, and
reordered outcomes. At minimum verify:

- healthy automatic admission begins at `P` without a probe or ramp;
- the first trustworthy pressure transition to one is idempotent regardless of candidate count or
  delivery order;
- no active lease is revoked and no future automatic peers overlap after existing leases drain;
- only a deadline with recorded peer overlap receives exclusive confirmation;
- a deadline without recorded peer overlap becomes directly attributable after drainage; and
- normal operation adds no command while ambiguous shared deadlines add exactly the required
  exclusive confirmation.

Cross-platform benchmarks should still measure the remaining-catalogue slowdown after degradation.
If that exceptional-path cost later proves material, recovery would be a new evidence-backed design,
not an unexplained multiplier added to this runner.
