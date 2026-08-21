# Shell architecture for the process-local runtime and capacity broker

_Research date: 2026-08-21. This is a design input for issue
[#62](https://github.com/gtramontina/ooze/issues/62), not a decision._

> **Scope:** [#57](https://github.com/gtramontina/ooze/issues/57) fixes the campaign algebra and
> [#58](https://github.com/gtramontina/ooze/issues/58) fixes the two-state automatic admission policy.
> Both are preserved here. This note gathers the mechanical facts that bear on *how* the pure
> process-runtime/broker core is reached from concurrent `Release` callers: who applies an event, where
> effects are dispatched, and where a waiting acquirer parks. It records evidence only; the choice
> belongs to #62's resolution comment.

## What Ooze concurrency looks like today

Ooze currently owns almost no concurrency, so this change introduces the process runtime rather than
refactoring one.

- Exactly one `go` statement exists in non-test code: the stdout/stderr pump in
  `internal/cmdtestrunner/cmdtestrunner.go:39`.
- Exactly two channels exist in non-test code: `outputCopied` (`cmdtestrunner.go:38`) and the
  `ready chan struct{}` inside `DeferredFuture` (`internal/future/deferred.go:7`).
- Exactly one `sync.*` use exists in non-test code: `sync.Once` in `internal/future/deferred.go:6`.
- Per-mutant fan-out is a serial `for` loop (`internal/ooze/ooze.go:87-92`). Real parallelism is
  delegated entirely to `testing`: `t.Run` plus `t.Parallel()` at
  `internal/testingtlaboratory/testingtlaboratory.go:83-86`.
- Nothing bounds parallelism. There is no semaphore, `WaitGroup` limit, `errgroup`, or
  `golang.org/x/sync` dependency at all. The only bound is the *caller's* `go test -parallel` flag,
  which Ooze neither reads nor sets (`options.go:61-64`).
- `Options.Parallel` is a bool with no numeric degree (`options.go:22`), consistent with the map's
  "no public parallelism knob" scope boundary.

The existing `DeferredFuture` is already the grant-delivery idiom under discussion: park on
`<-f.ready`, publish by `close(f.ready)` exactly once under `sync.Once`
(`internal/future/deferred.go:11-22`).

## `testing/synctest` availability and its blocking model

- `testing/synctest` is GA from Go 1.25; no `GOEXPERIMENT` is needed for `synctest.Test`. The
  deprecated `synctest.Run` is `GOEXPERIMENT`-gated in 1.25 and removed in 1.26. With `go 1.25.0` +
  `toolchain go1.26.6`, `Test` and `Wait` are the whole API surface. (Go 1.25 release notes, "New
  testing/synctest package"; `src/testing/synctest/run.go:5-14` in go1.25.9; no `run.go` in go1.26.6.)
- The compatibility job pins Go 1.25.x with `GOTOOLCHAIN=local`
  (`.github/workflows/compatibility.yml:17-36`), so any `synctest` use must compile under 1.25 as
  well — which it does, since the package is GA there.

The load-bearing detail is *what counts as durably blocked*, quoted from
`src/testing/synctest/synctest.go:77-93` (go1.26.6):

> The following operations durably block a goroutine:
> - a blocking send or receive on a channel created within the bubble
> - a blocking select statement where every case is a channel created within the bubble
> - `sync.Cond.Wait`
> - `sync.WaitGroup.Wait`, when `sync.WaitGroup.Add` was called within the bubble
> - `time.Sleep`
>
> Operations not in the above list are not durably blocking. In particular, the following operations
> may block a goroutine, but are not durably blocking [...]
> - locking a `sync.Mutex` or `sync.RWMutex`
> - blocking on I/O, such as reading from a network socket
> - system calls

The runtime's own table agrees: `isIdleInSynctest` (`src/runtime/runtime2.go:1384-1398`) lists
`syncCondWait`, `sleep`, and the synctest channel/select reasons, and does **not** list
`waitReasonSyncMutexLock` (defined at `runtime2.go:1245`). A bubble is active while any goroutine is
non-durably blocked (`src/runtime/synctest.go:23-31`), and `Wait` returns only when every *other*
bubble goroutine is durably blocked.

Two empirical probes under go1.26.6 confirm the consequence:

- A bubble where one goroutine holds a mutex and sleeps on the fake clock while another blocks in
  `Lock` does **not** produce a synctest deadlock panic. It hangs until `go test -timeout` fires,
  because the `Lock`-blocked goroutine keeps `running > 0` so the fake clock never advances.
- `synctest.Wait()` never returns while any bubble goroutine is blocked in `Mutex.Lock`.

Note the precise hazard: it is *holding a lock across a durably-blocking operation*, not mutex use
per se. A goroutine that takes a lock, runs pure code, releases it, and then parks on a bubbled
channel is only transiently non-durable, and the bubble reaches idle immediately after. Damien Neil
states the rationale (<https://go.dev/blog/synctest>): "Since mutexes are usually not held for long
periods of time, we simply exclude them from `testing/synctest`'s consideration."

## Neither mutexes nor channels give Ooze an ordering guarantee

Arbitration must therefore be the reducer's job, not a primitive's — which is what
[#57](https://github.com/gtramontina/ooze/issues/57) already requires.

- `sync.Mutex` barges. `src/internal/sync/mutex.go:31-55`: "In normal mode waiters are queued in FIFO
  order, but a woken up waiter does not own the mutex and competes with new arriving goroutines over
  the ownership. New arriving goroutines have an advantage [...] If a waiter fails to acquire the
  mutex for more than 1ms, it switches mutex to the starvation mode." The threshold is
  `starvationThresholdNs = 1e6`; a re-blocking waiter is requeued LIFO. None of this is documented in
  the public `sync` API — `grep -in 'fair|FIFO|starv' src/sync/*.go` finds nothing.
- `sync.RWMutex` gives writer-preference *eventually*, with no ordering guarantee: a second writer
  still inside `rw.w.Lock()` has not announced itself and does not exclude new readers
  (`src/sync/rwmutex.go:22-27, 62-66`).
- Channels give no waiter-order guarantee. The spec's FIFO statement is about *values*, not blocked
  goroutines (<https://go.dev/ref/spec#Channel_types>). Runtime `waitq` is FIFO in practice
  (`src/runtime/chan.go:872-916`) but `dequeue` can skip a sudog that lost a `select` race, and
  golang/go#11506 hardened the implementation without adding a spec guarantee.
- `select` is explicitly random: "If one or more of the communications can proceed, a single one that
  can proceed is chosen via a uniform pseudo-random selection." (<https://go.dev/ref/spec#Select_statements>)

## Prior art: pure core plus shell

**etcd raft** is the canonical Go split. `RawNode` is documented as "a thread-unsafe Node"
(`rawnode.go:31-33`); `Step` performs no I/O (`rawnode.go:115-125`); effects are *buffered into
slices* by `raft.send()` rather than dispatched (`raft.go:592-598`); and `Ready()` is split into a
read-only `readyWithoutAccept` plus an `acceptReady` commit (`rawnode.go:131-139`) so a shell can
compute a batch without obligating itself to run it.

etcd's concurrent shell is a **single serializer goroutine, not a mutex**: `StartNode` calls
`go n.run()` and the `node` struct holds only channels plus `*RawNode`, with no mutex at all
(`node.go:287-310`). Its documented costs are instructive:

- proposals "can be lost without notice, therefore it is user's job to ensure proposal retries"
  (`node.go:138-140`);
- `tickc` is a 128-buffered channel and `Tick` drops on full (`node.go:312-329`, `node.go:458-465`);
- every public method must `select` on `n.done` to avoid deadlocking against a stopped `run()`.

**CockroachDB**, the library's heaviest production user, deleted that shell. Its in-tree fork
`pkg/raft` contains `rawnode.go` and `ready.go` but no `node.go`. It replaced the serializer with
documented lock ordering — "Locking notes: `Replica.raftMu < Replica.mu`" — and avoids reentrancy with
an inbox drained at the top of the next ready cycle rather than a callback.

**net/http2** shows the tax a serializer imposes on the API: it ships two entry points for the same
effect and documents which goroutine each is for — `writeFrameFromHandler` "must not be run from the
serve goroutine itself, else it might deadlock"; `writeFrame` is for the serve goroutine. Functions
become coloured by caller identity.

### Where comparable code dispatches its grants

- **`golang.org/x/sync/semaphore`**: a waiter is `{n int64; ready chan<- struct{}}` in a
  `container/list`; grant is `close(w.ready)` performed in `notifyWaiters` **under `s.mu`**
  (`semaphore.go:14-17, 28-33, 142-168`). FIFO is real but undocumented, so not an API contract.
- **Kubernetes `flowcontrol` queueset**: grants admission **while holding the lock**
  (`dispatchLocked` → `request.decision.Set(...)`), safe because `Set` only closes a channel and no
  user code runs under the lock; the cost is a lock convoy per admitted request.
- **CockroachDB admission `WorkQueue`**: does the opposite and says why — "Reduce critical section by
  sending on channel after releasing mutex" — and consequently must copy `requestedCount` out before
  unlocking, because the item can leave the heap once the lock is dropped.
- **CockroachDB `scheduler`**: signals waiters strictly outside the mutex and chunks bulk enqueues
  (`enqueueChunkSize = 128`) to bound lock hold time.

### Ticket ordering

- `x/sync/semaphore` makes any queued waiter a barrier for everyone behind it: the fast path requires
  `s.waiters.Len() == 0` (`semaphore.go:55`), and `notifyWaiters` deliberately stops at an
  unsatisfiable head — "If we allow the readers to jump ahead in the queue, the writer will starve"
  (`semaphore.go:150-162`). This is exactly the exclusive-barrier rule #57 requires.
- **Do not copy** its oversized-request behaviour: `Acquire(ctx, n)` with `n > size` blocks until
  cancellation instead of erroring (`semaphore.go:65-70`). Kubernetes handles the analogous case by
  draining to zero and running the oversized request alone. Ooze avoids the trap entirely by modelling
  exclusivity as a *mode* rather than a weight of `P`.
- **CockroachDB `lock_table`** argues for sequence-number tickets over positional FIFO because not all
  resources are known upfront, and keeps an inactive head-of-queue waiter as a "claim" so
  higher-sequence requests cannot barge ahead.
- No reusable Go library implements a strict FIFO shared/exclusive admission queue with tickets. The
  three closest (`x/sync/semaphore`, Kubernetes queueset, CockroachDB spanlatch/lock_table) are all
  embedded in larger systems and all deliberately relax strict FIFO.

## Repository constraints any design must satisfy

`.golangci.yml` sets `default: all` (106 linters enabled, 8 disabled: `ireturn`, `paralleltest`,
`depguard`, `gomodguard`, `mnd`, `testifylint`, `wsl`, `wsl_v5`) with no `linters.settings` block, so
every linter runs at upstream defaults. The ones that shape this design:

| Linter | Constraint | Consequence |
| --- | --- | --- |
| `funlen` | >60 lines or >40 statements fails | one monolithic `Advance` switch is not viable; per-event handlers are forced |
| `cyclop` / `gocyclo` | complexity >10 fails | same |
| `nestif` | nested-block complexity ≥4 fires | guard clauses over nested conditionals |
| `exhaustruct` | every composite literal must set every field | favours constructor functions and interface-based sealed sums over tagged structs with unused "oneof" fields |
| `exhaustive` | `switch` over a typed enum must cover every constant | sealed phase/state enums are checked for free |
| `gochecknoglobals` | any package-level `var` fires | the process-wide instance needs `//nolint:gochecknoglobals` (precedent: `release.go:43`, `release.go:49`) |
| `gochecknoinits` | `init()` fires | precedent `release.go:45` carries a nolint |
| `containedctx` | a `context.Context` struct field fires | context must be a parameter |
| `recvcheck` + govet `copylocks` | no mixed receivers; no value receiver on a type holding a sync primitive | the shell must be pointer-receiver, and the pure state must not embed a mutex if it is to stay a value |
| `nonamedreturns` | named results rejected | — |
| `interfacebloat` | >10 interface methods fails | — |
| `forbidigo` | `fmt.Print*` forbidden | log through the injected `Logger` seam (`internal/ooze/ooze.go:12-14`) |
| `err113` + `wrapcheck` | static wrapped errors only; wrap external errors | — |
| `testpackage` | `_test.go` must be `package x_test`; `*_internal_test.go` is exempt | same-package tests of the pure core go in `*_internal_test.go` (precedent: `internal/cmdtestrunner/process_guardian_unix_internal_test.go:3`) |
| `funcorder` | constructor before methods; exported before unexported | — |

`issues.fix: true` means `make lint` rewrites source in place. No enabled linter forbids goroutines,
sync primitives, generics, or panics; panic is the established internal failure mode (~30 sites, e.g.
`internal/cmdtestrunner/cmdtestrunner.go:57`).

CI runs `make test` = `gotestsum -- -race -cover -timeout=60s -shuffle=on ./...` on ubuntu-24.04
(Go 1.26.6), plus required jobs on Go 1.25.x with `GOTOOLCHAIN=local` and on
ubuntu-24.04/macos-26/windows-2025. The pre-commit hook runs `CPUS=1 make lint test.failfast`. A 60s
per-package timeout applies to every unit and simulation test.

## Open questions this note does not answer

1. Whether the shell applies events under a mutex or through a serializer goroutine.
2. Whether effects are dispatched under the lock (total order, k8s-style) or after release
   (short critical section, CockroachDB-style).
3. How sealed events, effects, and phases are represented in Go given `exhaustruct` and `exhaustive`.
4. The concrete queue and lease data structures, and the cancellation repair path.
5. Where the process-wide instance lives and how simulation constructs an isolated world.
