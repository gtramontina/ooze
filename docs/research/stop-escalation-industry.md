# Stop escalation in mutation-test supervisors

Research date: 2026-08-23. All implementation claims below come from pinned
first-party source revisions. No timing measurements were made.

## Finding

Immediate forced termination is common for a timed-out mutant run, but it is
not universal. The strongest recurring distinction is this:

- A mutant command that has exceeded its permission to run is usually killed
  or its worker is force-recycled without an application-shutdown grace period.
- A reusable worker being retired normally may receive an orderly dispose/exit
  request, followed by forced termination after a bounded grace period.

That supports immediate forced termination as a defensible Ooze policy. It does
not establish an industry requirement. `cargo-mutants` on Unix is the clearest
counterexample, and StrykerJS's plugin-worker path deliberately attempts bounded
disposal before killing its worker.

| Implementation | Timed-out mutation/test path | Ordinary worker shutdown | Scope limit |
| --- | --- | --- | --- |
| cargo-mutants | Unix: `SIGTERM` to the process group, then an unbounded wait. Windows: `Child::kill`. | Same termination path handles interruption. | Unix provides neither immediate force nor a forced fallback. |
| StrykerJS command runner | Immediate process-tree `SIGKILL`. | Not applicable: each run starts a command. | Its plugin-runner path differs. |
| StrykerJS plugin runner | Timeout recovers by disposing and recreating the runner; disposal is bounded, then the worker is process-tree `SIGKILL`ed. | Same bounded dispose-then-kill mechanism. | Grace is for the controlled worker/plugin lifecycle, not a contract with arbitrary test descendants. |
| Stryker.NET Microsoft Testing Platform runner | Force-restarts the assembly test server; kill requests the entire process tree. | Sends an exit RPC and waits up to 30 seconds, then force-kills. | It owns a persistent test server rather than one arbitrary command per mutant. |
| Infection | Its dependency's timeout check calls `stop(0)`: send `SIGTERM`, wait zero seconds, then `SIGKILL` if still live. | Symfony Process otherwise offers a configurable graceful interval. | Direct Symfony process semantics do not prove Ooze-style descendant containment. |
| PIT | The minion reports `TIMEOUT`; the parent then destroys the minion JVM forcibly. | Not examined in this audit. | PIT force-kills a JVM worker, not an arbitrary process domain per mutant. |
| Mull | Calls `Child::kill`, waits, then records `Timedout`. | No separate worker protocol in this path. | The shown runner owns only the direct child. |
| Go `os/exec.CommandContext` | Default cancellation calls `Process.Kill`. | Caller can replace `Cmd.Cancel`. | Generic direct-child API; no tree containment or mutation-test classification. |

## Primary-source evidence

### cargo-mutants

