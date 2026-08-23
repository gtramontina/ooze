# Launch resource-exhaustion normalization

Research date: 2026-08-23. This is primary-source research for
[#61](https://github.com/gtramontina/ooze/issues/61). No timing measurement was made. The syscall and
Go behaviors below are sourced; the whitelist is an intentionally false-negative-biased Ooze policy.

## Rule

`LaunchResourceExhausted` is a closed launch result. Its classifier is a predicate over:

```text
(platform, exact primary operation, release stage, primary typed code, closure proof)
```

Every term is load-bearing:

1. The primary error comes from an enumerated Ooze-owned launch/setup operation.
2. Target code is proved not released; lack of an observation is insufficient.
3. If a launcher or suspended process exists, cleanup has authoritatively closed that obligation.
4. The typed code belongs to that operation's platform whitelist.

Classification happens before cleanup diagnostics are joined. Searching an `errors.Join` tree could
mistake a cleanup-side resource error for the cause of an unrelated primary launch failure. An
unknown release stage, post-release error, failed closure proof, malformed launcher status, or match
only in a cleanup sibling can never produce the closed branch.

## Initial operation tables

### Linux

| Proven-pre-release primary operation | Allowed typed codes |
| --- | --- |
| internal output/config/status descriptor acquisition | `EMFILE`, `ENFILE` |
| fixed launcher `Cmd.Start` | `EAGAIN`, `ENOMEM`, `EMFILE`, `ENFILE` |
| typed launcher-side target `execve` | `EAGAIN`, `ENOMEM` |

The four-code policy was accepted in #61 with an explicit conservative tradeoff. Linux documents
`fork` `EAGAIN` for process/thread limits and `ENOMEM` for kernel allocation or a dead PID-namespace
init; those latter meanings show that this is a classification policy, not proof that reducing Ooze
concurrency will recover. [`fork(2)`](https://man7.org/linux/man-pages/man2/fork.2.html) and
[`execve(2)`](https://man7.org/linux/man-pages/man2/execve.2.html) document the operation errors;
[`pipe(2)`](https://man7.org/linux/man-pages/man2/pipe.2.html) documents descriptor exhaustion.

The current Linux guardian collapses target-start failure with later supervision failure into exit
125. Production needs a typed launcher handshake before the inner `execve` row is usable; string or
exit-code guessing is excluded.

### Darwin

| Proven-pre-release primary operation | Allowed typed codes | Closure requirement |
| --- | --- | --- |
| internal output/config/status descriptor acquisition | `EMFILE`, `ENFILE` | no launcher exists, or the stopped launcher is reaped |
| fixed trusted `launcher.Start` | `EAGAIN`, `ENOMEM`, `EMFILE`, `ENFILE` | Go's failed `Start` path reaps pre-exec failure |
| `kqueue()` tracker creation | `ENOMEM`, `EMFILE`, `ENFILE` | stopped launcher killed and reaped |
| NOTE_EXIT `kevent(EV_ADD)` | `ENOMEM` | stopped launcher killed and reaped |
| typed launcher-side target `execve` | `ENOMEM` | replacement failed; launcher exits and is reaped |

Apple documents `fork` process-count `EAGAIN`, insufficient-swap `ENOMEM`, and that failure creates
no child in [xnu `fork(2)` lines 87–107](https://github.com/apple-oss-distributions/xnu/blob/88cc0b975a863932db8d475bd87c96c73292f9a3/bsd/man/man2/fork.2#L87-L107).
It documents pipe `EMFILE`/`ENFILE` in
[`pipe(2)` lines 97–114](https://github.com/apple-oss-distributions/xnu/blob/855239e564a912940801207fb9053ef0c13fd3cc/bsd/man/man2/pipe.2#L97-L114),
`kqueue` `ENOMEM`/`EMFILE`/`ENFILE` and `kevent` `ENOMEM` in
[`kqueue(2)` lines 729–769](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/man/man2/kqueue.2#L729-L769),
and `execve` `ENOMEM` in
[`execve(2)` lines 197–271](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/man/man2/execve.2#L197-L271).

Pinned Go 1.26.6 sources show `Cmd.Start`, the Unix status pipe and child-error read, `Wait4` before a
pre-exec error returns, and Darwin fork/setup/exec:

- [`os/exec.Cmd.Start`](https://github.com/golang/go/blob/1ea5a71ad8ceb7b9f16b4b6f8ea4739a4327dd6e/src/os/exec/exec.go#L641-L742)
- [Unix fork, child-error pipe, and reap](https://github.com/golang/go/blob/1ea5a71ad8ceb7b9f16b4b6f8ea4739a4327dd6e/src/syscall/exec_unix.go#L143-L249)
- [Darwin child setup and exec](https://github.com/golang/go/blob/1ea5a71ad8ceb7b9f16b4b6f8ea4739a4327dd6e/src/syscall/exec_libc2.go#L44-L300)
- [Darwin status-pipe setup](https://github.com/golang/go/blob/1ea5a71ad8ceb7b9f16b4b6f8ea4739a4327dd6e/src/syscall/forkpipe.go#L9-L21)

The current Darwin inner launcher writes `%v` text. Production needs a small typed status containing
operation and errno; missing or malformed status remains release-unknown. Darwin `execve` `ENOMEM`
may reflect a per-process VM limit rather than recoverable host contention, so its inclusion is
conservative policy rather than a recovery measurement.

### Windows

The same six-code whitelist applies only to internal-output acquisition, suspended `CreateProcess`
start, and pre-release Job/thread containment operations:

- `ERROR_TOO_MANY_OPEN_FILES` (4)
- `ERROR_NOT_ENOUGH_MEMORY` (8)
- `ERROR_OUTOFMEMORY` (14)
- `ERROR_NO_PROC_SLOTS` (89)
- `ERROR_NO_SYSTEM_RESOURCES` (1450)
- `ERROR_COMMITMENT_LIMIT` (1455)

Microsoft defines these as descriptor, memory/storage, process-slot, system-resource, and
commit/paging exhaustion in the current
[system codes 0–499](https://learn.microsoft.com/en-us/windows/win32/debug/system-error-codes--0-499-)
and [1300–1699](https://learn.microsoft.com/en-us/windows/win32/debug/system-error-codes--1300-1699-).
Go 1.26.6 itself treats codes 8 and 1455 as VirtualAlloc out-of-memory results in
[`runtime/mem_windows.go`](https://github.com/golang/go/blob/go1.26.6/src/runtime/mem_windows.go#L20-L21).
The repository's pinned x/sys exposes the names in
[`zerrors_windows.go`](https://github.com/golang/sys/blob/v0.47.0/windows/zerrors_windows.go).

Eligible pre-release operations are internal output acquisition; `CreateJobObject` and
`SetInformationJobObject`; failed `command.Start` with `CREATE_SUSPENDED`; and, after physical start
but before release, `OpenProcess`, `AssignProcessToJobObject`, and thread enumeration/opening. A
suspended process is not yet Owned under this contract: Microsoft says its primary thread does not
run until `ResumeThread` in the
[process-creation flags contract](https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags).
It must still be terminated and authoritatively settled before `NotReleased` closes.

A failing `ResumeThread` is release-unknown because the API documents failure as `DWORD(-1)` without
proving that it had no effect. Successful return with prior suspend count one is the release cut:
[`ResumeThread`](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-resumethread).
Any later error is owned/post-release.

Microsoft does not publish exhaustive error lists for every eligible API. The six names are therefore
a normalization whitelist, not a claim that they are every possible resource error. Path,
permission, image, configuration, disk/registry/remote capacity, insufficient-buffer, and ambiguous
quota/thread codes are ordinary `LaunchFailed` values. In particular, codes 155, 164, 567,
1451–1454, and 1816 remain excluded until a relevant-operation, identity-valid artifact supports an
observable contract expansion.

Pinned implementation paths:

- [Go Windows `StartProcess`/`CreateProcess`](https://github.com/golang/go/blob/go1.26.6/src/syscall/exec_windows.go#L307-L450)
- [Go `Cmd.Start`](https://github.com/golang/go/blob/go1.26.6/src/os/exec/exec.go#L641-L741)
- Ooze `internal/cmdtestrunner/process_tree_windows.go:32-59,68-149`

## Deterministic acceptance matrix for #67

For every allowed operation/code tuple:

- proven pre-release plus closed cleanup becomes `LaunchResourceExhausted`;
- the same code at unknown or post-release stage never does;
- a match only in joined cleanup never does;
- a suspended-process cleanup that remains unconfirmed stays a fatal residual;
- representative path, permission, invalid-configuration, disk, and quota codes remain
  `LaunchFailed`.

The semantic rule and initial tables belong to #61 because they determine #58's observable pressure
input. #67 owns build-tagged predicates, typed operation tags/launcher handshakes, stage tracking,
closure proof, and the deterministic table implementation.
