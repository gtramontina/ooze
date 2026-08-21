# TigerStyle as design input for Ooze managed execution

_Research date: 2026-08-21. This is design guidance, not an Ooze coding standard._

> **Resolution note:** The later [campaign transition-algebra decision](https://github.com/gtramontina/ooze/issues/57)
> fixes the process-runtime, start-commitment, confirmation-barrier, and invariant-emergency semantics.
> This guidance preserves the TigerStyle research while using those resolved boundaries below.

## Conclusion

Ooze should adopt TigerStyle's **reasoning disciplines**, not TigerBeetle's implementation rules.
The useful core is:

- make resource and progress bounds explicit;
- distinguish programmer defects from expected operating failures;
- encode the state-machine model with types and assertions before fuzzing it;
- keep one deterministic transition authority per state machine and one explicit coordination boundary;
- account for every resource from acquisition through authoritative release; and
- design for performance early, then decide with end-to-end measurements.

The resulting Ooze architecture is not “TigerBeetle in Go”. It is a small sequential control plane
(the campaign reducer and process-runtime/broker core) directing a parallel data plane (repository
work and supervised test commands). TigerBeetle itself makes the same distinction: its synchronous state
machine is separated from parallel I/O, and its architecture says sequential execution is the
exception rather than a rule for the whole system
([control/data plane and concurrency](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/ARCHITECTURE.md#L408-L447)).

Static allocation, single-threaded execution, Zig naming and layout rules, numeric assertion and
function-length quotas, and a zero-dependency policy should not be copied. They solve constraints of
a mission-critical Zig database, not those of an embedded Go library orchestrating external
processes on three operating systems.

## Source scope

The primary source is TigerBeetle's canonical
[`TIGER_STYLE.md`](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md),
pinned here to commit `97c7a8ef385270ebe0e1b75959d3d21d134629df`. It explicitly orders its
goals as safety, performance, and developer experience and treats style as a means to those goals,
not an aesthetic end
([design goals](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L14-L33)).
Its rules must therefore be evaluated against Ooze's domain and language, rather than detached from
their rationale.

The interpretation below also uses TigerBeetle's official architecture, VOPR documentation, and
linked engineering articles. Those sources explain where the style rules are architectural and
where they depend on TigerBeetle's workload.

## Principles to adopt, with Ooze-specific meaning

### 1. Bound obligations and progress, not valid repositories

TigerStyle asks for explicit bounds on loops and queues so that nontermination and tail-latency
failures become visible
([bounds](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L90-L100)).
For Ooze, the useful bounds are operational:

- admission capacity and the number of active leases;
- the process fuse and attempt deadline;
- the provisional confirmation wave, bounded by attempts already `StartCommitted` when admission
  closes;
- one absolute bound for concurrent termination and domain drain per local drain epoch or runtime
  emergency sweep;
- retained diagnostic/trace history; and
- simulation steps, generated campaigns, and fault counts per case.

Every loop should either consume a finite collection, decrease an explicit obligation count, or be
an event loop whose progress and shutdown invariants are asserted. Backpressure is preferable to an
arbitrary rejection threshold where requests may legitimately wait.

This principle does **not** justify a hard maximum on source files, mutants, or campaign duration.
Those quantities are finite inputs or user policy, and an arbitrary library cap would reject valid
repositories. Nor should a cleanup deadline be treated as proof that cleanup occurred: expiry of a
local drain bound creates a `DrainUnconfirmed` fatal seed, while only a non-empty residual after a
containment-only process-runtime sweep becomes `CleanupUnconfirmed`. Neither releases the
execution-domain obligation.

### 2. Use assertions only for impossible internal states

TigerStyle distinguishes unexpected programmer defects from expected operating failures and treats
assertions as a fuzzing force multiplier
([assertion contract](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L104-L149)).
That distinction maps cleanly to Ooze:

- test failure, attempt deadline, process-fuse trip, admission cancellation, spawn failure, artifact
  cleanup failure, and cleanup uncertainty are typed observations, outcomes, or faults;
- an unknown logical ID, duplicate release, capacity underflow, illegal phase transition, or normal
  finish with live obligations is an internal invariant violation; and
- an assertion panic must not bypass resource safety. The process runtime must retain an emergency
  path that closes registration, admission, start commitment, and new score commitment; invalidates
  every still-live campaign; settles broker requests and leases; and drains every prospective and
  owned execution domain before re-panicking once. The runtime remains closed if the panic is
  recovered.

An `internal/assert` package can make that policy recognizable, but it should remain tiny: condition,
message, and perhaps an unreachable helper. It must not become an alternative error framework.

TigerBeetle's paired-assertion practice is valuable at ownership boundaries. Its rationale is that
the producer and consumer should each state the relevant contract, ideally from their independent
local knowledge
([paired assertions](https://tigerbeetle.com/blog/2023-12-27-it-takes-two-to-contract/)).
Useful Ooze pairs include:

- the broker asserting capacity when it emits a grant, and the campaign asserting that the grant
  matches its pending admission;
- the campaign recording a pending-start obligation, the process runtime accepting `StartCommitted`
  only against the matching lease and start gate, and the driver refusing to launch without that
  acceptance;
- the supervisor asserting domain emptiness before reporting `Drained`, and the reducer permitting
  release only after that observation; and
- `Finish` proving an empty live-obligation ledger, while `Fail(CleanupUnconfirmed)` proves a
  non-empty residual execution-obligation set whose variants include unresolved committed launches.

Test both positive and negative space: valid transitions, duplicates, stale generations, late grants,
late starts, unknown observations, and observations arriving on every side of an admission barrier.
Do not import TigerBeetle's quota of two assertions per function. Assertion placement should follow
an invariant and an independent observer, not a numeric target.

### 3. Make deterministic replay a property of the real decision core

TigerBeetle's VOPR runs production logic with clock, network, and disk nondeterminism replaced by
controlled inputs. A failure is replayed from its seed and code revision, and deterministic cases use
the same infrastructure
([VOPR model](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/internals/vopr.md#L1-L46)).
The transferable Ooze contract is:

```text
same code + initial state + configuration + ordered event trace
    -> same state transitions + ordered effects
       + terminal result or invariant diagnostic
```

The campaign reducer and process-runtime/broker core should therefore read no wall clock, OS state,
goroutine arrival order, package-global counter, random source, or Go map iteration. Logical time,
stable IDs and generations, arbitration choices, attempt observations, and fault injections enter as
explicit events. A production trace records the normalized order that actually occurred; a seeded
simulator chooses and records legal orders. The seed is not enough without the code revision and
ordered trace when real OS concurrency supplied observations.

Ooze does not need TigerBeetle's stronger byte-for-byte or physical-path determinism, which exists
for replicated storage and repair
([TigerBeetle determinism](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/ARCHITECTURE.md#L281-L307)).
Logical replay of Ooze's decisions is sufficient; child-process scheduling remains an injected,
recorded boundary.

Simulation must check both safety and liveness. In addition to random races and faults, enter a
recovery phase that admits no new faults, advances virtual time, and requires the simulated world to
reach one resolved branch: normal or unscored completion with an empty live-obligation ledger; a
containment-only runtime sweep with an empty residual and forced `Aborted` outcomes; one
containment-only `Fail(CleanupUnconfirmed)` with a non-empty residual; or one deterministic `InvariantViolation`
diagnostic followed by the modeled re-panic. A separate case should deliberately withhold the
authoritative drain observation through the final runtime bound without injecting an invariant and
require `CleanupUnconfirmed`, rather than treating a local `DrainUnconfirmed` seed as immediate
terminal failure or successful convergence.
TigerBeetle added a comparable liveness phase because continuously changing random faults can hide
livelock
([simulation testing for liveness](https://tigerbeetle.com/blog/2023-07-06-simulation-testing-for-liveness/)).

### 4. Make resource ownership monotonic and singular

TigerStyle cautions against duplicate mutable state and recommends keeping checks close to use
([state and check locality](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L372-L424)).
For Ooze, each acquired capability should create one ledger obligation before the corresponding
external effect can occur. Its lifecycle is monotonic:

```text
admission requested -> cancelled
admission requested -> granted -> returned
pending start -> cancelled before commitment
pending start -> start committed -> prospective domain -> pre-release failure proven -> released
pending start -> start committed -> prospective domain -> domain owned -> drained -> released
pending start -> start committed -> prospective domain -> domain owned -> stop committed -> drained -> released
workspace/snapshot artifact registered -> cleanup confirmed -> released
workspace/snapshot artifact registered -> cleanup failed -> settled artifact residue
```

The campaign reducer and process-runtime/broker core own logical truth. The driver may keep the native
handles needed for emergency cleanup, but that registry is a capability inventory, not a competing
state machine. Normal terminal results require every live obligation released; cleanup-unconfirmed
failure retains a non-empty residual set. These construction rules are more valuable than scattering
cleanup booleans through goroutines.

### 5. Centralize transition authority while keeping the module deep

TigerStyle recommends explicit control flow, central state manipulation, and pure leaf helpers
([control-flow shape](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L158-L183)).
Ooze should have one transition entry point per state machine, sealed phase/event types, and
phase-specific pure helpers. This means one authority, not one giant function and not a public
framework of callbacks.

The external driver normalizes OS and goroutine observations into one ordered event at a time. The
pure campaign and process-runtime cores return state plus ordered effects; their shared coordination
boundary atomically orders start commitment, primary-gate closure, confirmation barriers, runtime
emergencies, and terminal commits. The driver may dispatch independent data-plane effects
concurrently—especially starts and bounded cleanup—without moving lifecycle decisions out of those
cores. This preserves deterministic reasoning without serializing expensive mutation work.

### 6. Design performance invariants, then measure the system

TigerStyle calls for early back-of-the-envelope resource sketches and separates cheap control-plane
decisions from bulk data-plane work
([performance guidance](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L231-L264)).
For Ooze, the design invariants should be:

- healthy mutants receive no retry, probe, or extra control attempt;
- the campaign and process-runtime/broker cores do constant or small bounded work per ordinary event;
- exclusive baseline, `Serial()` primary, and confirmation waits do not preempt or rematerialize
  healthy attempts;
- all independent stops use one concurrent drain epoch and one absolute deadline; and
- automation, not users, coordinates process-local capacity.

Before optimizing implementation details, measure end-to-end wall time, attempt throughput, peak
process count, and memory on macOS, Linux, and Windows. At minimum compare a fast healthy campaign,
a process-heavy command, a provisional confirmation wave, cleanup escalation, and concurrent
campaigns against the current implementation. Keep cheap local assertions enabled; run expensive
whole-ledger checkers in deterministic simulations unless benchmarks show they are negligible.

## Rules to adapt or reject

| TigerBeetle rule | Ooze decision | Reason |
| --- | --- | --- |
| Allocate all memory at startup. | Reject; retain explicit resource ledgers and bounded retained traces. | TigerBeetle derives object counts from database capacity and avoids post-start allocation in Zig ([architecture](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/ARCHITECTURE.md#L189-L205)). Go, arbitrary mutant catalogues, and an embedded library have different memory semantics. |
| Run single-threaded. | Apply only to each pure decision core. Keep repository and child-process work parallel. | TigerBeetle's choice follows a highly contentious financial workload, not a universal safety rule ([rationale](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/ARCHITECTURE.md#L168-L187)). |
| Fixed-width integers everywhere; Zig pointer/out-parameter conventions. | Reject literal rules; use Go semantic types where confusion is plausible (`time.Duration`, IDs, generations, counts) and checked conversions at OS boundaries. | These rules depend on Zig's ABI, layout, and allocation model. Ordinary Go slice indexes and value returns should remain idiomatic. |
| No recursion, 70-line functions, two assertions per function, 100-column lines, `snake_case`. | Reject quotas; preserve their intent with bounded transition graphs, focused functions, invariant-driven assertions, and `gofmt`. | Numeric and syntactic house rules are not evidence of correctness in Go. |
| Crash on assertion failure. | Adapt: emit one deterministic `InvariantViolation`, perform process-runtime-wide emergency cleanup, then re-panic exactly once. Model every expected environment or test failure explicitly. | Ooze is a library inside a test process; a panic has host-wide consequences and cannot substitute for supervision. |
| Zero dependencies and Zig-only tooling. | Reject as policy; minimize new dependencies and assess each for portability, ownership, determinism, and maintenance cost. | Ooze should follow its Go toolchain and repository conventions rather than recreating TigerBeetle's supply-chain boundary ([dependency/tooling policy](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L474-L500)). |
| Pass every library option explicitly. | Resolve defaults once, then pass a complete immutable internal configuration to the reducer and driver. Preserve idiomatic public defaults. | Deterministic replay needs explicit resolved policy; wrapping every stable Go default adds noise without the same benefit. |
| “Zero technical debt.” | Treat known violations of attribution, cleanup, and determinism invariants as release blockers; do not use the slogan to pull speculative features into the first implementation. | TigerBeetle itself frames this as shipping solid increments even when features remain absent ([incremental context](https://github.com/tigerbeetle/tigerbeetle/blob/97c7a8ef385270ebe0e1b75959d3d21d134629df/docs/TIGER_STYLE.md#L62-L79)). |

## Minimal implementation and verification consequences

1. Implement the sealed campaign reducer and process-runtime/broker core as pure transitions before OS
   orchestration changes.
2. Add the smallest `internal/assert` surface only when the first invariant needs it.
3. Put obligation creation before every effect that can acquire an external resource, and pair the
   reducer assertion with an adapter/supervisor assertion.
4. Build a deterministic driver around the production transition functions; record logical time,
   stable IDs, fatal epochs, ordered events/effects, invariant diagnostics, seed, and code revision.
5. Start with hand-authored race traces, then seeded generation, shrinking, and a recovery/liveness
   phase. A general-purpose simulator or model checker is unnecessary until concrete state-space
   limits justify it.
6. Add platform integration tests for native domain ownership and drainage separately from reducer
   simulation.
7. Establish performance baselines before merging automatic capacity, confirmation, and cleanup
   changes; reject regressions caused by extra healthy-path attempts or accidental serialization.

## Cargo-cult and overengineering risks

- A general simulation runtime before the reducer and fake driver expose a real need.
- Arbitrary fixed repository limits merely to satisfy “bound everything”.
- Static pools or custom allocators in Go without measured memory or latency evidence.
- Serializing child commands because deterministic decisions are sequential.
- Trying to replay the physical OS/goroutine schedule instead of replaying normalized observations.
- Assertion quotas, assertion wrappers with logging/configuration, or duplicate ledgers created only
  to “pair” checks.
- Public extension points for reducers, reporters, supervisors, or schedulers before a second stable
  implementation exists.
- A cross-platform guardian, hostile-process sandbox, or exhaustive model checker in the first
  managed-execution slice.
- Microbenchmarks of reducer branches used as a substitute for end-to-end mutation throughput and
  cleanup measurements.

The right TigerStyle test for any proposed mechanism is therefore not “does TigerBeetle do this?” It
is “which Ooze invariant does this make explicit, how is it falsified under deterministic simulation,
and what measured cost does it impose on the healthy parallel path?”
