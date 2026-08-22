# What a descendant-process fuse can actually observe

_Research date: 2026-08-22. Measurements and platform facts for issue
[#60](https://github.com/gtramontina/ooze/issues/60)._

> **Resolution note:** [Choose the automatic runaway fuse](https://github.com/gtramontina/ooze/issues/60)
> resolved the conditional to **yes, under the automatic execution profile only**. Every measurement below
> was taken at the *ambient* `GOMAXPROCS`, which is not the profile an automatic attempt runs under; the
> "no threshold can work" conclusion holds for the serial profile and is superseded for the automatic one
> by the follow-up measurements in the final section. No kernel process-creation ceiling is set on any
> platform.

> **Scope:** [#57](https://github.com/gtramontina/ooze/issues/57) and
> [#63](https://github.com/gtramontina/ooze/issues/63) deliberately leave the fuse conditional —
> "which fuse observations, **if any**". This note establishes whether a descendant-count signal can
> discriminate a runaway mutant from an ordinary Go build, and what each platform can enforce.

## The measurement

Sampler: the whole process table via `sysctl kern.proc.all` every **10 ms**, descendants counted two
independent ways per sample (ppid tree walk from the root, and process-group match). Machine:
darwin/arm64, Apple M3 Max, **14 logical CPUs**, `GOMAXPROCS=14`, Go 1.26.6. 24 sampled runs.
Cold-cache runs used a fresh `GOCACHE` directory.

| Scenario | Peak descendants | x GOMAXPROCS |
| --- | --- | --- |
| Trivial single package, warm | 1 | 0.07 |
| `go test -p 1 ./...`, cold | 3 | 0.21 |
| Trivial standalone module, cold | 13 | 0.93 |
| **Ooze's own mutant command**, warm *and* cold | **17** | 1.21 |
| `go test -cover ./...`, cold | 27 | 1.93 |
| `make test` (`-race -cover -shuffle`), cold | 55 / 60 / 87 | up to 6.21 |
| **Legitimate ceiling measured** (14 concurrent cold `go tool covdata`) | **187** | 13.4 |
| **Deliberate 200-process runaway fixture** | **201** | 14.4 |

Three conclusions, each with the numbers behind it.

**An absolute count threshold cannot work.** Legitimate 187 against runaway 201 is a margin of
**1.07x**. Any threshold below 187 fires on an ordinary cold `go test -cover`-shaped workload; any
threshold above 201 misses a 200-process runaway.

**A threshold expressed as a multiple of `GOMAXPROCS` fails too, because the legitimate ceiling is
quadratic in CPU count.** On a cold cache `go tool covdata` *rebuilds itself*, spawning up to
`GOMAXPROCS` concurrent `compile` children — and `go test -cover ./...` runs up to `-p` of those
concurrently. The fan-out is `p + p²`, not `p`. Measured 187 against a predicted 14 + 196 = 210 at
p = 14; the same mechanism predicts roughly 1056 on a 32-vCPU runner. A linear threshold chases a
quadratic ceiling.

**A growth-rate threshold is strictly worse, because it is inverted.** Legitimate cold builds reached
**125 procs/s** over a 1 s window (814/s over 100 ms). The deliberately slow leaks grew at **75/s** and
**19/s** — below the legitimate maximum — and still reached 201 descendants. A rate threshold fires on
builds and misses leaks.

**The only signal with real headroom is sustained occupancy.** Largest count held continuously for at
least the window: legitimate max was **45** at 1 s and **28** at 2 s, against **201** for every runaway
variant — margins of 4.5x and 7.2x. Equivalently, time spent above 100 descendants was 0 ms in 19 of
20 legitimate runs (worst case 483 ms) against 2048–6710 ms for the runaways. Shape discriminates too:
a runaway climbs monotonically (`14→32→50→68→86→104→122→140→158→175→193→201`, +18/s) while build
fan-out oscillates (`14→120→36→28→…→53→27→10→1`).

Incidental but useful: **Ooze's own mutant command peaks at 17 descendants, identical cold and warm.**
Cold caches change process *throughput* ~10x (781 distinct pids against 73) but not *concurrency* —
Go's `-p` semaphore caps how many toolchain processes are alive at once. It is `-race -cover` that
explodes, and that is not the mutant command.

## What each platform can enforce

| | In-kernel count cap | Async notification | Setup required | Needs polling |
| --- | --- | --- | --- | --- |
| **Windows** | **Yes** — `JOB_OBJECT_LIMIT_ACTIVE_PROCESS`, counts *processes*, kills the offender on create | Yes (IOCP `JOB_OBJECT_MSG_ACTIVE_PROCESS_LIMIT`) but delivery explicitly "not guaranteed" | None; no privileges | No |
| **Linux** | Conditionally — cgroup v2 `pids.max`, `fork` returns `-EAGAIN` | Yes (`poll` on `pids.events`) | **Delegated uid-writable cgroup v2 subtree** | Where delegation is absent |
| **macOS** | **No — nothing exists** | Partial (`NOTE_EXIT` per known pid only) | n/a | Always |

The disqualifying details:

- **Linux `pids.max` counts threads, not processes.** Kernel docs: *"Note that PIDs used in this
  controller refer to TIDs, process IDs as used by the kernel."* A Go test binary routinely holds
  `GOMAXPROCS`+ OS threads, so a cap of "10 descendant processes" expressed as `pids.max=10` kills one
  healthy `go test`. It is wrong by construction as a descendant-*process* cap. It also needs a
  delegated subtree — systemd ships `Delegate=pids memory cpu` on `user@.service`, so an interactive
  terminal has it, but a system service or an unprivileged CI container may not.
- **macOS has no per-tree limit of any kind.** `RLIMIT_NPROC` and launchd's `NumberOfProcesses` are
  both per-UID, so they throttle the supervisor itself precisely when it needs to act. `NOTE_TRACK` is
  dead — Apple's own header reads *"DEPRECATED!!!!!!!!! NOTE_TRACK, NOTE_TRACKERR, and NOTE_CHILD are
  no longer supported as of 10.5"*, and `filt_procattach()` returns `ENOTSUP` if you try. There is no
  subreaper, and `NOTE_EXIT_REPARENTED` is `__deprecated_enum_msg("no longer sent")`. `NOTE_FORK` fires
  without the child pid, so reacting to it means re-enumerating anyway.
- **Windows has a counter footgun**: the active-process count decrements only once a terminated
  process's handles are all closed, so a supervisor holding child handles (as `os/exec` does until
  `Wait`) can wedge its own job at the cap.

## What the field does

**Zero of seven** mutation-testing tools surveyed — PIT, StrykerJS, Stryker.NET, mutmut,
cargo-mutants, Infection, Mull — contain any descendant-count limit, process-tree size monitoring, or
runaway concept distinct from a timeout. Every tool that has a fuse at all uses **time** as the fuse
variable, never **size**. Count caps exist only at the OS and container-runtime layer: cgroup
`pids.max`, Docker `--pids-limit`, systemd `TasksMax`, Windows job objects.

## What Ooze already has

The existing supervisor **already enumerates descendants on all three platforms and discards the
count**:

- Linux: `/proc`-based enumeration, one pass per drain iteration separated by a fixed 1 ms sleep; the
  loop collapses `len(children)` into a boolean `hasChildren`.
- macOS: full process-group snapshot via `sysctl kern.proc.pgrp`, also collapsed to a bool. The kqueue
  filter watches only the root pid with `NOTE_EXIT` — no live descendant tracking.
- Windows: `QueryInformationJobObject`/`JobObjectBasicProcessIdList` reads
  `NumberOfAssignedProcesses` **straight out of the kernel** and uses it only for buffer sizing.

All three enumerate only *after* the root command exits. The job object exists already, created before
launch, with exactly one limit flag: `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`. `ActiveProcessLimit` is
never assigned and no completion port is associated.

Test coverage today is one two-level orphan-spawner fixture. Not covered: fork bombs or unbounded
creation, depth > 2, breadth > 1, `setsid` escapees, `CREATE_BREAKAWAY_FROM_JOB`, signal ignorers, the
5 s drain budget expiring, or the supervision panic path.

## Residual risks to decide against, not to hide

- On macOS a double-forked descendant that also calls `setsid()` is invisible to both the ppid walk
  and the process-group check. No primitive fixes this.
- Between the start of a runaway and the mutation deadline firing, a platform with no free in-kernel
  cap is unprotected. Measured spawn rate of a trivial spawner was 1747 procs/s single-threaded.
- `kinfo_proc p_comm` lags the exec'd image, so any name-based filtering misclassifies processes
  mid-exec. Observed directly: a process reporting `comm=go` whose argv was already `.../compile`.

## Follow-up: the same questions under Ooze's own execution profile

Every measurement above ran at the ambient `GOMAXPROCS` (14). That is the wrong profile.
[#58](https://github.com/gtramontina/ooze/issues/58) gives every **automatic** attempt root
`GOMAXPROCS=1`, and `go help build` documents that `-p` — "the number of programs, such as build
commands or test binaries, that can be run in parallel" — **defaults to `GOMAXPROCS`**. Pinning it to 1
removes the toolchain fan-out that made count uninformative. Re-measured with `GOMAXPROCS` set
explicitly in the child environment, 38 runs, same 10 ms sampler:

| Command, cold cache | `GOMAXPROCS=14` | `GOMAXPROCS=1` |
| --- | --- | --- |
| Ooze's own mutant command | 17 (x4 runs) | **4** (x4 runs) |
| `gotestsum -- -race -cover -shuffle` | 42 / 52 / 67 | **4** |
| `go test -cover ./...` | 27 | **3** |
| N concurrent cold `go tool covdata`, N=14 | 101 / 138 / 101 | **28** (exactly 2N, zero variance) |
| Runaway fixture, 200 sleeping children | 201 | **201** (profile-independent) |

- **Automatic profile ceiling: 4 peak, 3 sustained-3s.** Margin to the runaway is **50x** on peak and
  **67x** on sustained occupancy. Time above 100 descendants was 0 ms in all eight legitimate runs
  against ~4,100 ms for the runaway.
- **Serial profile ceiling: 78** without a user-side process pool, **138** with one. Margin **2.58x** /
  **1.46x**. Only sustained occupancy retains usable separation (28 against 201, 7.2x).
- **`GOMAXPROCS=1` eliminates the quadratic nesting outright.** N concurrent cold `covdata` produce
  exactly `2N` descendants — one `go` plus exactly one `compile` child each — with zero variance across
  repetitions, against 7.2–10.8 per `covdata` at `GOMAXPROCS=14`.
- **The count is contention-independent under the automatic profile.** Peak was exactly 4 in every run,
  cold and warm, across repetitions. This is what licenses direct attribution without confirmation: peers
  cannot manufacture a fuse trip.
- **`k · (p + p²)` is not defensible.** The quadratic term is empirically absent once `GOMAXPROCS` is
  pinned: the formula gives 2 at `p=1` against a measured 4, and 210 at `p=14`, already above the runaway
  it must catch. The smallest `k` with no false positive anywhere is 25, which at `p=14` is 5,250 and can
  never fire.
- **An explicit `-p` in the supervised command is the dominant lever** and is bounded by nothing Ooze
  sets. Measured at `GOMAXPROCS=1`: `-p 8` → 10, `-p 14` → 29, `-p 32` → 50, fitting `peak ≈ 2p + 1`.
- **`-parallel` has zero effect on descendant count**, confirming it is in-process test parallelism:
  `-parallel 8` measured identically to no flag, and adding it on top of `-p 8` changed nothing. The
  readme's documented `-parallel 3` example is therefore unaffected.

### Process-group counting structurally undercounts

Ooze's macOS supervisor enumerates with `sysctl kern.proc.pgrp` (`process_tree_darwin.go:335`), but its
own children call `Setpgid` (`process_tree_darwin.go:187`, `process_tree_linux.go:121`) and thereby leave
the watched group. Up to **26% of samples disagreed** with a parent-pid walk, and at one measured peak
**2 of 4 live descendants were invisible to the group check**. A census must walk parent identity; group
membership is a containment handle for signalling, not a count.
