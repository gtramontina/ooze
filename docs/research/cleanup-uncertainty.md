# Cleanup uncertainty: what may Ooze do when a process domain will not drain?

_Research date: 2026-08-21. This is a design input, not an implementation specification._

> **Resolution note:** The later [campaign transition-algebra decision](https://github.com/gtramontina/ooze/issues/57)
> makes cleanup escalation process-runtime-wide. This report retains the primary-source research and
> rejected alternatives, while the policy language below reflects that final resolution.

## Question

After Ooze has stopped admission, requested termination, escalated to forced termination, and waited for a bounded cleanup interval, what should happen if it still cannot prove that an execution domain is empty?

The short answer is: **do not convert that uncertainty into an ordinary `Lost -> Aborted` campaign completion.** An expired cleanup bound proves that cleanup is overdue; it does not prove that cleanup happened.

The smallest honest policy for the current architecture is:

1. treat a campaign-local failure to drain as `DrainUnconfirmed`, a fatal seed rather than a result;
2. atomically close the process runtime to registration, admission, start commitment, and new score
   commitment, invalidating every still-live local campaign;
3. perform one bounded, concurrent escalation over every prospective and owned execution-domain
   obligation in that runtime;
4. accept only a proven pre-release launch failure or authoritative `EmptyVerified` observation as
   release;
5. in a containment-only epoch, if the runtime-wide residual is empty, force every affected campaign
   to unscored `Aborted` without a containment panic;
6. in a containment-only epoch, if the residual is non-empty, produce exactly one
   `Fail(CleanupUnconfirmed)` carrying its stable union;
7. if an `InvariantViolation` joins the epoch, let its first ingress-ordered occurrence dominate:
   attach every other cause and any residual to one deterministic diagnostic, then re-panic once after
   the same emergency settlement instead of emitting a separate `Fail`; and
8. keep panic and fail-stop policy in the elected `testing.T` integration, outside the pure reducers.

There must be no new test commands after the fault. A future durable guardian could make a bounded error return safe, but only after it has accepted custody of a stable, non-escapable domain capability. Ooze does not currently have that portable contract, and it should not be invented speculatively.

## The distinction the model must preserve

These are not equivalent observations:

- **Termination requested:** the supervisor asked the OS to stop processes.
- **Termination committed:** the OS accepted an uncatchable whole-domain termination operation.
- **Empty verified:** the supervisor has authoritative evidence that no process in the supported execution domain can execute or fork.
- **Released:** the domain is empty and Ooze has relinquished its handles and other capabilities.
- **Custody transferred:** another durable owner has atomically accepted the obligation and can itself prove emptiness.
- **Drain unconfirmed:** a campaign-local supervisor lacks an emptiness proof. This is a fatal seed;
  it does not yet determine the runtime-wide terminal result.
- **Cleanup unconfirmed:** after the process-runtime sweep, at least one prospective launch or owned
  domain remains unresolved. This does not assert that a process is definitely still runnable; it
  asserts that safe continuation has not been established.

`Lost` is therefore a dangerous name if it removes an entry from the resource ledger. Losing knowledge or a handle makes the obligation more serious, not complete.

“Empty” should mean that no process can still execute or create descendants within the supported domain. It need not mean that every kernel accounting object has been physically reclaimed. Linux, for example, may retain a deleted cgroup as a dying kernel object after its `populated` value has reached zero; that delayed OS reclamation is different from possible runnable residue ([Linux cgroup v2 `cgroup.events` and `cgroup.stat`](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)).

## Current Ooze constraints

Ooze is embedded in a Go test binary: public [`Release`](../../release.go) accepts `*testing.T` and returns no error. Its [`CMDTestRunner`](../../internal/cmdtestrunner/cmdtestrunner.go) already panics on a supervision error. In an ordinary Go test, an unrecovered panic is process-fatal; the testing package deliberately re-panics from its test runner so the process terminates ([Go `testing.tRunner` source](https://go.dev/src/testing/testing.go)). By contrast, `T.FailNow` terminates only the current test goroutine and permits later tests to run; it does not stop other goroutines ([Go `testing.T.FailNow`](https://pkg.go.dev/testing#T.FailNow)).

The current native supervision mechanisms are materially different:

- [Windows](../../internal/cmdtestrunner/process_tree_windows.go) creates the root suspended, assigns it to a Job Object, then resumes it.
- [Linux](../../internal/cmdtestrunner/process_tree_linux.go) re-executes an Ooze helper as a child subreaper and repeatedly kills/reaps adopted descendants.
- [macOS](../../internal/cmdtestrunner/process_tree_darwin.go) launches the root in a new process group, observes root exit with kqueue, then kills and scans that group.
- [Other Unix systems](../../internal/cmdtestrunner/process_tree_unix.go) retain root-process-only behavior.

Each of the three primary adapters currently has a five-second termination bound. A timeout becomes a supervision error and therefore a panic. Replacing that with an ordinary `Aborted` return would relax today's fail-stop behavior while allowing possible residue to continue.

## What the operating systems can actually prove

### Windows: a strong native domain, with an asynchronous drain

A Windows Job Object is the closest current implementation to a stable execution-domain capability. Once assigned, a process cannot leave its job; children created normally with `CreateProcess` join by default. Breakaway flags must remain disabled. Microsoft documents an exception for children created through `Win32_Process.Create`, and nested-job behavior must still be respected ([Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)).

`TerminateJobObject` applies termination to every associated process and cannot be caught or postponed by those processes ([`TerminateJobObject`](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-terminatejobobject)). That successful call is still not the drain barrier: `TerminateProcess` is asynchronous, and pending kernel I/O can delay final termination ([`TerminateProcess`](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-terminateprocess)). Ooze needs an observed zero active-process count, after closing its retained per-process handles, rather than treating a successful terminate call as empty ([`JOBOBJECT_BASIC_ACCOUNTING_INFORMATION`](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_basic_accounting_information)). Job completion-port messages are useful wake-ups but are not a substitute for querying the authoritative state ([job completion-port messages](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_associate_completion_port)).

`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` makes Windows unusually crash-safe: closing the last Job handle kills the associated hierarchy ([Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)). A durable guardian could receive a duplicated Job handle, but real custody transfer would require the guardian to become the sole owner; otherwise “last handle closed” has not been established ([`DuplicateHandle`](https://learn.microsoft.com/en-us/windows/win32/api/handleapi/nf-handleapi-duplicatehandle)).

### Linux: strong with delegated cgroup v2; weaker with ancestry alone

The Linux kernel offers the desired primitive through a delegated cgroup-v2 subtree. Writing `1` to `cgroup.kill` sends `SIGKILL` to the entire subtree and explicitly handles concurrent forks and migrations. `cgroup.events` reports `populated=0` only when the cgroup and all descendants contain no live processes. The write itself is not documented as a synchronous emptiness barrier, so the proof is the subsequent `populated=0` observation ([Linux cgroup v2 core interface](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)).

That primitive is not automatically available to an unprivileged library. An administrator or service manager must delegate a subtree; the kernel's delegation rules then constrain process migration outside it ([Linux cgroup v2 delegation model](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)). Ooze must not assume that hosted CI or a developer machine grants this access.

Without a delegated cgroup, process groups and ancestry tracking do not form an inescapable domain. Sending a signal to a process group targets its current members; successful `kill` only establishes that at least one signal was delivered ([Linux `kill(2)`](https://man7.org/linux/man-pages/man2/kill.2.html)). A child subreaper receives orphaned descendants so that it can wait for them, but it does not itself constrain or terminate them ([`PR_SET_CHILD_SUBREAPER`](https://man7.org/linux/man-pages/man2/PR_SET_CHILD_SUBREAPER.2const.html)). Parent-death signals do not recursively solve this because the setting is cleared in forked children ([`PR_SET_PDEATHSIG`](https://man7.org/linux/man-pages/man2/PR_SET_PDEATHSIG.2const.html)).

A guardian with delegated cgroup access could outlive Ooze, kill the subtree, and wait for `populated=0`. Linux has no equivalent of “kill this cgroup when the last file descriptor closes,” however, so guardian failure still needs an owner above it.

### macOS: no public, unprivileged recursive containment primitive was found

`killpg` signals the members of one process group but does not wait for them ([Apple `killpg(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/killpg.2.html)). A descendant may call `setsid`, becoming the sole member and leader of a new session and process group, so an original process-group identifier is not an inescapable causal boundary ([Apple `setsid(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/setsid.2.html)).

kqueue's `EVFILT_PROC` can authoritatively observe a specified PID's exit, but it is not a recursive process-domain barrier ([Apple `kqueue(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/kqueue.2.html)). Apple's current XNU header says that the historical `NOTE_TRACK`, `NOTE_TRACKERR`, and `NOTE_CHILD` process-tracking flags have been unsupported since macOS 10.5 ([XNU `event.h`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/sys/event.h)). libproc child/group enumeration is snapshot-based and its Apple header describes those interfaces as private and subject to change ([XNU `libproc.h`](https://github.com/apple-oss-distributions/xnu/blob/main/libsyscall/wrappers/libproc/libproc.h)).

Therefore, “root reaped and original process group empty” proves the controlled group is empty; it does not prove that every causal descendant is gone if the command is allowed to daemonize or create a new session. A helper that outlives Ooze can keep killing the original group, but it cannot manufacture the missing containment boundary after a timeout.

## What established supervisors do

The common pattern is not “the timeout elapsed, therefore forget the resource.” It is either durable ownership, refusal to start replacement work, or fail-stop of the enclosing scope.

- systemd defaults to killing a unit's whole control group. It explicitly discourages killing only the main process or no processes because residue escapes lifecycle and resource management. After the graceful bound it sends `SIGKILL`; when that escalation is disabled and prior processes remain, it refuses to restart the service ([systemd `KillMode` and `SendSIGKILL`](https://github.com/systemd/systemd/blob/main/man/systemd.kill.xml), [`TimeoutStopSec`](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml)).
- Docker performs a graceful stop, then `SIGKILL` after the bound ([Docker stop semantics](https://docs.docker.com/reference/cli/docker/container/stop/)). Moby waits for an exit event after escalation and reports a failure if none arrives rather than committing “stopped”; this is viable because the daemon remains the durable owner ([Moby stop path](https://github.com/moby/moby/blob/master/daemon/stop.go), [Moby kill path](https://github.com/moby/moby/blob/master/daemon/kill.go)).
- Bazel bounds graceful server shutdown, escalates, waits again, and terminates the client with a local-environmental error when server death still cannot be confirmed rather than starting a replacement ([Bazel POSIX server shutdown](https://github.com/bazelbuild/bazel/blob/master/src/main/cpp/blaze_util_posix.cc), [Bazel shutdown bounds](https://github.com/bazelbuild/bazel/blob/master/src/main/cpp/blaze_util.cc)). Its process wrapper also kills the child and descendants on normal exit or timeout ([Bazel `process-wrapper`](https://github.com/bazelbuild/bazel/blob/master/src/main/tools/process-wrapper.cc)).
- Kubernetes force deletion is the useful counterexample: it may remove the API object without confirmation and explicitly warns that the workload may continue indefinitely. That trade is supported by a durable kubelet/control plane and is unsafe for identities that cannot overlap ([Kubernetes forced termination](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/)). Ooze has no equivalent durable control plane.

Two superficially permissive test-runner precedents do not establish that Ooze should continue:

- cargo-nextest may stop waiting when inherited stdout/stderr remains open and report a test as leaky. Its documentation says this detects only some leaks and cannot detect detached descendants that do not inherit those handles ([nextest leaky tests](https://www.nexte.st/docs/features/leaky-tests/)). That is a bounded output-capture policy, not a recursive emptiness proof. For actual test timeouts, nextest escalates from process-group termination to `SIGKILL`, and uses Job Objects on Windows ([nextest timeout handling](https://www.nexte.st/docs/features/slow-tests/)).
- Bazel's test specification permits the root exit code to remain authoritative in the presence of stray children, but the same specification forbids tests from creating process groups or sessions ([Bazel Test Encyclopedia](https://bazel.build/reference/test-encyclopedia)). Ooze currently accepts arbitrary commands through `WithTestCommand` and has not imposed that contract.

## Go primitives do not close the gap

`(*os.Process).Kill` targets one process and does not wait for exit; `Wait` is the reaping/exit observation. `Process.Release` merely releases Go's handle and does not terminate the process ([Go `os.Process`](https://pkg.go.dev/os#Process)).

`exec.Cmd.WaitDelay` is useful for bounding command cancellation and inherited-pipe hangs: it kills the direct child and closes pipes after the delay ([Go `os/exec.Cmd.WaitDelay`](https://pkg.go.dev/os/exec#Cmd.WaitDelay)). It neither supplies a recursive execution-domain guarantee nor turns a delayed kernel exit into an emptiness proof. Ooze still needs its platform supervisor and an explicit domain-empty observation.

`os.Exit` is not an appropriate library escape hatch. It ends the program immediately and skips deferred functions ([Go `os.Exit`](https://pkg.go.dev/os#Exit)); it also does not inherently kill Unix descendants. A panic is different: it runs defers, and the standard Go test runner normally propagates it to terminate the test binary. Panic is still an integration policy, not a reducer effect, and must be documented as a fatal containment response rather than used for ordinary test or mutant outcomes.

## Alternatives

| Policy after final bounded drain | Bounded caller return | Prevents unsafe continuation | Proves cleanup | Assessment for Ooze now |
| --- | ---: | ---: | ---: | --- |
| Return ordinary `Aborted` and discard the ledger entry | Yes | No | No | Reject. Score-honest but host-unsafe. |
| Wait forever | No | Yes | Eventually, if the OS ever reports exit | Reject. Recreates the CI/local hang this work is meant to remove. |
| `T.FailNow` or process-local broker poison | Yes | Partly | No | Insufficient alone. Other tests and non-Ooze goroutines continue; hidden global poison also complicates deterministic simulations and concurrent campaigns. |
| Call `os.Exit` | Process ends | Usually | No | Reject for a library: skips defers and does not guarantee descendant cleanup. |
| Close the process runtime and sweep every execution obligation; emit one fatal fault only if the residual remains non-empty | Normal return if the residual empties; fail-stop if non-empty | Yes, absent an explicit caller recovery | No absolute cross-platform proof | Recommended current boundary. An empty global residual instead forces unscored `Aborted` results without a containment panic; a joined invariant still re-panics after settlement. |
| Transfer a stable domain to a durable guardian, then return a typed fatal result | Yes | Yes | Guardian can later prove it | Sound future option, but only with a concrete custody protocol; not a first-slice feature. |
| Return on strong platforms, fail-stop on weak ones | Varies | Varies | Varies | Reject at campaign level. Platform adapters may differ internally, but score and safety semantics should not. |

## Recommended model

The reducer should distinguish normal completion from a fatal containment fault. One possible sealed shape is:

```go
type stepResult interface{ isStepResult() }

type continueWith struct {
	state state
}

// Requires every execution-domain obligation to be released.
type finishWith struct {
	outcome outcome
}

// CleanupUnconfirmed carries a statically non-empty residual-obligation set.
type failWith struct {
	fault fatalFault
}
```

The resource capability should have monotonic lifecycles resembling:

```text
StartCommitted -> ProspectiveDomain -> PreReleaseFailureProven -> Released
StartCommitted -> ProspectiveDomain -> Owned -> StopCommitted -> EmptyVerified -> Released
```

A future, concrete guardian may add:

```text
Owned -> StopCommitted -> CustodyTransferred
```

There should be no transition from `ProspectiveDomain`, `Owned`, or `StopCommitted` to a
ledger-removing `Lost` state. If the runtime-wide final wait expires, the fatal value retains a
`NonEmpty[ResidualExecutionObligation]` collection whose variants distinguish an unresolved
committed launch from an owned but undrained domain, plus the evidence needed for diagnosis.
`Completed`, `NoMutants`, and `Aborted` remain constructible only with an empty live-resource ledger;
`Aborted` may still record a settled workspace or snapshot artifact residue.

Operationally:

```text
local DrainUnconfirmed
  -> close the process runtime and invalidate every live campaign
  -> retain every accepted prospective launch
  -> continue every obligation from its existing termination progress
  -> dispatch every still-needed stop concurrently
  -> force-stop remaining domains at one fixed escalation instant
  -> wait only to the one absolute runtime-sweep deadline
  -> no joined InvariantViolation and every prospective launch resolved and every domain EmptyVerified:
       release and force unscored Aborted results, with no containment panic
  -> no joined InvariantViolation and anything unconfirmed:
       no score, no new commands, one globally aggregated fatal containment fault
  -> joined InvariantViolation:
       attach all causes and any residual to one diagnostic, then re-panic once after settlement
```

The bound should be absolute for each local drain epoch and for the process-runtime emergency epoch,
not multiplied serially by the number of active domains. Healthy execution pays no retry, probe,
control-run, or guardian cost. Only the exceptional path performs escalation.

For the current public integration, the pure process-runtime core aggregates residuals. In a
containment-only epoch with a non-empty residual, it delivers one `failWith` to the stably elected
campaign reducer so deterministic tests can inspect it. In an invariant-dominant epoch, it instead
returns the one deterministic invariant diagnostic with the causes and residual attached. The elected
`testing.T` adapter should first emit the consolidated diagnostics, then panic. Reducers themselves
should know nothing about panic or the Go test process. An explicit recovery by an embedding caller
would mean that caller accepts responsibility for an unsafe process; the runtime remains closed and
Ooze does not silently pretend normal reuse is supported.

## What a future guardian would have to guarantee

An external helper is not made safe merely by outliving `Release`. A valid handoff requires all of the following:

1. containment is established before the root can execute, or a stable kernel capability is transferred atomically;
2. descendants cannot escape the transferred domain under Ooze's supported-command contract;
3. the guardian acknowledges custody before Ooze relinquishes it;
4. the guardian survives the test binary, or its own death triggers kernel-enforced termination;
5. it can authoritatively observe emptiness and owns final workspace cleanup; and
6. no later campaign reuses the domain or workspace before that proof.

Windows Job handles come closest. A delegated Linux cgroup can support the domain and emptiness proof but not kill-on-guardian-close. No comparable public macOS primitive was found. Building a cross-platform guardian before resolving that asymmetry would add IPC and lifecycle machinery without delivering the promised invariant.

## Explicit unknowns and deferred decisions

1. **Supported-command contract.** Is Ooze protecting against ordinary descendant trees only, or must it contain commands that deliberately daemonize, call `setsid`, use Windows breakaway mechanisms, or otherwise escape? Ooze is not a security sandbox, but the contract must be explicit. Full hostile containment is not portable with the current mechanisms.
2. **macOS drain proof.** Under a “must not escape the created process group” contract, root reaped plus empty group is a useful proof. Without that contract, no public recursive proof was found.
3. **Linux cgroup availability.** We do not yet know whether the intended local and hosted-CI environments consistently delegate cgroup v2 subtrees. This must be measured before relying on `cgroup.kill`.
4. **Dedicated worker boundary.** A future major version could run each campaign in a dedicated Ooze worker supervised by the calling test process. That would make fail-stop more controlled, but it is a separate architecture decision and should not be smuggled into the transition algebra.
5. **Fatal API shape.** The internal engine needs a typed fatal result for deterministic simulation. Whether a future public non-testing API returns that value, while the current testing adapter panics, is a later API decision.
6. **Cleanup bound.** Its default and any future `OOZE_*` override belong to the deadline-policy work. This research establishes only that expiry cannot synthesize an emptiness proof.
7. **Diagnostic evidence.** At minimum the fatal fault should identify platform, domain and attempt IDs, command, workspace, termination operations attempted, last authoritative observation, and whether termination was accepted but emptiness remained unobserved. PID-only evidence must account for PID reuse.
8. **Artifact residue.** Failure to release an already-quiescent attempt workspace or snapshot backing
   artifact is an infrastructure failure, but it is not the same hazard as an unconfirmed runnable
   process. The domain model should not collapse them into one `Lost` state.

## Decision consequence

The earlier proposal—“after bounded best effort, move the resource to `Lost` and permit `Aborted`”—should be retracted.

The replacement is narrower and more honest:

> A successful or ordinarily aborted campaign requires every execution-domain obligation to be
> resolved and released. A local failure to establish that starts one process-runtime-wide cleanup
> epoch. In a containment-only epoch, an empty global residual authorizes forced, unscored `Aborted`
> results without a containment panic; a non-empty residual authorizes one fatal containment fault and
> fail-stop. If an `InvariantViolation` joins, it instead owns the one diagnostic and re-panic after
> the same settlement, with every other cause and any residual attached. Bounded normal return with a
> residual becomes permissible only after a durable guardian has accepted custody.
