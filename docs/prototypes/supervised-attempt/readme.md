# Supervised attempt prototype

Throwaway artifacts backing [#61, Define the supervised attempt
contract](https://github.com/gtramontina/ooze/issues/61). The ticket is not resolved. This revision
replaces the first sketch after adversarial review invalidated several of its public seams, timing
rules, and measurements.

The module is nested so it does not enter Ooze's own package graph or mutation catalogue:

```sh
devbox run -- go -C docs/prototypes/supervised-attempt test ./...
```

The suite is deterministic: it does not sleep, start a process, inspect the host, or read the wall
clock.

## Contract shape

The caller sees one concrete supervisor and one opaque owned-attempt capability:

```go
launched := supervisor.Launch(spec) // synchronous obligation classification

switch launched := launched.(type) {
case attempt.Owned:
	go func() {
		terminal := launched.Attempt.Wait()
		results <- terminal
	}()
	// On campaign stop:
	launched.Attempt.Stop(attempt.StopRequest{At: stopAt, DrainBy: drainBy})

case attempt.NotReleased:
	// Target code provably never ran. Resource exhaustion is one typed kind.

case attempt.LaunchUnconfirmed:
	// Fatal: ownership was not proved before LaunchBy. The supervisor retains
	// the pending native operation and adopts any late successful start.
}

// Fatal campaign cleanup covers both owned and still-prospective obligations.
settlement := supervisor.EmergencyDrain(attempt.EmergencyRequest{
	At: emergencyAt, DrainBy: emergencyDrainBy,
})
```

That is the complete public lifecycle surface:

- `Supervisor.Launch(Spec)`
- `OwnedAttempt.Stop(StopRequest)`
- `OwnedAttempt.Wait()`
- `Supervisor.EmergencyDrain(EmergencyRequest)`

The caller cannot signal, enumerate, release, or obtain native domain identity. It owns any goroutine
used to wait concurrently. This replaces `Laboratory.Test`'s future-returning orchestration;
`internal/future` is deleted when the production integration lands in [#67](https://github.com/gtramontina/ooze/issues/67).

`Launch` runs only inside the process runtime's accepted `StartCommitted` linearization. That private
step validates #62's matching grant and open start gate, allocates the private generation, and records
the prospective obligation before native work. Fatal closure or missing authorization rejects the
commitment and reaches no native launch; #62 owns the exact broker/token representation.

`Stop` and `Wait` are safe to call concurrently. `Wait` is idempotent and returns the same immutable
terminal while executing the underlying wait once. Invalid stop instants fail before native work.
Valid stop facts received before intent participate by logical `At`; same-instant stops use the
earliest `DrainBy`, and facts delivered after intent do not rewrite it.
Stop admission is sealed either at the native domain-release linearization point before release, or
immediately before `DrainUnconfirmed` transfers exclusive residual custody to the Supervisor without
release. A stop accepted first reaches the reducer; one arriving after either cut performs no native
work.

`Spec` contains attempt identity, command, directory, environment, profile, and an already-resolved
positive command deadline. Baseline and mutation policy resolve that deadline upstream. Launch,
drain, and census policy are supervisor-private.

## Private state machine

The implementation keeps a pure timestamped reducer behind the concrete module. Production records
native facts; simulation injects the same facts and logical instants. Each fact carries an attempt
generation, so stale native observations cannot affect a reused logical slot.

The first valid terminal fact wins. Priority is only a tie-break at the same logical instant:

1. a fuse sample for the matching generation which also proves the root was live;
2. an already-observable root exit;
3. a required running-observation failure, including census or root-wait failure;
4. the command deadline, inclusive at `releasedAt + Deadline`;
5. a stop request.

Production must stamp a native observation after the native operation returns. It must recheck an
observable exit at the deadline boundary. Once intent is latched, later exit, stop, or census facts do
not rewrite it and fuse sampling stops. `CommandDuration` ends at that intent instant, not after
cleanup.

Stop has two instants: `At` selects the terminal fact and `DrainBy` bounds cleanup. Local and emergency
drain are fresh epochs of one private drain-policy class. Launch progress is the other policy class.
The contract therefore has three resolved absolute deadlines (`LaunchBy`, local `DrainBy`, emergency
`DrainBy`) but only two kinds of policy. #61 fixes no numeric default and exposes no public timeout
knob; resolved instants are trace input for exact replay.

One emergency epoch closes the process runtime permanently and dispatches the same `At` and `DrainBy`
concurrently to every current owned or prospective obligation. A late successful launch is adopted
into that same epoch. The stable result is `SweepDrained` or an ordered immutable
`SweepUnconfirmed` set whose entries distinguish prospective-unresolved from owned-undrained.
It settles as soon as every obligation resolves or inclusively at `DrainBy`; later custody work may
close or adopt residuals but cannot rewrite the delivered settlement. Resolution must be accepted
strictly before `DrainBy` to produce `SweepDrained`; a closure or adoption at equality is applied only
after the residual snapshot is latched.

Every deadline, fuse, stop, or running-observation fault begins with forced domain termination. A natural
root exit first asks for authoritative emptiness and forces termination only if a residual exists or
emptiness is unobservable. There is no graceful phase. Every native drain step receives only the
remaining time to the same absolute `DrainBy`; no fixed rounds, polling interval, or backoff is
semantic policy. Expiry is inclusive: an empty observation stamped exactly at `DrainBy` is too late
for the local result. Expiry never proves emptiness, and an observed residual is forced even when its
observation arrives at or after the bound.
Every drain, output, and release effect/completion carries the private generation and a monotonic action token.
Stale, duplicate, wrong-kind, or wrong-generation observations are invariant failures and cannot
authorize output capture or domain release.

## Observations and retained evidence

An owned attempt ends as exactly one of:

- `Settled`, with root exit status;
- `Tripped`, with either a fuse trip, automatic deadline trip, or serial deadline trip;
- `Stopped`;
- `Infrastructure`, after an authoritative empty proof exposes a real supervision failure;
- `DrainUnconfirmed`, when authoritative emptiness is not proved by `DrainBy`.

Only `FuseTrip` carries its trip count. An automatic deadline carries the highest valid running-phase
count when one was observed; absence remains explicit rather than becoming a fabricated zero. Serial
deadlines and other terminals carry no invented count.

Every owned terminal carries the resolved command deadline, launch duration, command duration, and
which command bound fired. It also carries one immutable contiguous prefix of merged stdout/stderr:
the supervisor records one cutoff, excludes later appends, and states whether the prefix was read
through that cutoff and whether drainage made it final. Output capture is a causal phase after drain
settlement and before release; an output error retains the successfully read prefix. Output
is written to a private file rather than a pipe and the path is never exposed. Retaining mutant
output is deliberate: [#75](https://github.com/gtramontina/ooze/issues/75)
may use its signature, together with which Ooze bound fired and command duration, to distinguish an
inner command timeout. #61 does not perform that classification.

The same common evidence retains wait, running-census, termination-control, drain-census, output,
and release failures independently, including on `DrainUnconfirmed`. `Infrastructure.Cause` selects
one stable presentation cause without discarding the other axes. If wait and running census fail at
the same instant, wait is the primary cause and both remain recorded.

## Launch closure and resource exhaustion

`LaunchResourceExhausted` is a closed launch result, not an owned terminal. It is valid only when the
native adapter proves the target was not released and any created suspended launcher has been
authoritatively closed. Classification is a predicate over platform, exact primary operation,
release stage, typed code, and closure proof; cleanup diagnostics are never searched after joining.
The initial false-negative-biased tables are:

- Linux: `EMFILE`/`ENFILE` for internal descriptor acquisition; all four Unix codes for
  `launcher.Start`; `EAGAIN`/`ENOMEM` for the typed target-exec handshake.
- Darwin: `EMFILE`/`ENFILE` for internal descriptors; all four Unix codes for `launcher.Start`;
  `ENOMEM`/`EMFILE`/`ENFILE` for `kqueue`; `ENOMEM` for NOTE_EXIT registration and typed target
  `execve` failure.
- Windows: `ERROR_TOO_MANY_OPEN_FILES` (4), `ERROR_NOT_ENOUGH_MEMORY` (8), `ERROR_OUTOFMEMORY` (14),
  `ERROR_NO_PROC_SLOTS` (89), `ERROR_NO_SYSTEM_RESOURCES` (1450), and `ERROR_COMMITMENT_LIMIT` (1455)
  at internal-output, suspended-start, or pre-release Job/thread-containment operations.

A failed Windows `ResumeThread` is release-unknown; success with prior suspend count one is the
release cut. The same allowed code at an ineligible operation, unknown/post-release stage, joined
cleanup sibling, or unclosed suspended process cannot take this branch. Ambiguous Windows quota and
thread families remain ordinary `LaunchFailed` values until an identity-valid artifact justifies a
contract expansion.

A launch-progress deadline cannot cancel an in-flight `Start`/`CreateProcess` syscall. On expiry the
supervisor reports `LaunchUnconfirmed`, retains the prospective obligation, and later adopts and
force-drains a successful physical start. It never manufactures a not-released proof.

At `LaunchBy`, the launch sequencer takes one serialized snapshot of the fully classified completion
cell. A released-inside-containment or proven-not-released-and-closed completion already published
with `At <= LaunchBy` wins; a nil snapshot latches `LaunchUnconfirmed`. Notifications are wakeups,
not semantic ordering. A completion stamped at the same instant but published after the nil snapshot
is late: it closes or adopts the same generation without rewriting the public result. The trace
records the boundary event and its completion-or-nil snapshot for deterministic replay.
If a stopped/suspended native identity becomes controllable before final classification, the same
sequencer records it immediately. Expiry or emergency closure then revokes target release and closes
that identity; only a target whose release already became unavoidable is adopted and force-drained.
Every launch event carries the private generation; stale-generation facts and release after explicit
revocation are invariant failures. Emergency closure uses the same serialized completion snapshot
and revocation transition as expiry; its earlier `At` becomes the monotonic floor for later custody
events, so a pending launch can close before `LaunchBy` without time moving backward. A completion
already in that snapshot still returns its synchronous launch result; an owned result is atomically
joined to the emergency drain.

The unsupported fallback (`!windows && !darwin && !linux`) cannot meet this contract: it has neither
containment nor an authoritative emptiness proof. Supervisor construction therefore fails before
admission or native launch on those platforms. Build tags select mechanics, not different semantic
outcomes.

## Fuse and drainage instruments

The automatic-profile live-root fuse remains #60's parent-identity walk with ceiling 64. The process
group is not unioned into that fuse. This preserves #60's published margin decision; it is not a
claim that the walk is the most complete imaginable census.

The private nominal fuse cadence is 50 ms. It is an argued engineering choice, not a measured upper
bound on observation delay, process growth, or portable resource safety. A scheduled tick may slip.
The prototype promises neither a public cadence knob nor a shared-sampler architecture; a production
adapter may privately amortize platform reads without changing the contract.

Drainage is a different question. The portable obligation is: **do not destroy reachable identity
before acting on it**.

- Darwin must begin enumeration before destructive termination, prevent reachable processes from
  creating or destroying identities, and terminate the union of live group members and the
  descendant closure reachable from those members. Each escapee is captured as PID plus birth
  identity and revalidated immediately before every by-PID stop or kill; mismatch or an unreadable
  identity is never signalled and cannot prove drainage. Escapees must be stopped and killed by
  identity as well as the group. The closure converges against one absolute drain deadline.
- Linux's subreaper adoption and repeated depth-one sweeps preserve identity without pretending to
  offer a portable transitive parent walk.
- Windows Job Objects provide transitive accounting and containment without a Unix freeze analogue.

A process group is a signalling containment handle. It is not the live-root fuse census, and group
emptiness alone is never proof that the whole attempt domain is empty. In drainage, group membership
may be one platform-native identity seed. In particular, `kill(-pgid, 0) == ESRCH` does not prove that
an escapee is gone.

There remains an unavoidable race before the freeze has reached every identity, including a process
mid-fork or an escapee that leaves before enumeration, plus Darwin's check-to-signal PID-reuse window
because it has no atomic pidfd-like signal handle. Those shapes are not reliably reachable and remain
the macOS platform limit; the convergence statement begins only after the captured set is frozen.

The committed [#66](https://github.com/gtramontina/ooze/issues/66) fixture where the current sweep
orphans an escapee behind a live group member should turn green when the Darwin closure is captured
before killing. A descendant that escapes directly from the root before enumeration remains the
recorded macOS platform limit; #61 does not turn probabilistic sampling into a promise.

## Research and probes

The industry comparison in
[`docs/research/stop-escalation-industry.md`](../../research/stop-escalation-industry.md) finds that
immediate forced termination or worker recycling is dominant for timed-out mutant work, but not
universal. It supports the policy direction, not a mandate.

The deadline comparison in
[`docs/research/supervision-bounds-industry.md`](../../research/supervision-bounds-industry.md) finds
no transferable numeric default. Comparable systems separate launch/readiness, execution, graceful
stop, and post-force observation; durable daemons can retain custody after returning an error, while
an in-process library cannot. #61 copies the separation and failure semantics, not their numbers.

The operation/code/closure evidence for the portable launch-pressure branch is recorded in
[`docs/research/launch-resource-exhaustion.md`](../../research/launch-resource-exhaustion.md). Its
tables are conservative normalization policy over sourced platform errors, not measurements that
reducing concurrency will necessarily recover each failure.

The probes under `probes/` remain measurement artifacts rather than contract code:

- `census` and `postexit` demonstrate that parent walks and group membership observe different
  descendant shapes, and that a root-seeded walk loses identities after root exit.
- `peaks` can compare the live-root walk with a union instrument. No raw result artifact with command,
  sample count, statistic, tree identity, and identity proof is committed, so this revision cites no
  number from it. Any future run must state which tree was measured because #66's fixtures change the
  working-tree process shape.
- `cadence` shows the order of magnitude of local Darwin census cost and process creation; it does not
  establish a portable 50 ms safety guarantee.
- `drain` has no identity- and provenance-valid result artifact. No drain-round count is cited.

## Corrections and retractions

These remain explicit because silently polishing the record would make later review unreliable.

- Resource exhaustion was first published as an `Infrastructure` terminal. Retracted: it is a
  proven-not-released `LaunchResourceExhausted` result.
- The public `Platform`/`Snapshot`/`Domain` seam and one-shared-sampler architecture were published,
  then retracted. Native mechanics and any amortization are private.
- A 30 s launch bound, 5 s drain bound, 1 ms drain polling, 32 tight rounds, and exponential backoff
  were published without transferable evidence. Retracted: #61 requires finite resolved absolute
  deadlines but chooses no values or retry algorithm.
- Bundle-wide `fuse > exit > census failure > deadline > stop` precedence was published, then
  retracted. Earliest valid logical fact wins; that order applies only to exact ties.
- A stop channel and bare `StopBy` conflated the stop instant with the cleanup deadline. Retracted in
  favor of `StopRequest{At, DrainBy}`.
- Output was exposed as a caller-supplied path and mutant output was at risk of being discarded.
  Retracted in favor of private storage and immutable merged bytes on every owned terminal.
- Elapsed time included drainage and peak counts continued during cleanup. Retracted: launch and
  command durations are separate, command duration ends at intent, and running sampling stops there.
- Exact 50 ms overshoot, headroom, and CPU-percentage claims were not established by a probe that
  identified and sampled the production workload. Retracted. The cadence remains an argued private
  constant only.
- Earlier census timings presented warm-loop minima as medians and mixed incompatible statistics.
  No exact cost figure is used by this contract.
- Three unsourced figures are intentionally absent: drain fixpoint rounds, processes appearing
  between two samples, and `NOTE_EXIT` delivery latency. No valid citable artifact exists for them.
