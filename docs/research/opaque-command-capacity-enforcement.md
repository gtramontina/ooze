# Capacity enforcement for an opaque test-command subtree

_Research date: 2026-08-21. This note answers whether Ooze can transparently enforce one total
CPU/concurrency budget over an arbitrary `WithTestCommand` subtree on Linux, macOS, and Windows._

> **Decision status:** Issue [#58](https://github.com/gtramontina/ooze/issues/58) adopts the narrow
> cross-platform contract established here. Automatic admission starts at aggregate detected capacity
> `P`; trustworthy hard shared exhaustion or a deadline with recorded peer overlap that disappears
> under exclusive confirmation irreversibly changes future automatic admission to one.
> `GOMAXPROCS=1` remains cooperative rather than a subtree quota. There is no intermediate backoff,
> recovery, command parsing, or user parallelism knob; fuse normalization and future
> `OOZE_*`/selection work are outside this decision.

## Conclusion

No portable mechanism can give that guarantee without changing the execution environment or
requiring descendant cooperation.

Ooze can portably bound the number of **attempt roots it admits** and can give Go descendants a
conservative default with `GOMAXPROCS=1`. It cannot honestly claim that an arbitrary command and
everything it causes will consume at most that many CPUs, processes, threads, bytes of memory, or
I/O operations. Those are different resources, and the operating systems expose different
boundaries for them.

Linux cgroup v2 and Windows Job Objects can provide materially stronger subtree enforcement when
their prerequisites are met. Linux needs an enabled, writable delegated cgroup hierarchy. Windows
can aggregate ordinary descendants in a Job Object, subject to nesting and breakaway rules. The
inspected public macOS interfaces provide inherited per-process or per-user limits and process
groups for signalling, but no equivalent aggregate CPU-rate and process-count boundary. A VM or
container can close that macOS gap only by making a configured execution environment a product
requirement; it is not a transparent way to run an arbitrary host command.

The smallest honest cross-platform contract is therefore:

1. Ooze enforces one process-local **attempt-admission** limit across its campaigns.
2. Automatic attempt roots receive `GOMAXPROCS=1` as a cooperative Go execution profile.
3. Platform supervision contains, observes, and drains the descendants covered by its execution
   domain; a separately defined future fuse may abort abnormal descendant growth.
4. Resource exhaustion, failed containment, and failed drainage are infrastructure outcomes and
   never mutation evidence.
5. Ooze does **not** promise a hard aggregate CPU, process, thread, memory, or I/O quota for an
   opaque command or for work it delegates to an external service.

That contract addresses the observed nested-Go oversubscription without building three different
resource managers and pretending they are equivalent.

## “One budget” conflates different guarantees

A single integer such as detected capacity `P` can bound Ooze-owned attempt admission. It cannot
simultaneously describe all of these resources:

| Resource | What a limit would mean | Why `P` is not enough |
| --- | --- | --- |
| CPU throughput | Aggregate CPU time available per scheduling period | A process can have many runnable threads; a quota is a rate, not a process count. |
| CPU placement | CPUs on which threads may execute | Affinity does not limit how many processes or threads exist, memory, or I/O. |
| Processes | Simultaneously live descendant processes | One test command needs wrappers, compilers, test binaries, and supervisors; the safe count is not the CPU count. |
| Threads | Simultaneously live kernel threads | A single process may create many threads even with one CPU of runnable work. |
| Memory | Aggregate resident or committed bytes | Workload memory is not derivable from CPU count. |
| I/O | Bytes or operations per unit time | Device, filesystem, and network I/O have different accounting boundaries. |

Go makes the distinction explicit: `GOMAXPROCS` controls how many CPUs may execute Go code
simultaneously, but the runtime may create arbitrarily more threads to service blocking system
calls ([Go FAQ](https://go.dev/doc/faq#number_cpus)). A `GOMAXPROCS=1` child profile is valuable,
but it is neither a thread limit nor a process-tree CPU quota.

## The command is opaque today

[`WithTestCommand`](../../options.go) splits one string on literal spaces, then
[`CMDTestRunner`](../../internal/cmdtestrunner/cmdtestrunner.go) starts the resulting executable
and arguments directly with the current environment. It does not invoke a shell unless the user
explicitly names one. Consequently:

- quoting and escaping are already not represented by the public string;
- the visible executable may be `make`, `gotestsum`, a script, or a private test harness rather
  than the program that creates the expensive work; and
- the command may contact an already-running daemon or remote executor whose work is not in its
  descendant tree.

No general command rewrite can see through those cases. Adding a POSIX shell parser would still
not model `cmd.exe`, PowerShell, make recipes, scripts, or programmatic `exec` calls. It would also
change the meaning of the existing direct-execution API.

### A `PATH` shim is interception, not enforcement

Ooze could prepend a directory containing wrapper executables named `go`, `make`, or other known
tools. That works only when a descendant performs a `PATH` lookup for one of those names. Go's own
lookup contract tries a path containing a separator directly and does not consult `PATH`; Windows
also applies `PATHEXT` and has command-line parsing exceptions for `cmd.exe` and batch files
([`os/exec.LookPath`](https://pkg.go.dev/os/exec#LookPath),
[`os/exec.Command`](https://pkg.go.dev/os/exec#Command)). An absolute executable path, a reset
`PATH`, an unwrapped tool, an in-process thread pool, or a remote service bypasses the shim.

Maintaining cross-platform wrappers for every build tool would be a large compatibility surface
with no hard guarantee. It is not justified for managed mutation execution.

## Environment controls help Go, but descendants may override them

### `GOMAXPROCS`

At startup a positive `GOMAXPROCS` environment value becomes a Go runtime's default. Program code
may later call `runtime.GOMAXPROCS`, and calling it disables automatic updates until explicitly
restored ([`runtime.GOMAXPROCS`](https://pkg.go.dev/runtime#GOMAXPROCS)). The value therefore
controls cooperating Go programs, not arbitrary native commands and not hostile or explicitly
overriding Go programs.

It is nevertheless the smallest useful intervention for Ooze's normal Go workload. For `go test`,
the `go` command's `-p` default is `GOMAXPROCS`, and each test binary's `-parallel` default is also
`GOMAXPROCS`. The latter applies only inside one test binary; package test binaries may run in
parallel according to `-p` ([Go command build flags and test flags](https://pkg.go.dev/cmd/go)).
Thus `GOMAXPROCS=1` removes the default nested fan-out without parsing the configured command.
For example, an explicit `go test -p=4` may still run four build commands or package test binaries
at once, each with its own per-process `GOMAXPROCS=1`; the environment value does not turn those
four processes into one shared CPU allocation.

It remains a default rather than an enforcement boundary:

- a non-Go descendant ignores it;
- a command can replace or remove the environment value;
- Go code can call `runtime.GOMAXPROCS`; and
- `go test -cpu` changes the test binary's `GOMAXPROCS` while running its test matrix, while explicit
  `-p` and `-parallel` flags override their defaults
  ([Go test flags](https://pkg.go.dev/cmd/go#hdr-Testing_flags),
  [`testing` implementation](https://go.dev/src/testing/testing.go#L2551)).

The runtime limit covers threads executing user-level Go code. Threads blocked in system calls do
not count, and foreign work reached through cgo is outside that execution accounting while the Go
runtime is in syscall state
([Go runtime environment contract](https://pkg.go.dev/runtime#hdr-Environment_Variables),
[`cgocall` implementation](https://github.com/golang/go/blob/go1.26.6/src/runtime/cgocall.go#L134-L184)).

### `GOFLAGS`

`GOFLAGS` supplies default flags to Go commands, but flags on the command line are applied later
and override it ([Go environment variables](https://pkg.go.dev/cmd/go#hdr-Environment_variables)).
Injecting `GOFLAGS=-p=1` therefore cannot enforce a limit. Merging it also requires preserving the
application's existing `GOFLAGS`, whereas replacing it silently changes unrelated build policy.
Adding `-parallel=1` still reaches only `go test` and remains command-line-overridable.

Go's process API confirms that an explicitly supplied environment is just the child's initial
environment; duplicate keys use the last value ([`exec.Cmd.Env`](https://pkg.go.dev/os/exec#Cmd)).
Nothing makes an inherited environment variable immutable. Since `GOMAXPROCS=1` already supplies
the right defaults for `-p` and `-parallel`, adding `GOFLAGS` produces conflict risk without a
stronger guarantee.

## Cooperative jobservers cannot govern an opaque subtree

GNU make's jobserver is a strong design for cooperating tools. The parent publishes authentication
through `MAKEFLAGS`; participating tools acquire and return tokens, and every command starts with
one implicit slot. The manual explicitly requires a tool to parse the protocol and return exactly
the tokens it acquired ([GNU make job slots](https://www.gnu.org/software/make/manual/html_node/Job-Slots.html)).
POSIX implementations use a FIFO or pipe; Windows uses a named semaphore
([POSIX jobserver](https://www.gnu.org/software/make/manual/html_node/POSIX-Jobserver.html),
[Windows jobserver](https://www.gnu.org/software/make/manual/html_node/Windows-Jobserver.html)).

That protocol does not stop a non-participant from creating processes or threads. Publishing an
Ooze jobserver could help GNU make and the subset of tools that deliberately integrate with it,
but it would not govern Go, arbitrary scripts, or custom test runners by default. It could also
conflict with a jobserver already supplied by an enclosing build. Correct token inheritance,
descriptor/semaphore lifetime, cancellation, and crash recovery would be substantial machinery
for a cooperative optimization.

The Go command documents no GNU jobserver contract. Its Go 1.26 implementation initializes its own
build parallelism from `runtime.GOMAXPROCS(0)` and starts that many action goroutines
([`cfg.BuildP`](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/cfg/cfg.go#L87),
[`Builder.Do`](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/work/exec.go#L216-L225)). Passing `MAKEFLAGS` is
therefore not a replacement for the child Go execution profile.

An Ooze-specific cross-process broker has the same boundary. It could coordinate attempt roots
from participating Ooze processes, but their descendants and external services would not acquire
its leases. It would add service discovery, security, stale-lease recovery, and cross-platform
lifecycle concerns without creating an OS resource quota. The resolved process-local broker is the
appropriate scope unless cross-process coordination becomes a separate product requirement.

## Native enforcement is materially different by operating system

### Linux: cgroup v2 is strong when delegated

A cgroup v2 subtree can enforce several aggregate limits:

- `cpu.max` caps fair-scheduler CPU bandwidth as quota per period;
- `pids.max` stops new `fork` or `clone` operations with `EAGAIN` and counts kernel tasks/TIDs;
- `memory.max` is an aggregate hard memory boundary; and
- `io.max` limits block-device bytes or operations per second
  ([Linux cgroup v2 CPU controller](https://docs.kernel.org/admin-guide/cgroup-v2.html#cpu),
  [PID controller](https://docs.kernel.org/admin-guide/cgroup-v2.html#pid),
  [memory controller](https://docs.kernel.org/admin-guide/cgroup-v2.html#memory),
  [I/O controller](https://docs.kernel.org/admin-guide/cgroup-v2.html#io)).

New children are born into the forking process's cgroup. A properly delegated subtree is contained:
the delegate cannot move processes outside it, and restrictions imposed by ancestors remain in
force ([cgroup membership and delegation](https://docs.kernel.org/admin-guide/cgroup-v2.html#delegation)).
This is a real kernel boundary, not an environment hint.

It is not universally available to an ordinary test library. Controllers are not enabled by
default, are available only when the kernel and parent hierarchy expose them, and are owned by the
parent cgroup. A non-root process needs a writable delegated hierarchy in which to create and
configure children ([controller availability and enablement](https://docs.kernel.org/admin-guide/cgroup-v2.html#controlling-controllers)).
Rootless container documentation illustrates the deployment dependency: resource flags work only
with cgroup v2 and systemd, and commonly only memory and PID controllers are delegated until an
administrator changes the service configuration
([Docker rootless resource limits](https://docs.docker.com/engine/security/rootless/tips/#limiting-resources)).

CPU quota, PID count, memory, and I/O would still need separate values. Setting `pids.max=P` is not
viable: a single Go test attempt may legitimately need more than `P` kernel tasks. Choosing those
additional limits is the process-fuse problem, not something derivable from detected CPU capacity.

CPU affinity alone is weaker. Linux affinity is inherited across `fork` and preserved across
`exec`, but a process with matching credentials can change a thread's mask; the effective mask is
also intersected with cgroup cpuset restrictions
([`sched_setaffinity(2)`](https://man7.org/linux/man-pages/man2/sched_setaffinity.2.html)). A cgroup
`cpuset` can enforce placement hierarchically, but placement on `P` CPUs is not a CPU-time quota and
does not limit processes, threads, memory, or I/O
([cgroup cpuset controller](https://docs.kernel.org/admin-guide/cgroup-v2.html#cpuset)).

### Windows: Job Objects can enforce an aggregate job boundary

Windows Job Objects manage associated processes as a unit. Ordinary `CreateProcess` children join
their parent's job by default, and jobs can be nested on Windows 8/Server 2012 and later. Breakaway
flags can weaken that containment; `Win32_Process.Create` is also documented as an exception to
automatic child association ([Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects),
[nested jobs](https://learn.microsoft.com/en-us/windows/win32/procthread/nested-jobs)).

A job can set:

- a hard aggregate CPU rate, after which its threads stop running until the next scheduling
  interval;
- a maximum number of active processes;
- job-wide committed-memory limits; and
- affinity for associated processes
  ([job CPU-rate control](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_cpu_rate_control_information),
  [basic job limits](https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_basic_limit_information)).

The process limit does not limit thread count. Job affinity restricts placement rather than CPU
throughput. CPU-rate control is unavailable when Remote Desktop Dynamic Fair Share Scheduling is
in effect, and a rate in a nested job is relative to its immediate parent's allocation. These are
strong primitives, but they do not collapse into one portable integer.

Ooze already creates one per-attempt Job Object with only `KILL_ON_JOB_CLOSE`
([`newProcessTreeJob`](../../internal/cmdtestrunner/process_tree_windows.go)). Enforcing one
aggregate runtime CPU or process limit would require a shared parent job plus compatible nested
per-attempt jobs, or abandoning independent per-attempt termination. That is feasible Windows-only
work, not a small extension to the portable admission policy.

Modern Windows also lacks a simple general job I/O-rate counterpart: Microsoft's documented
`JOBOBJECT_IO_RATE_CONTROL_INFORMATION` says it is unsupported starting with Windows 10 version
1607 ([job I/O rate structure](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/ns-jobapi2-jobobject_io_rate_control_information)).
Job accounting can observe I/O, but observation is not a throttle.

### macOS: process groups and rlimits do not form an aggregate resource domain

The inspected public macOS resource-limit API applies limits to the current process and each
process it creates. `RLIMIT_CPU` is CPU seconds **per process**, and `RLIMIT_NPROC` is the maximum
simultaneous process count for the entire user ID, not for one descendant subtree
([macOS `getrlimit(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getrlimit.2.html)).
A per-process CPU-time limit eventually terminates long healthy processes and multiplies with every
descendant; a per-user process limit couples Ooze to unrelated applications. Neither is the desired
aggregate budget.

macOS process groups exist for signal distribution and terminal job control
([macOS `getpgrp(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getpgrp.2.html)).
A descendant may create a new session and process group with `setsid`
([macOS `setsid(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/setsid.2.html)).
The current Ooze Darwin launcher uses a process group for supervision, but that group neither
accounts nor throttles aggregate CPU, processes, threads, memory, or I/O.

Apple exposes scheduling-priority controls, but priority determines scheduling preference rather
than a hard rate ([macOS `getpriority(2)`](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/getpriority.2.html)).
The inspected public interfaces contain no cgroup/Job-Object analogue that Ooze can create around
an arbitrary transient command. A privileged helper would not manufacture a missing aggregate
kernel contract.

## Containers and VMs are stronger, but not transparent

On Linux, container CPU and memory limits are implemented using host cgroups; Docker's `--cpus`,
for example, configures a CFS quota, while `--cpuset-cpus` configures placement
([Docker resource constraints](https://docs.docker.com/engine/containers/resource_constraints/)).
This can provide a strong execution boundary when an image, runtime, and daemon are already part of
the user's workflow.

It cannot transparently wrap an arbitrary host `WithTestCommand` on every supported OS. Docker
Desktop runs Linux containers in a Linux VM on macOS and Windows; source sharing, filesystem
semantics, binaries, credentials, network access, and toolchains cross that boundary
([Docker Desktop virtual-machine managers](https://docs.docker.com/desktop/features/vmm/),
[Docker Desktop resource settings](https://docs.docker.com/desktop/settings-and-maintenance/settings/#resources)).
The user must install and run the container system, supply an image, grant daemon access, and make
the repository and dependencies available inside it. Native macOS or Windows commands cannot run in
a Linux container unchanged.

Containerizing all mutation commands would therefore be a new execution product, not an internal
safety improvement. If a future strict resource guarantee is required, the honest interface is to
require a caller-provided sandbox/container executor and report its capabilities—not to provision
one invisibly.

## Guarantee matrix

| Mechanism | CPU | Processes | Threads | I/O | External broker work | Portable default? |
| --- | --- | --- | --- | --- | --- | --- |
| Ooze admission | Counts attempt roots | No descendant bound | No | No | No | Yes |
| `GOMAXPROCS=1` | Cooperative Go parallelism | No | No; extra blocked threads remain possible | No | No | Yes |
| `GOFLAGS` / rewritten flags | Go defaults only; CLI can override | No | No | No | No | Technically portable, not enforcement |
| `PATH` shim | Only intercepted programs | Only intercepted launches | No | No | No | Fragile |
| GNU jobserver | Participating work slots | Participating jobs only | Participating tools only | No | Only if the broker participates | Cooperative |
| Linux cgroup v2 | Hard fair-scheduler bandwidth | Hard task/TID limit | Counted by PID controller | Block-device BPS/IOPS | No, unless service is in the cgroup | Linux with delegation |
| Windows Job Object | Hard job CPU rate | Hard active-process limit | CPU applies to job threads; no count limit | Accounting; modern general rate API unavailable | No, unless service is in the job | Windows only |
| macOS process group + rlimits | Per-process time or scheduling priority | Per-user, not subtree | No | No aggregate rate | No | Insufficient |
| Container/VM | Strong when configured | Strong when configured | Strong when configured | Runtime/platform dependent | Only services inside boundary | Requires a new execution environment |

“External broker work” includes an already-running compiler daemon, container daemon, remote build
service, or any service asked to perform work by IPC. This conclusion follows from the native
membership rules: cgroups and Jobs account their members; process groups identify their members;
none charges an unrelated server's work to its client merely because the client requested it.

## Recommended contract and scope

The implementation should describe `P` as a detected-capacity input and admission ceiling, not as
CPUs exclusively owned or kernel-enforced by Ooze. For automatic mode, Ooze should guarantee only:

```text
FullAutomatic(P): at most P Ooze-owned shared attempts are start-committed.
SingleAdmissionAutomatic: at most one future Ooze-owned automatic attempt is admitted.

Each automatic attempt root starts with GOMAXPROCS=1.
```

The transition from full to single admission is one-way and affects no existing lease. It removes
future Ooze-owned outer overlap; it does not claim that one is a measured safe resource capacity.

The configured command remains opaque. Standard Go commands inherit useful safe defaults without
user tuning. Explicit overrides and non-Go internal parallelism remain outside the portable quota.
Typed, trustworthy infrastructure observations may activate the outer single-admission fallback.
The process fuse is resolved as a profile-scoped descendant-count ceiling (see
[#60](https://github.com/gtramontina/ooze/issues/60)); neither mechanism is evidence that Ooze can
reserve all resources in the command subtree.

A future optional native resource-domain adapter may be worthwhile only if users require stronger
containment and can supply the platform prerequisites:

- Linux: a delegated cgroup v2 subtree, with independently chosen CPU, PID, memory, and perhaps I/O
  policies;
- Windows: a shared parent Job Object with nested attempt jobs and independently chosen CPU and
  process policies; and
- macOS: an explicitly supplied container/VM executor, because there is no equivalent native
  public aggregate boundary in the inspected APIs.

Such an adapter must report capabilities and failure explicitly. Silent fallback would make the
same configuration mean hard enforcement on one machine and a hint on another.

For the current managed-execution work, command parsing, `PATH` interposition, a new jobserver, a
system-wide Ooze daemon, and automatic container provisioning are overengineering. They add broad
compatibility and lifecycle surfaces while still failing to enforce one total budget over every
opaque command. Process-local admission, the one-CPU Go child profile, correct containment, and
infrastructure-safe scoring are the smallest current design that truthfully addresses the observed
failure. A fuse is portable only under the automatic execution profile, where
[#60](https://github.com/gtramontina/ooze/issues/60) measured a fiftyfold margin; under the serial
profile the margin is 1.46x and no count observation is made.
