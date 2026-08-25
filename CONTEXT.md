# Managed Mutation Execution

Ooze evaluates source mutations through supervised test-command executions. This language distinguishes mutation evidence from the infrastructure required to obtain it safely.

## Campaign

**Campaign**:
One invocation of Ooze that attempts to establish and evaluate one fixed repository snapshot and mutant catalogue. A campaign either produces one trustworthy mutation score or produces no score.
_Avoid_: Run, release

**Process runtime**:
The process-local coordination authority shared by every Ooze campaign in one Go process. It owns campaign registration, admission, start and terminal linearization, and runtime-wide emergency settlement; after a fatal epoch it remains closed for the life of the process.
_Avoid_: Global lock, campaign scheduler

**Detected capacity**:
The positive process-local Go-concurrency snapshot that supplies the full-automatic admission bound and the execution-profile value of serial attempts. It remains fixed for one process runtime and is not a claim that Ooze owns that many CPUs.
_Avoid_: Worker count, CPU count

**Repository snapshot**:
The immutable source state from which the baseline and every mutant repository in a campaign are materialized.
_Avoid_: Working tree, repository copy

**Mutant catalogue**:
The stable, ordered collection of source mutations discovered from a repository snapshot.
_Avoid_: Mutation list, infected files

**Attempt**:
One execution of the configured test command against a materialized repository. An attempt is a baseline, a primary mutant attempt, or a confirmation.
_Avoid_: Test, run, worker

**Baseline**:
The unmutated attempt that establishes whether a non-empty campaign may admit mutants.
_Avoid_: Dry run, control

**Execution profile**:
The inherited, cooperative Go runtime setting fixed by the campaign mode and preserved across its baseline, primary attempts, and confirmations. Automatic attempt roots receive `GOMAXPROCS=1`; serial attempt roots receive the detected-capacity value. Descendants may override or ignore it, so it is not an aggregate CPU or process-tree quota.
_Avoid_: CPU allocation, subtree quota, confirmation override

**Confirmation**:
A fresh, process-local exclusive attempt that resolves a primary deadline with recorded peer overlap after all competing attempts have drained. A primary deadline without recorded peer overlap needs no confirmation. Fuse trips never make a primary provisional or trigger confirmation. An automatic confirmation may independently trip its fuse; after authoritative drainage, that observation is directly attributable `Runaway` under the ordinary fuse rule.
_Avoid_: Retry, rerun

**Confirmation queue**:
The stable, finite ordering of provisional mutants awaiting confirmation while their campaign remains paused.
_Avoid_: Retry batch, recovery queue

**Serial attempt**:
A primary attempt that runs process-local exclusively and receives the full detected-capacity cooperative execution profile. Separate campaigns may be interleaved between serial attempts, but never overlap one.
_Avoid_: Serial campaign, campaign lock

**Admission**:
Capacity reserved by Ooze's process-local policy for an attempt to proceed toward start commitment. Admission is either shared with bounded peers or exclusive after every peer has drained; it does not itself authorize launch, and waiting is ordinary scheduling rather than resource pressure.
_Avoid_: Worker slot, semaphore

**Recorded peer overlap**:
The fact latched for a primary attempt once its live execution-domain obligation coexists with another Ooze-owned attempt's live obligation after both start commitments were accepted. It remains true if the peer drains before the primary trips and determines whether that primary's deadline requires confirmation; it is not a resource-usage measurement.
_Avoid_: Active at trip, shared mode

**Full automatic admission**:
The initial automatic admission state, in which at most the detected capacity of shared automatic primary attempts may overlap across every campaign in the process runtime.
_Avoid_: Ramp, calibrated capacity

**Single-admission automatic**:
The irreversible automatic fallback state, in which at most one automatic primary attempt may run in the process runtime and the automatic execution profile remains unchanged. It is not a serial attempt or an inferred resource capacity.
_Avoid_: Serial fallback, learned capacity

