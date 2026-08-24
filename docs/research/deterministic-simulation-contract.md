# Deterministic simulation contract: trace, generation, shrinking, and conformance

_Research date: 2026-08-24. This is a design input for [#64](https://github.com/gtramontina/ooze/issues/64), not a resolution or implementation specification._

## Conclusion

The smallest credible contract has two representations, not one:

1. a **canonical typed in-memory replay trace** containing an immutable definition plus an ordered sequence of
   normalized ingress records with stable logical identities; and
2. an unpersisted **fuzz-choice byte stream** consumed by a state-aware generator that selects only
   currently enabled actions.

Treating arbitrary fuzz bytes as a serialized event trace conflates syntax rejection with semantic
invariant testing and gives Go's byte minimizer permission to destroy event boundaries. Treating a Go
fuzz corpus entry as the durable trace also couples replay to an internal corpus encoding that is not
Ooze's contract. The replay trace should instead be an Ooze value independent of the fuzz corpus;
fuzz bytes are merely one way to choose a legal path through it. A durable trace codec remains
deliberately deferred by [#55](https://github.com/gtramontina/ooze/issues/55) and the parent map.

The ordinary generator should never inject stale, duplicate, contradictory, unknown, or phase-invalid
events. A separate table/property suite should construct those malformed **normalized values**
deliberately and require the resolved deterministic `InvariantViolation` cleanup and re-panic. A custom
semantic shrink pass is required for promoted counterexamples; Go's minimizer is a useful first pass over
choice bytes, but it neither understands enabled actions nor preserves a particular failure identity.

## What the accepted decisions already bind

The campaign algebra fixes the machine boundary: `Begin(definition)` and `Advance(state, event)` are
pure, consume one inert normalized event, and return ordered effects, a terminal, or a fatal fault. The
runtime/broker core is another pure machine. Neither may read time, I/O, the environment, goroutine
scheduling, globals, or map iteration order. The same definition and ordered ingress must reproduce
state, effects, election, commitment, outcomes, and invariant diagnostics
([#57 resolution](https://github.com/gtramontina/ooze/issues/57#issuecomment-5365009491)). Production
chooses a linearization of real concurrent ingress and records it; simulation chooses one explicitly.

The later decisions narrow the generated vocabulary rather than adding policy choices:

- A primary deadline is provisional only when its accepted execution-domain obligation has recorded
  peer overlap; the fact is latched when two `StartCommitted` obligations coexist, not sampled at the
  trip instant. A lone deadline is direct. An ordinary exclusive confirmation that removes the overlap
  moves automatic admission to the fallback; a repeated deadline does not
  ([#51 refinement](https://github.com/gtramontina/ooze/issues/51#issuecomment-5366364348)).
- Automatic admission is the monotonic two-state sum `FullAutomatic(P) |
  SingleAdmissionAutomatic`. Trustworthy shared launch exhaustion or an overlap-attributed deadline
  disappearing in confirmation makes the one irreversible transition; there is no ramp, recovery,
  capacity arithmetic, or revocation of live leases
  ([#58 resolution](https://github.com/gtramontina/ooze/issues/58#issuecomment-5366365199)).
- An automatic fuse trip is one contention-independent injected event, counted from the attempt root
  by parent identity at the fixed ceiling 64. It is directly attributable after drainage, never
  confirmed, never pressure, and absent from serial attempts
  ([#60 resolution](https://github.com/gtramontina/ooze/issues/60#issuecomment-5377011082)).
- The mutation deadline is resolved once from the baseline observation and immutable `peers`; the
  resolved duration must be recorded because tuning the internal constants would otherwise make an
  old trace silently change meaning
  ([#59 resolution and codicil](https://github.com/gtramontina/ooze/issues/59#issuecomment-5378843338),
  [#64 propagation note](https://github.com/gtramontina/ooze/issues/64#issuecomment-5380593648)).
- Supervision contributes generation-correlated launch facts, the `StartCommitted` cut, `LaunchBy`,
  the serialized completion-or-nil boundary snapshot, separately recorded `Stop.At` and `DrainBy`,
  monotonic action tokens, inclusive command-bound eligibility, output cutoff/prefix facts, and stable
  prospective-versus-owned emergency residuals. Production stamps facts after native reads; pure tests
  inject them
  ([#61 resolution](https://github.com/gtramontina/ooze/issues/61#issuecomment-5386039287)).

The command-count bounds are consequently derivable, not generator knobs: exactly one baseline for a
non-empty catalogue; at most `P` automatic shared requests or leases per campaign; at most one serial
primary request or lease; at most one outstanding confirmation barrier/request/lease; at most one
primary and, only for an overlap-provisional deadline, one confirmation per mutant; never a third
attempt ([#57](https://github.com/gtramontina/ooze/issues/57#issuecomment-5365009491)).

## Evidence in the current implementation

The current branch already has the important seam. `processRuntime` methods take and return values;
the process shell serializes calls under one mutex and invokes those same core methods for admission,
start commitment, attempt observations, emergency settlement, and terminal commitment
([core](https://github.com/gtramontina/ooze/blob/2bd4a9772846bec7131691911beec51bec40028e/internal/ooze/process_runtime.go#L292-L434),
[shell](https://github.com/gtramontina/ooze/blob/2bd4a9772846bec7131691911beec51bec40028e/internal/ooze/process_runtime_shell.go#L108-L234)).
The supervisor likewise has one typed `supervisorEvent -> (state, ordered actions)` reducer which its
driver consumes
([reducer](https://github.com/gtramontina/ooze/blob/2bd4a9772846bec7131691911beec51bec40028e/internal/ooze/supervisor_reducer.go#L371-L422)). Existing
contract tests are already handwritten ordered traces covering overlap/barrier ordering, pressure,
emergency settlement, stale facts, duplicates, and equality boundaries
([runtime traces](https://github.com/gtramontina/ooze/blob/2bd4a9772846bec7131691911beec51bec40028e/internal/ooze/process_runtime_contract_internal_test.go),
[supervisor traces](https://github.com/gtramontina/ooze/blob/2bd4a9772846bec7131691911beec51bec40028e/internal/ooze/supervisor_reducer_internal_test.go)).

Two shell details must not leak into the trace. Admission authority currently embeds a delivery
channel, so Go equality includes process-local channel identity; the pure tests intentionally omit
that shell-only field. Also, supervisor values use `time.Time`, whose in-memory location and monotonic
components are not a portable serialized identity. The trace projection therefore needs stable
request/action/generation IDs and canonical integer instants or durations; adapters may retain channels
and `time.Time` privately.

There is no existing whole-world snapshot cut: `processRuntimeShell` serializes runtime transitions
under its mutex, while `supervisorDriver` serializes supervisor transitions under a different mutex.
One atomic process-runtime-wide record sequence can preserve their actual accepted order without adding
a global transition lock, but conformance can compare complete state only within the authority that
committed each record. A complete composed projection is sound at quiescent barriers and terminal
settlement; causally independent cross-authority transitions must commute when replay swaps them.

## Go fuzzing constraints

Go fuzz targets accept only primitive values, `string`, and `[]byte`; a structured `Trace` cannot be a
native fuzz argument. Targets are required to be fast, deterministic, independent of shared state, and
not retain mutable inputs
([`testing.F.Fuzz`](https://github.com/golang/go/blob/go1.26.0/src/testing/fuzz.go#L150-L209)). Seed
entries come from `F.Add` and `testdata/fuzz/<name>`, while discovered crashers are written as corpus
files. That is valuable regression persistence, but the corpus format is Go's own versioned textual
encoding of supported arguments, not an application schema
([testing source](https://github.com/golang/go/blob/go1.26.0/src/testing/fuzz.go#L56-L80),
[corpus encoding](https://github.com/golang/go/blob/go1.26.0/src/internal/fuzz/encoding.go#L18-L101)).

Only `string` and `[]byte` arguments are structurally minimizable. The Go 1.26 minimizer deletes tails,
individual bytes, and byte ranges, then substitutes printable bytes
([minimizer](https://github.com/golang/go/blob/go1.26.0/src/internal/fuzz/minimize.go#L10-L94)). It
re-runs the target and accepts a candidate when it still produces **an** error; the worker updates the
reported error and does not compare invariant kind, trace location, or message
([worker](https://github.com/golang/go/blob/go1.26.0/src/internal/fuzz/worker.go#L845-L920)). Minimization
is also bounded by time or invocation count and can be disabled
([coordinator options](https://github.com/golang/go/blob/go1.26.0/src/internal/fuzz/fuzz.go#L27-L69)).

Consequences:

- a decoder over arbitrary serialized trace bytes mostly explores malformed syntax, not legal machine
  transitions;
- deletion can join or corrupt event encodings instead of removing an event;
- a minimized failure may be a different invariant from the one first found; and
- “Go minimized it” is not evidence of a semantically minimal trace.

## Options and tradeoffs

| Choice | Benefit | Cost / failure mode |
| --- | --- | --- |
| Fuzz serialized typed traces directly | One apparent artifact; automatic corpus persistence | Most mutations are invalid encodings; byte shrinking destroys boundaries; malformed syntax competes with semantic invariants |
| Fuzz a seed plus PRNG | Compact and easy to replay | Generator changes make old seeds mean something else; poor shrinkability; the seed alone is not a durable trace |
| Fuzz choice bytes, capture the produced typed trace | Every byte string can select a legal path; Go can shorten choices; replay is independent of generator evolution | Requires a small deterministic choice consumer and promotion into a typed Go fixture |
| Handwrite only structured property tests | Precise failures and shrinking | Gives up coverage-guided discovery of unexpected long interleavings |

**Recommendation:** use choice bytes for discovery and capture the generated typed trace in failure
output. Promote important failures into typed Go fixtures, keeping choice bytes as provenance rather
than the sole replay key. This preserves Go's coverage-guided engine without assigning its byte corpus
architectural authority or prematurely choosing a durable codec.

## Proposed first contract

### Canonical replay value

An internal typed `Trace` should contain:

- the immutable definition, including detected capacity, mode, stable catalogue identities/order, and
  execution profile;
- an ordered ingress sequence number assigned at the production linearization point;
- one sealed event tag and payload per record;
- stable campaign, request, attempt, generation, action, obligation, emergency-epoch, and output
  identities—never pointers, channels, PIDs as logical identity, or map-derived ordering;
- canonical integer instants/bounds sufficient for the #61 before/equality/after rules, including
  `LaunchBy`, accepted `At`, local and emergency `DrainBy`, and which command bound fired; and
- the serialized launch/emergency boundary snapshot, completion-or-nil, output cutoff/prefix facts,
  and stable ordered residual facts required by #61; and
- the resolved mutation deadline on the existing baseline-settlement record: #64 says it adds no event
  kind, while #59 requires the causal value to be recorded rather than silently recomputed.

Record normalized ingress, not wakeups or reducer-derived effects. For regression evidence, a fixture
may additionally store an expected per-step digest/transcript of projected state and ordered effects,
but replay must be able to recompute it.

### Legal stateful generator

`Enabled(world)` should derive a finite ordered list of next actions from the current campaign,
runtime/broker, supervisor state, and outstanding emitted effects. Choice bytes select an index and
bounded payload values. The generator then applies the real transition and updates the world. Stable
ordering is essential: catalogue order first, then logical identity; never range over a map.

Generation should deliberately alternate progress and recovery phases so it does not merely accumulate
waiting work. Its enabled set must include settlement/return, closure-set drainage, barrier binding,
confirmation completion, terminal commitment, and emergency sweep settlement whenever applicable.
Bound the world by the accepted command-count rules above and end each trace with a deterministic
drain-to-quiescence phase unless the property intentionally targets a live boundary.

Seed focused traces for every list in #64: late grants; start versus closure; overlap versus a lone
trip; catalogue-ordered multiple provisionals and FIFO barriers; the one pressure transition and
repeated intrinsic trips; terminal versus fatal commitment; global drain expiry; mixed
containment/invariant dominance; and #61's before/equality/after, stale/duplicate, revocation,
late-adoption, and residual-order cases. The stale/duplicate cases belong to the negative suite, not
ordinary legal generation.

### Negative malformed suite

Start from a known legal prefix, then apply exactly one named corruption: unknown/stale generation,
duplicate fact or completion, wrong action token/kind, contradictory completion, phase-impossible
event, zero/missing correlation, release after revocation, or malformed emergency settlement. Assert:

1. the rejected normalized value and pre-state identify the same `InvariantViolation` on replay;
2. no illegal ordinary effect or score is committed;
3. deterministic emergency cleanup runs with the expected ordered obligations/residual; and
4. the original violation is re-panicked after cleanup, including mixed-cause dominance.

Parser/codec errors should be tested separately. They are not normalized events and therefore are not
`InvariantViolation` inputs to the state machine.

### Shrinking expectations

After Go finds a failure, replay and semantically shrink the typed trace while preserving a stable
failure fingerprint: property name plus invariant/terminal kind and the relevant stable identities.
Use, in order:

1. remove whole ingress records or causally independent spans, replaying legality after each removal;
2. remove unrelated campaigns, attempts, catalogue members, and settled obligations;
3. reduce `P`, catalogue size, and demand within the accepted lower bounds;
4. canonicalize logical IDs by first appearance; and
5. move instants toward the named before/equality/after boundaries rather than toward numeric zero.

For ordinary legal generation, a candidate that is no longer enabled is rejected or regenerated from
its remaining choices; it is never fed to the reducer as a negative test. For the malformed suite,
retain the one intended corruption and shrink only its legal prefix and irrelevant payload.

### Production/simulation conformance

Do not build a second “simulation implementation.” The smallest harness is a recorder around the
production ingress boundary plus the existing pure transitions:

1. run a scripted production shell with fake effect executors and stable logical IDs;
2. record every accepted normalized ingress value in mutex/serializer order;
3. replay the canonical trace into fresh isolated campaign/runtime/supervisor states; and
4. after every ingress, compare the committing authority's projected logical state and ordered outputs,
   then compare the complete composed state, overlap facts, admission state, election/commit decisions,
   terminal, or invariant diagnostic at quiescent barriers and terminal settlement.

Projection strips shell-only channels, mutexes, callbacks, native handles, and notification timing.
A second check should execute the same scripted observations directly against the pure core and through
the shell, proving that the shell only linearizes, records, and dispatches. That is stronger and smaller
than comparing final results alone: it detects reordered effects and transient obligation divergence at
the exact step where production and replay separate.

## Deliberately deferred

This evidence does not choose JSON, CBOR, protobuf, custom text, a versioning scheme, or any other
durable trace codec. #55 explicitly defers durable replay artifacts, and the parent map keeps a durable
trace/replay format out of scope. Parser errors and unknown encoded tags therefore do not belong to the
first contract at all; typed malformed normalized values remain the negative-contract input.