At revision [`0a46fd3`](https://github.com/sourcefrog/cargo-mutants/tree/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2),
timeout polling calls `terminate()` and then waits for the child
([`process.rs` lines 105-138](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/process.rs#L105-L138)).
On Unix, termination sends `SIGTERM` to the process group and has no later
`SIGKILL` step
([`unix.rs` lines 16-33](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/process/unix.rs#L16-L33)).
On Windows it calls `Child::kill`
([`windows.rs` lines 9-12](https://github.com/sourcefrog/cargo-mutants/blob/0a46fd3274c3ea5e51542fd343318dc6ab06cfe2/src/process/windows.rs#L9-L12)).

The `terminate()` documentation says "initially gently and then harshly," but
the cited Unix implementation contains only the gentle step. The behavior
reported here follows the executable source, not that comment.

### StrykerJS

At revision [`de5ae70`](https://github.com/stryker-mutator/stryker-js/tree/de5ae70f3cb488fd85e1f4c9a6850b84980bb671),
a test timeout calls `recover()`
([`timeout-decorator.ts` lines 48-70](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/timeout-decorator.ts#L48-L70)),
which disposes the current runner and constructs a replacement
([`resource-decorator.ts` lines 19-27](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/concurrent/resource-decorator.ts#L19-L27)).

For the command runner, disposal invokes its active timeout handler, which
records timeout and kills the command
([`command-test-runner.ts` lines 125-128 and 185-189](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/command-test-runner.ts#L125-L189)).
The shared kill helper requests process-tree `SIGKILL`
([`object-utils.ts` lines 65-81](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/utils/object-utils.ts#L65-L81)).

For plugin runners, Stryker first gives the inner runner up to two seconds to
dispose and then disposes its worker
([`child-process-test-runner-proxy.ts` lines 73-93](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/test-runner/child-process-test-runner-proxy.ts#L73-L93)).
Worker disposal sends a dispose message, waits on another two-second expirable
task, and unconditionally invokes the same kill helper in `finally`
([`child-process-proxy.ts` lines 333-345](https://github.com/stryker-mutator/stryker-js/blob/de5ae70f3cb488fd85e1f4c9a6850b84980bb671/packages/core/src/child-proxy/child-process-proxy.ts#L333-L345)).

### Stryker.NET

At revision [`eb94878`](https://github.com/stryker-mutator/stryker-net/tree/eb94878a9763a7315b891d1d0bcf11f225b32e28),
an assembly timeout restarts its test server with `force: true`
([`MicrosoftTestingPlatformRunner.cs` lines 912-939](https://github.com/stryker-mutator/stryker-net/blob/eb94878a9763a7315b891d1d0bcf11f225b32e28/src/Stryker.TestRunner.MicrosoftTestPlatform/MicrosoftTestingPlatformRunner.cs#L912-L939)).
The forced stop kills immediately; the non-forced stop instead sends an exit RPC,
waits up to 30 seconds for exit, and only then kills
([`AssemblyTestServer.cs` lines 199-228](https://github.com/stryker-mutator/stryker-net/blob/eb94878a9763a7315b891d1d0bcf11f225b32e28/src/Stryker.TestRunner.MicrosoftTestPlatform/AssemblyTestServer.cs#L199-L228)).
Its kill primitive requests the entire process tree
([`ProcessHandle.cs` lines 30-40](https://github.com/stryker-mutator/stryker-net/blob/eb94878a9763a7315b891d1d0bcf11f225b32e28/src/Stryker.TestRunner.MicrosoftTestPlatform/ProcessHandle.cs#L30-L40)).

### Infection and Symfony Process

At revision [`b452e00`](https://github.com/infection/infection/tree/b452e00dc12e2e0dececdc6355e11c1876f4a7d5),
Infection gives each mutant a Symfony `Process` timeout
([`MutantProcessContainerFactory.php` lines 71-86](https://github.com/infection/infection/blob/b452e00dc12e2e0dececdc6355e11c1876f4a7d5/src/Process/Factory/MutantProcessContainerFactory.php#L71-L86))
and calls `checkTimeout()` while polling
([`ParallelProcessRunner.php` lines 174-190](https://github.com/infection/infection/blob/b452e00dc12e2e0dececdc6355e11c1876f4a7d5/src/Process/Runner/ParallelProcessRunner.php#L174-L190)).
Its lockfile pins Symfony Process revision
[`d9593c9`](https://github.com/symfony/process/tree/d9593c9efa40499eb078b81144de42cbc28a31f0).
There, `checkTimeout()` calls `stop(0)`
([`Process.php` lines 1198-1223](https://github.com/symfony/process/blob/d9593c9efa40499eb078b81144de42cbc28a31f0/Process.php#L1198-L1223));
`stop()` sends `SIGTERM`, waits for the supplied interval, and then defaults to
`SIGKILL`
([`Process.php` lines 934-968](https://github.com/symfony/process/blob/d9593c9efa40499eb078b81144de42cbc28a31f0/Process.php#L934-L968)).
Because the timeout path supplies zero, it does not provide an application grace
interval.

### PIT

At revision [`ba0cfdc`](https://github.com/hcoles/pitest/tree/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df),
the timeout decorator schedules a side effect at the derived deadline
([`MutationTimeoutDecorator.java` lines 45-72](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/execute/MutationTimeoutDecorator.java#L45-L72)).
That side effect reports the `TIMEOUT` exit code
([`TimeOutSystemExitSideEffect.java` lines 14-17](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest/src/main/java/org/pitest/mutationtest/execute/TimeOutSystemExitSideEffect.java#L14-L17)).
The parent always destroys the minion after its communication thread finishes
([`MutationTestProcess.java` lines 50-75](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/mutationtest/execute/MutationTestProcess.java#L50-L75)),
and that destroy operation uses `destroyForcibly()`
([`JavaProcess.java` lines 39-43](https://github.com/hcoles/pitest/blob/ba0cfdc2d9dd0561c769f0822b43d07cc9a130df/pitest-entry/src/main/java/org/pitest/process/JavaProcess.java#L39-L43)).

### Mull

At revision [`a83b055`](https://github.com/mull-project/mull/tree/a83b055f77b3b9b9083a8e05cedec4ebaa22a521),
the runner's timeout branch calls `child.kill()`, waits for it, collects output,
and records `Timedout`; it has no graceful phase
([`runner.rs` lines 74-101](https://github.com/mull-project/mull/blob/a83b055f77b3b9b9083a8e05cedec4ebaa22a521/rust/mull-tools/runner.rs#L74-L101)).

### Go standard library comparator

In Go 1.26.6, `exec.CommandContext` documents and installs `Process.Kill` as its
default cancellation behavior; callers may replace `Cmd.Cancel`
([`exec.go` lines 475-493](https://github.com/golang/go/blob/go1.26.6/src/os/exec/exec.go#L475-L493)).
This is evidence that immediate forced cancellation is an ordinary Go default,
not evidence for process-tree containment or for mutation-test result semantics.

## Implication for Ooze

The comparables support, but do not compel, this narrow policy:

> Once an Ooze attempt has lost permission to continue because its resolved
> bound fired or the supervisor ordered a stop, begin forced domain termination
> immediately; do not add a general application-shutdown grace phase.

This is argued from the source comparison, not measured. It keeps the attempt
state machine deterministic and avoids granting a timed-out mutant extra time in
which it may spawn descendants. It should not prevent a future, separately
specified orderly shutdown protocol for an Ooze-owned persistent worker. Those
are different lifecycle events, as the Stryker implementations demonstrate.
