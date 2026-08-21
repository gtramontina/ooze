# Fatal containment blast radius in a shared Go process

_Research date: 2026-08-21. This is a design input, not an implementation specification._

> **Resolution note:** The later [campaign transition-algebra decision](https://github.com/gtramontina/ooze/issues/57)
> retains this process-runtime blast radius, adds prospective launch obligations, and gives a joined
> `InvariantViolation` precedence over containment-only terminal presentation. The language below
> reflects those resolved refinements.

## Decision

A possible execution-domain leak in one active Ooze campaign is a **process-runtime-wide fault**, not
a campaign-local fault.

The first campaign-local observation that an execution-domain obligation has not resolved must close
the shared runtime to new work and start one coordinated drain of every nonterminal campaign. It must
not immediately return terminal `Fail` or panic. In a containment-only epoch, only after that global
drain settles may Ooze choose between:

- **every prospective launch resolved and every owned domain authoritatively drained:** every affected
  campaign receives a forced, unscored process-wide `Aborted` result; or
- **one or more execution obligations still unconfirmed:** one terminal
  `Fail(CleanupUnconfirmed)` carries the union of the non-empty residual execution obligations,
  diagnostics are flushed, and the elected `testing.T` adapter panics.

If an `InvariantViolation` joins the same epoch, its first ingress-ordered occurrence dominates
presentation: containment causes and any residual attach to that one diagnostic, and the elected guard
re-panics exactly once instead of emitting a separate `Fail` or containment panic.

This preserves the intended type invariant: `CleanupUnconfirmed` is terminal only when its residual
set is non-empty. Before the global sweep, the initiating observation is a **fatal seed** or
`DrainUnconfirmed`, not the final fault value.

The blast radius stops at Ooze's process runtime. It does not include another `go test` package
binary, another terminal's Ooze process, or arbitrary host processes.

## Why campaign-local handling is unsound

### A panic does not clean sibling Go work

Go's panic sequence runs deferred calls while unwinding the panicking goroutine and its callers; if
the panic reaches the top of that goroutine, the program terminates. It does not unwind unrelated
goroutines or run their defers
([Go specification](https://go.dev/ref/spec#Handling_panics)).

The `testing` package runs each test in its own goroutine. For an unrecovered panic, `tRunner` runs
that test's cleanup/reporting and then re-panics specifically so the process terminates
([`tRunner`](https://github.com/golang/go/blob/b2b97b94965fc7eede9664da1e07117215232ef6/src/testing/testing.go#L2037-L2134)).
Parallel tests may already be running in other goroutines
([`T.Parallel`](https://github.com/golang/go/blob/b2b97b94965fc7eede9664da1e07117215232ef6/src/testing/testing.go#L1914-L1982)).
Their driver defers and `t.Cleanup` callbacks are not a process-shutdown protocol.

`T.FailNow` is even less suitable: it calls `runtime.Goexit`, stops only the current test goroutine,
explicitly leaves other goroutines running, and permits later tests to execute
([`FailNow`](https://github.com/golang/go/blob/b2b97b94965fc7eede9664da1e07117215232ef6/src/testing/testing.go#L1074-L1108)).
`os.Exit` is not a substitute because it skips deferred functions entirely
([`os.Exit`](https://pkg.go.dev/os#Exit)).

Therefore the process-runtime drain must finish **before** any adapter panic. A campaign-local panic
can terminate the binary while sibling Ooze campaigns still own children.

### Parent death does not portably terminate descendants

The operating systems reinforce the same boundary:

- **Linux:** a child subreaper changes where orphaned descendants are reparented so that the
  subreaper can wait for them; it does not kill them
  ([`PR_SET_CHILD_SUBREAPER`](https://man7.org/linux/man-pages/man2/PR_SET_CHILD_SUBREAPER.2const.html)).
  `PR_SET_PDEATHSIG` is not recursive: its setting is cleared in a forked child, and its “parent” is
  the particular creating thread
  ([`PR_SET_PDEATHSIG`](https://man7.org/linux/man-pages/man2/PR_SET_PDEATHSIG.2const.html)).
  Exiting the Go test process is therefore not a descendant-tree kill.
- **macOS:** when a creating process exits, its children are reparented to `init`
  ([Apple process definitions](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/intro.2.html)).
  `killpg` requires an explicit caller action, and a descendant may create a new session and process
  group with `setsid`
  ([`killpg`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/killpg.2.html),
  [`setsid`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/setsid.2.html)).
  A panic in the tracking Go process supplies neither an action nor recursive containment.
- **Windows:** ordinary process termination also does not terminate child processes
  ([Microsoft process termination](https://learn.microsoft.com/en-us/windows/win32/procthread/terminating-a-process)).
  A correctly configured Job Object is the useful exception: ordinary `CreateProcess` children join
  by default, and `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` terminates the job hierarchy when the last
  handle closes
  ([Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)).
  [Ooze already sets that flag](../../internal/cmdtestrunner/process_tree_windows.go), but handle-close
  termination occurs as the test process disappears; it is not an observed drain, and a recovered
  panic leaves the handle open.

Windows thus has a stronger final backstop, but not a reason to give the three platforms different
campaign semantics.

### One residual execution obligation contaminates sibling evidence

The process-local broker's capacity and exclusive barriers are sound only while the runtime knows
which attempts consume its resources. An unresolved committed launch may already have created a
process that has not yet been reported, and an unconfirmed owned domain may still use CPU, memory,
process slots, files, ports, or descendant capacity outside that accounting. A sibling attempt
completing after the fatal linearization point can no longer prove that its deadline, fuse, or result
was uncontaminated.

Consequently:

- a score already committed before the fatal sequence remains history;
- every campaign nonterminal at that sequence becomes unscored; and
- no completion observed afterward may be converted into a score, even if its command exits
  normally.

The fault is runtime-wide for both **resource ownership** and **evidentiary attribution**.

## Exact fatal-path choreography

The runtime owner, not an individual campaign reducer or the admission broker alone, coordinates
this sequence.

### 1. Linearize closure before any other effect

On the first `DrainUnconfirmed` observation, atomically transition the process runtime from `Open` to
`FatalClosing(epoch)` and close its broker. This is the fatal linearization point. Concurrent seeds are
aggregated into that epoch in stable ingress order; they must not start competing panic paths.

At that boundary:

- accept no further campaign registration, admission request or grant, start commitment, or terminal
  commit;
- cancel and settle queued admission and confirmation-barrier requests with a runtime-fault
  notification;
- after its pending-start obligation settles, return a granted lease whose start was not committed;
- retain a granted lease whose start was committed until its associated attempt settles; and
- retain every prospective execution-domain obligation created by a start commitment accepted before
  closure, including a physical start delivered late because of a driver race.

Closing or “poisoning” only the broker is insufficient: it prevents new grants but cannot stop
already committed domains.

### 2. Invalidate and notify every nonterminal campaign

Deliver `RuntimeEmergency(epoch)` to all registered campaigns in stable logical order.

- Every still-live campaign enters `Draining(RuntimeEmergency(epoch))`; none returns terminal `Fail`
  or commits a score before epoch settlement.
- Siblings discard partial mutation evidence and later receive peer-fault abort authorization.
- Preparing campaigns settle snapshot, admission, and artifact obligations without starting a command;
  the runtime retains their registrations until epoch settlement.
- Running campaigns stop admitting mutants; already `StartCommitted` attempts remain owned until
  drained.

Notification is a reducer event, not `T.FailNow`, panic, context cancellation with ambiguous meaning,
or a best-effort broadcast that drivers may miss.

### 3. Drain the runtime-owned execution obligations concurrently

The runtime snapshots its prospective-launch and owned-domain registries and drives one emergency
cleanup epoch:

1. retain every prospective launch until it resolves as a proven pre-release failure or an owned
   domain;
2. request graceful stop concurrently for every owned domain for which that phase is still meaningful;
3. at one shared escalation instant, force-stop every domain not yet drained;
4. adopt and stop any late physical start corresponding to a pre-closure start commitment; and
5. wait to one absolute final deadline, not one deadline per campaign or process.

The initiating domain retains its existing termination progress; the global path must not repeatedly
reset its timeout and turn bounded cleanup into serial retries. Independent stop and wait operations
remain parallel.

Only a proven pre-release launch failure or authoritative `Drained` observation resolves an
execution-domain obligation. Failure to release a workspace or snapshot backing artifact after domain
drainage is an ordinary infrastructure abort. Before drainage, the artifact cannot be classified as
artifact-only residue; its associated execution-domain obligation remains live.

### 4. Aggregate before allowing any terminal result

When all observations arrive or the absolute deadline expires, compute the stable union of residual
execution-domain obligations. Its sealed variants distinguish unresolved committed launches from owned
but undrained domains.

- In a containment-only epoch, if the union is empty, issue epoch-scoped forced-abort authorization to
  every campaign while atomically releasing their registrations. They may then return unscored
  `Aborted(RuntimeContainmentEscalation)` results. There is no `Fail(CleanupUnconfirmed)` and no
  containment panic.
- In a containment-only epoch, if the union is non-empty, atomically elect and deliver
  `FatalAuthorized(epoch, residuals)` to one initiating reducer. **Only now** may it return terminal
  `Fail(CleanupUnconfirmed{residuals})`; non-elected registrations are atomically released with
  peer-fault abort authorization, but the process is not declared reusable.
- If an `InvariantViolation` joined the epoch, these containment-only presentation branches are
  suppressed. The first such violation owns one diagnostic and re-panic, with all causes and any
  residual attached.

Epoch settlement owns the union, rather than leaving each campaign to print a partial and potentially
contradictory account. The containment-only fatal value or invariant-dominant diagnostic carries it.

The resolved policy does not reopen after the all-empty case. The runtime transitions permanently to
`ClosedDrained(epoch)` or `ClosedUnconfirmed(epoch, nonemptyResidual)` as part of epoch settlement. A
recovered panic cannot reopen either state.

### 5. Close permanently, then panic at most once

For a non-empty containment-only union, the runtime first transitions to
`ClosedUnconfirmed(epoch, residual)` and wakes pending callers with the same fatal result. The elected
testing adapter emits one consolidated diagnostic, flushes it, and then panics. Panic is an adapter
policy; it is not a reducer effect. An empty containment-only union reaches `ClosedDrained(epoch)` and
does not panic.

If one or more invariant violations joined the epoch, the first such violation in stable ingress
order becomes the single diagnostic and panic cause whether or not the containment residual is empty.
All other causes attach to it; no second guard or adapter panics.

The permanent closure matters because Go permits a caller to recover a panic. If recovery occurs,
Ooze cannot force the host test to exit, but it can guarantee that no later `Release` through this
runtime starts another command. Recovery accepts responsibility for an unsafe host; it is not a
supported reuse path.

Other active adapters should receive peer-fault forced-abort authorization and must not independently
race to report different residual sets. The epoch deterministically elects one reporting owner, with
first-invariant dominance when a mixed-cause epoch contains an invariant violation.

## Why this matches established supervision boundaries

Established supervisors widen failure handling to the resource scope they own rather than merely
stopping the goroutine or root process that noticed the problem:

- systemd defaults to killing every remaining process in a unit's control group. Its documentation
  explicitly discourages main-process-only modes because children escape lifecycle/resource
  management; it escalates the whole applicable scope after `TimeoutStopSec`
  ([systemd kill policy](https://github.com/systemd/systemd/blob/58b0764a206fc6cc67aa1a1c60f9f766a366edf8/man/systemd.kill.xml#L63-L101)).
- Bazel kills a stale server's process group, waits for death, and exits with a local-environmental
  error instead of proceeding when termination cannot be confirmed
  ([Bazel source](https://github.com/bazelbuild/bazel/blob/5f76ff86cda0bd5b3e22053fd2ebe88eb0386f2e/src/main/cpp/blaze_util_posix.cc#L764-L778)).
- Docker can return a kill failure without terminating its client because a durable daemon remains
  the external owner. It waits for the container's not-running event after `SIGKILL` and reports an
  error if that event never arrives
  ([Moby source](https://github.com/moby/moby/blob/f0ea3132ecc456d83408cdb60a60bb9fa3d673bc/daemon/kill.go#L172-L215)).

Ooze currently has no portable durable owner above the Go test process with acknowledged custody and
an authoritative emptiness proof. Its honest current boundary is therefore: close the process-local
runtime, drain everything it can still reach, and, if any residual obligation remains, report it and
fail-stop the test process.

## Mission-required implementation facts

1. Before launch, the process runtime must register a prospective execution-domain obligation against
   the matching lease and start gate. The adapter establishes the initial containment promised by its
   platform contract before releasing the target instruction; parent-side native capability
   registration may resolve with the launch and remains covered by the prospective obligation. A
   campaign stack-local process handle is not enough for global drain.
2. Broker closure, the fatal sequence, campaign registration, start commitment, and domain registry
   must share one linearizable coordination boundary.
3. Reducers remain pure. They consume runtime-fault and global-drain observations and return typed
   effects/results; they do not inspect other campaigns, call panic, or read OS state.
4. The fatal path must be deterministic under simulation: choose the first seed explicitly, order
   mixed causes, notifications, and residuals stably, inject late starts/drains and invariant
   violations, and assert that post-fatal events never produce a score.
5. The cleanup bound is runtime-absolute and stop dispatch is concurrent. Healthy campaigns pay no
   extra command, retry, or probe.
6. `Finish(Completed | NoMutants | Aborted)` requires an empty live-obligation ledger.
   `Fail(CleanupUnconfirmed)` requires a statically non-empty, globally aggregated residual execution
   set, including unresolved committed launches.
7. In a containment-only epoch, the testing adapter panics only for a non-empty residual and only
   after the runtime sweep and consolidated reporting. An invariant-triggered epoch always re-panics
   once after the same drain discipline. A permanent closed state is installed before either panic can
   be recovered.

The present native runners do not yet expose the required ownership seam. The Windows Job handle is
local to [`runProcessTree`](../../internal/cmdtestrunner/process_tree_windows.go); the macOS tracker is
also stack-local in [`process_tree_darwin.go`](../../internal/cmdtestrunner/process_tree_darwin.go);
and the Linux helper in [`process_tree_linux.go`](../../internal/cmdtestrunner/process_tree_linux.go)
has no post-launch stop/custody channel available to a process runtime. Making those domains explicit
is required work, not an optional refinement.

## Future architecture, not required now

A fault could become campaign-local only after Ooze introduces a stronger ownership and attribution
boundary:

- a dedicated worker process per campaign whose supervisor owns a non-escapable execution domain;
- or a durable guardian that acknowledges custody of a stable Windows Job or delegated Linux cgroup,
  remains alive after the test process, and can prove the domain empty.

That architecture could stop one worker, preserve sibling campaigns, and return a typed error only
if the guardian both retains cleanup custody **and** prevents the quarantined domain from contaminating
sibling evidence—for example by proving it empty before siblings resume, or by enforcing sufficient
resource isolation. Custody alone does not make sibling deadlines attributable. This is not portable
today: Linux cgroup delegation is not assured, macOS has no equivalent public recursive containment
primitive, and the current helpers do not implement atomic custody transfer.

The following are therefore out of scope for the managed-execution slice:

- a cross-process admission broker;
- machine-wide campaign coordination;
- a hostile-process sandbox;
- guardian IPC and crash recovery;
- a new public non-testing error API; and
- platform-dependent campaign scoring semantics.

The current invariant is smaller: **one fatal execution-obligation uncertainty closes one Ooze
process runtime; no adapter panics until every prospective launch has resolved or appears in the
single residual set and every owned domain has drained or appears there. A joined invariant violation
dominates presentation, but never bypasses that drain discipline.**
