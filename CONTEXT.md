# Managed Mutation Execution

Ooze evaluates source mutations through supervised test-command executions. This language distinguishes mutation evidence from the infrastructure required to obtain it safely.

## Campaign

**Campaign**:
One invocation of Ooze that attempts to establish and evaluate one fixed repository snapshot and mutant catalogue. A campaign either produces one trustworthy mutation score or produces no score.
_Avoid_: Run, release

**Process runtime**:
The process-local coordination authority shared by every Ooze campaign in one Go process. It owns campaign registration, admission, start and terminal linearization, and runtime-wide emergency settlement; after a fatal epoch it remains closed for the life of the process.
_Avoid_: Global lock, campaign scheduler

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
The unmutated attempt that establishes whether a non-empty campaign may admit mutants and supplies observations for automatic execution policy.
_Avoid_: Dry run, control

**Execution profile**:
The child CPU allocation fixed by the campaign mode and preserved across its baseline, primary attempts, and confirmations. Automatic campaigns use one child CPU; serial campaigns use Ooze's full detected capacity.
_Avoid_: Worker count, confirmation override

**Confirmation**:
A fresh, process-local exclusive attempt that resolves one provisional mutant result after all competing attempts have drained.
_Avoid_: Retry, rerun

**Confirmation queue**:
The stable, finite ordering of provisional mutants awaiting confirmation while their campaign remains paused.
_Avoid_: Retry batch, recovery queue

**Serial attempt**:
A primary attempt that runs process-local exclusively with Ooze's full owned CPU capacity. Separate campaigns may be interleaved between serial attempts, but never overlap one.
_Avoid_: Serial campaign, campaign lock

**Admission**:
Capacity reserved by Ooze's process-local policy for an attempt to proceed toward start commitment. Admission is either shared with bounded peers or exclusive after every peer has drained; it does not itself authorize launch, and waiting is ordinary scheduling rather than resource pressure.
_Avoid_: Worker slot, semaphore

**Start commitment**:
The process runtime's accepted authorization for one prepared attempt generation to begin external execution. It creates an execution-domain obligation before launch, so later admission closure cannot reinterpret that attempt as unstarted.
_Avoid_: Started callback, launch acknowledgement

## Supervision

**Execution domain**:
The supervised operating-system process boundary owned by one attempt, including the descendants covered by the platform contract.
_Avoid_: Process, process group, process tree

**Execution-domain obligation**:
The runtime ownership created by an accepted start commitment. It resolves as a proven pre-release launch failure or becomes an owned execution domain until authoritative drainage.
_Avoid_: PID reservation, started process

**Process fuse**:
Ooze's automatic execution-domain guard against runaway descendant activity.
_Avoid_: Process limit, runaway timeout

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
The first attempt-local deadline or process-fuse observation for a primary mutant attempt. It cannot affect the mutation score until confirmation.
_Avoid_: Timeout result, runaway result

**Attributable outcome**:
A mutant result supported by a passing baseline, verified domain drainage, and no uncertainty in the evidence used to classify it.
_Avoid_: Test result, command result

**Infrastructure uncertainty**:
Evidence that execution conditions, supervision, cleanup, or attribution are not trustworthy enough to produce a mutation score.
_Avoid_: Killed mutant, test failure