**Capacity pressure**:
A trustworthy hard resource-exhaustion observation from shared automatic execution, or a primary deadline with recorded peer overlap that disappears under exclusive confirmation. It moves the process runtime from full automatic admission to single-admission automatic but never determines a mutant outcome or repairs uncertain evidence.
_Avoid_: Host load, slow test, killed mutant

**Start commitment**:
The process runtime's accepted authorization for one prepared attempt generation to begin external execution. It creates an execution-domain obligation before launch, so later admission closure cannot reinterpret that attempt as unstarted.
_Avoid_: Started callback, launch acknowledgement

## Simulation

**Deterministic simulation**:
An evaluation scenario whose immutable definition and accepted fact order fully determine every campaign, process-runtime, broker, and supervised-attempt decision.
_Avoid_: Fake engine, model implementation

**Simulation trace**:
An immutable simulation definition plus a stable ordered sequence of normalized facts sufficient to replay every decision. Effects, wakeups, and fuzz choices are not trace authority.
_Avoid_: Event log, fuzz corpus, transcript

**Fuzz choice stream**:
Ephemeral bytes that select from the deterministic simulation's currently enabled moves during discovery. It may be retained as provenance but is never the replay authority.
_Avoid_: Simulation trace, replay seed

## Supervision

**Execution domain**:
The supervised operating-system process boundary owned by one attempt, including the descendants covered by the platform contract.
_Avoid_: Process, process group, process tree

**Execution-domain obligation**:
The runtime ownership created by an accepted start commitment. It resolves as a proven pre-release launch failure or becomes an owned execution domain until authoritative drainage.
_Avoid_: PID reservation, started process

**Process fuse**:
Ooze's automatic guard against runaway descendant creation. It compares the count of an automatic attempt root's live descendants, walked by parent identity, against one fixed ceiling. It exists only under the automatic execution profile, where that count is contention-independent, so a trip is directly attributable after drainage and never capacity pressure. Serial attempts carry no fuse. Process-group membership is a signalling containment handle, not the live-root fuse census; drainage may use live group membership as a platform-native identity seed, but group emptiness alone never proves that the execution domain is drained.
_Avoid_: Process limit, runaway timeout, host load

**Drained domain**:
An execution domain authoritatively observed to contain no process that can execute or create descendants within the platform contract.
_Avoid_: Root exited, kill requested

**Drain unconfirmed**:
A campaign-local fatal seed produced when bounded termination cannot establish that an execution-domain obligation is resolved. It closes the process runtime and starts one runtime-wide emergency sweep; it is not itself a terminal campaign result.
_Avoid_: Cleanup unconfirmed, timeout result

**Cleanup unconfirmed**:
A fatal containment fault produced only after a containment-only process-runtime-wide sweep leaves a non-empty set of unresolved execution-domain obligations.
_Avoid_: Lost, cleanup timeout

**Artifact residue**:
A repository artifact, such as a temporary attempt workspace or snapshot backing store, that could not be released after every associated execution domain was proven drained. It is infrastructure residue, not a possible running process.
_Avoid_: Cleanup unconfirmed, leaked process

## Outcomes

**Provisional trip**:
The first primary-mutant deadline observation from an attempt with recorded peer overlap. A deadline is the only observation that can be provisional, because a fuse trip is contention-independent and therefore directly attributable. It cannot affect the mutation score until exclusive confirmation; a deadline without recorded peer overlap becomes directly attributable after authoritative drainage.
_Avoid_: Timeout result, runaway result

**Attributable outcome**:
A mutant result supported by a passing baseline, verified domain drainage, and no uncertainty in the evidence used to classify it.
_Avoid_: Test result, command result

**Infrastructure uncertainty**:
Evidence that execution conditions, supervision, cleanup, or attribution are not trustworthy enough to produce a mutation score.
_Avoid_: Killed mutant, test failure
