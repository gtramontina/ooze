# One-shot await in Go, and the fate of `internal/future`

_Research date: 2026-08-21. This is a design input, not a decision._

> **Scope:** prompted while resolving [#62](https://github.com/gtramontina/ooze/issues/62). Two separable
> questions are answered here: (1) is `internal/future`'s *mechanism* the right one, or does Go now offer
> something better; and (2) does the managed-execution design still need the *package*. Question 2 is
> deliberately left open — the map records "the migration route away from the current
> `testing.T`/future/laboratory orchestration" as not yet specified, gated on the reducer, supervisor,
> and broker contracts. This note supplies the evidence that patch will graduate on.

## Why the package exists

`internal/future` is 46 lines of non-test code across four files, introduced by `930ef4e`
(_feat: simple Future implementation_, 4 Jan 2023) and adopted four minutes later by `bc7bfb8`
(_refactor: replace explicit use of channels with a simple Future_). It replaced a hand-rolled
`<-chan result.Result[string]` that `907d154` had introduced three weeks earlier. Neither `930ef4e` nor
the later `df81417` carries a commit-message body, so the rationale has to be read off the diffs.

There is exactly **one** reason the value is unavailable at return time. `testingSubtests.Run` calls
`t.Parallel()` as the first statement of the subtest body, so `s.t.Run` returns while the body is still
parked (`internal/testingtlaboratory/testingtlaboratory.go:81-93`). `Test` therefore has to hand back a
placeholder. The repo pins both halves of that behaviour with dedicated tests:
`TestTestingSubtestsRunSerialBodyBeforeReturning` and
`TestTestingSubtestsDeferParallelBodyUntilParentReturns`
(`testingtlaboratory_test.go:88-129`).

Everything else about the type's shape follows from that one constraint:

- In `serial` mode the future is **provably redundant** — `t.Run` runs the body to completion, so `fut`
  is resolved before `Test` returns and `Await()` never blocks.
- `internal/laboratory/laboratory.go:34-39` contributes no asynchrony at all; it runs the command
  synchronously and wraps the finished value in `future.Resolved`. So do
  `internal/verboselaboratory/verboselaboratory.go:26-30` and
  `internal/oozetesting/fakelaboratory/fakelaboratory.go:53-63`. Three of the four `ooze.Laboratory`
  implementations exist as futures only to satisfy the interface — which is why `Future[T]` needs a
  second, already-resolved implementation at all.
- `Resolve`-once doubles as an abort channel: a `completed` flag plus a deferred
  `fut.Resolve(result.Err[string]("mutation execution aborted"))` guarantees a panicking subtest still
  publishes something rather than leaving `Await` blocked forever
  (`testingtlaboratory.go:49-54`, proven by `TestTestingTLaboratoryResolvesParallelFutureWhenDelegatePanics`).
- `T` is `result.Result[string]` at every production and reporter-test call site. The only other
  instantiations anywhere are `string` and `int`, both inside `future_test.go`.
- Blocking is safe only because no reporter awaits during `AddDiagnostic`
  (`consolereporter.go:35-37`, `verbosereporter.go:20-23`) and the sole await happens in
  `Summarize()`, invoked from the `t.Cleanup` in `release.go:127-133` — after Go has joined every
  parallel subtest.

`df81417` (_refactor: publish deferred futures synchronously_) fixed the one genuine defect. The
original `Resolve` was `f.once.Do(func() { go func() { f.channel <- value; close(f.channel) }() })` over
an **unbuffered** channel: it returned before the value was published and left a goroutine blocked on
the send until somebody awaited, so a never-awaited future leaked a goroutine permanently. `Await` also
took a `sync.Mutex` to latch the received value. The rewrite reduced both to
`close(f.ready)` / `<-f.ready`. Two consequences worth noting: a never-awaited future now costs
nothing, and the package became `synctest`-compatible, since a channel receive is durably blocking
while `sync.Mutex.Lock` explicitly is not.

## Go offers no replacement, by deliberate decision

Every exported symbol in Go 1.26 is recorded in `api/*.txt`. Grepping all 28 files at tag `go1.26.0`
for `future` and `promise` yields only `syscall.MCL_FUTURE`. There is no `sync.Future`, no
`x/sync` equivalent, and no accepted proposal for one.

| Proposal | State | Decisive comment |
| --- | --- | --- |
| [#17466](https://github.com/golang/go/issues/17466) add "future" internal type | closed 2018-01-23, locked | Ian Lance Taylor: "there is considerable overlap with channels, and I don't think it adds sufficient value over channels [...] The fact that many other languages offer futures is not convincing, since those languages generally do not offer channels." |
| [#17483](https://github.com/golang/go/issues/17483) add `sync.Future` | closed 2016-10-19 | Ian Lance Taylor: "the way to start something like this is to write an external library." |
| [#22293](https://github.com/golang/go/issues/22293) future-like sync primitive | closed as duplicate | Ian Lance Taylor: "When you already have channels, you don't also need futures." |
| [#33046](https://github.com/golang/go/issues/33046) goroutine returns a result channel | declined | bcmills prefers a done-channel plus an accessor over a result channel — the same split `Await`/`ready` makes. |
| [#56461](https://github.com/golang/go/issues/56461) `x/exp/future` | **open since 2022-10-27**, no maintainer ruling in ~4 years | — |

Nothing in the standard library fits the required shape, which is *push* (an external producer supplies
the value) plus *1:many blocking await*:

| Candidate | Why it does not fit |
| --- | --- |
| `sync.OnceValue` / `OnceValues` (Go 1.21) | **Pull, not push.** The returned closure calls `d.result = d.f()` on the *awaiting* goroutine's stack; the value's provenance is the function captured at construction. There is no way to hand a value in from outside. |
| `sync.Once` alone | Provides resolve-once, provides no await. A consumer that has not called `Do` cannot block until someone else's `Do` finishes. |
| `sync.WaitGroup`, incl. the new `WaitGroup.Go` (Go 1.25) | A completion barrier that carries no value: `Go` takes `func()`, `Wait` returns nothing. |
| `chan T` buffered 1 | Single-consumer. The value is taken by exactly one receiver; consumer #2 blocks forever. |
| `close(chan struct{})` + plain field | **This is the fit** — and it is what `internal/future` already does. The spec guarantees unlimited non-blocking receives after close. |
| `context.AfterFunc` | Push, but callback-shaped: needs a `Context` as trigger, delivers no value, one goroutine per registration. |
| `context.WithCancelCause` + `Cause` | Genuinely push-and-broadcast, but the payload type is fixed to `error`. Cannot carry `T`. |
| `atomic.Pointer[T]` | Race-free publication, no blocking await. (CockroachDB pairs it with a lazily created done channel for exactly this reason.) |
| `sync.Map` | `LoadOrStore` is non-blocking; a consumer arriving first installs its own zero value instead of waiting. |
| `x/sync/errgroup` | `Go` takes `func() error`, `Wait` returns an error. No value channel. |
| `x/sync/singleflight` | Pull (`fn` runs on the first caller's goroutine); results are **not retained** — `doCall` deletes the map entry, so a late consumer re-runs `fn`; `string` keys, `any` values; `DoChan`'s channel is buffered-1 and explicitly never closed, so one consumer per call. And on panic with waiters present it does `go panic(e); select{}` deliberately so the panic cannot be recovered. |

Go 1.25 changed one thing in `sync` (`WaitGroup.Go`); Go 1.26 changed nothing in `sync`, `sync/atomic`,
or `context` at all. The only 1.26 feature relevant here is the experimental goroutine-leak profile
(`GOEXPERIMENT=goroutineleakprofile`), which detects precisely the never-resolved-and-dropped failure
mode. The reason every real-world Go one-shot value is channel-backed is
[#16620](https://github.com/golang/go/issues/16620) — still open since 2016 — because there is no way to
`select` on a non-channel primitive.

## The mechanism is the mainstream idiom, including in our own prior art

`sync.Once` + `close(chan struct{})` + a plain sibling field read only after the close is not a
workaround; it is what the standard library does.

- **`x/net/http2`** (vendored into `net/http`) uses it byte-for-byte, twice in one struct:
  `abortOnce sync.Once` / `abort chan struct{}` / `abortErr error // set if abort is closed`, and
  `respHeaderRecv chan struct{}` / `res *http.Response // set if respHeaderRecv is closed`.
  `abortStreamLocked` *is* `Resolve`; the `RoundTrip` select *is* `Await`, with extra cases.
- **`net/http`'s connection pool** uses a buffered-1 result channel per `wantConn` plus an explicit
  `mu`/`done bool` one-shot guard, sending *then* closing so late receivers also unblock.
- **`database/sql`** uses a per-request buffered-1 `connRequest` channel, documented as buffered "so
  that the connectionOpener doesn't block".
- **etcd `pkg/wait`** generalizes it over request IDs: `Register(id) <-chan any` hands back a buffered-1
  channel and `Trigger(id, x)` sends-then-closes. It panics on duplicate registration.
- **CockroachDB** uses a buffered-1 `doneCh chan proposalResult` on per-request `ProposalData`, nil-ed
  after signalling; discipline enforced by comment, not type.
- **Kubernetes' apiserver** wraps it in a named type whose implementation is essentially identical to
  `DeferredFuture`: `promise.WriteOnce` is `sync.Once` + `close(setCh)` + a plain `value` field, with
  `Get()` blocking and `Set(v) bool`. Its consumer is the API Priority and Fairness **queueset** — the
  same code cited in `broker-shell-architecture.md` as prior art for delivering an admission grant
  under the lock.

That last convergence is the notable one: the canonical Go implementation of the thing this ticket is
designing uses this exact type to deliver its grants.

## Correctness review of the current implementation

```go
type DeferredFuture[T any] struct {
	once  sync.Once
	ready chan struct{}
	value T
}
func (f *DeferredFuture[T]) Await() T { <-f.ready; return f.value }
func (f *DeferredFuture[T]) Resolve(value T) {
	f.once.Do(func() { f.value = value; close(f.ready) })
}
```

**It is race-free.** The write `f.value = value` is sequenced before `close(f.ready)` in the same
goroutine, and "the closing of a channel is synchronized before a receive that returns a zero value
because the channel is closed" (<https://go.dev/ref/mem>). Happens-before is the transitive closure of
sequenced-before and synchronized-before, so the single write happens before every `Await`'s read.
`Once` admits only one write. There is no read-write and no write-write race. Belt and braces: `Once`
itself synchronizes — "the return from `f` synchronizes before the return from any call of `once.Do(f)`."

Four real gaps remain, each of which every cited real-world equivalent closes:

1. **The zero value is a landmine, and it fails silently.** `var f DeferredFuture[T]` leaves `ready`
   nil, so `Await` blocks forever and `Resolve` panics with "close of nil channel". Worse: `Once.Do`
   "considers a panicking `f` to have returned", so the first `Resolve` panics *and marks the `Once`
   done* — every later `Resolve` then silently no-ops. Recover that panic anywhere up-stack and the
   future is permanently unresolvable with nothing surfaced. Only the constructor guards this, and
   nothing enforces the constructor. `context` (`closedchan`) and CockroachDB's `util/future` both
   solve it by lazy channel init; the latter documents "A zero initialized Future is ready to be used."
2. **No `Done() <-chan struct{}`.** `Await() T` cannot be composed with cancellation, a deadline, or any
   other event, and — because #16620 is still open — a channel accessor is the *only* way to compose in
   Go. `x/net/http2` selects across four channels at its await point; CockroachDB exposes `Done()` and
   `Wait(ctx, f)`; the original `sync.Future` proposal listed a select-able channel as a requirement.
3. **`Resolve` returns nothing**, so a losing producer gets no signal and cannot distinguish "I set it"
   from "someone beat me". Kubernetes returns `bool` from `Set`; CockroachDB returns `wasSet` and offers
   `MustSet`. Ooze already pays for this: the `completed` flag at `testingtlaboratory.go:49-54` exists
   only because `Resolve` cannot report whether it won.
4. **`Await` returns a copy of `f.value`.** The close orders the write to the field, not writes to
   memory the field points at. If `T` ever contains a pointer, slice, or map, producer mutation after
   `Resolve` is a genuine race that the type does not prevent. Moot while `T` is
   `result.Result[string]`; not moot if `T` gains a slice.

## What this means for the managed-execution design

Two independent conclusions, deliberately not merged:

- **The mechanism is right.** Do not replace it with anything from the standard library, because there
  is nothing to replace it with, and the hand-rolled shape is the one the standard library itself uses.
  `df81417` already removed its only real defect.
- **The package's current *purpose* does not survive.** The sole reason a future exists is that
  `t.Parallel()` defers subtest bodies. Under managed execution Ooze owns fan-out, `t.Run` stops being
  the scheduler, and [#57](https://github.com/gtramontina/ooze/issues/57) makes reporting "a projection
  after an accepted terminal result, not a campaign phase or effect" — an await disappears from the
  reporting path. So `future.Future` as `Laboratory.Test`'s return type, the `Diagnostic.res` field, and
  the already-resolved second implementation all lose their reason to exist.

The mechanism nevertheless reappears one layer down: an admission request *is* a one-shot promise —
request now, grant later, at most once — which is exactly what Kubernetes' queueset uses
`promise.WriteOnce` for. Whether the lease waiter reuses a generalized `internal/future` (with `Done()`
added, gap 2 above) or a purpose-built unexported type is open, and belongs to
[#62](https://github.com/gtramontina/ooze/issues/62). Whether the package itself is deleted, and by
which commit, belongs to the migration-route patch still in the map's "Not yet specified".
