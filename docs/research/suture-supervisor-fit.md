# Suture fit for managed mutation execution

Research date: 2026-08-24. This is documentary research for
[#67](https://github.com/gtramontina/ooze/issues/67), not an implementation
specification. Behavioral claims about Suture come from its official
repository, pinned source, tests, and module metadata. Contract claims come
from Ooze's settled issue resolutions and current implementation. The final
recommendation is an engineering argument from those facts.

## Executive finding

[`github.com/thejerf/suture/v4`](https://github.com/thejerf/suture) cannot
replace Ooze's managed-mutation supervisor, and there is no demonstrated
benefit from embedding it inside #67.

The two systems supervise different things:

- Suture keeps long-lived **Go services** available by running
  `Serve(context.Context)` in goroutines and restarting services that return
  or panic.
- Ooze gives one opaque command attempt synchronous pre-release custody,
  contains its **OS process domain**, chooses one deterministic intent, forces
  and proves drainage, captures an immutable output prefix, releases or
  transfers custody, and returns one typed terminal.

Suture deliberately relies on cooperative context cancellation. Its own
contract says a non-cooperating service and the goroutine trying to stop it
remain leaked after the timeout. It has no process launch, process-tree
containment, native identity, authoritative emptiness observation, output
cutoff, exact completion token, or immutable terminal model. Wrapping Ooze's
implementation in a Suture `Service` would therefore retain all intrinsic
Ooze code and add a second scheduler-driven lifecycle and restart policy.

Suture remains a reasonable independent choice for an application that wants
to restart a long-lived, cooperative, non-custodial Go service. If Ooze later
acquires such a service outside the process-safety boundary, it could expose a
plain `Serve(ctx) error` method and allow a host to use Suture without Ooze
taking a dependency. Suture's own README recommends exactly that loose
integration style. There is no such use case in #67 today.

## Compared artifacts

### Ooze contract

The comparison uses these binding inputs:

- [#61's resolution](https://github.com/gtramontina/ooze/issues/61#issuecomment-5386039287)
  fixes the four-operation lifecycle, prospective-before-native launch,
  OS-specific containment behind one portable pure reducer, correlated
  generation/action tokens, authoritative drainage, immutable output
  evidence, typed terminals, and stable emergency settlement.
- [#62's resolution](https://github.com/gtramontina/ooze/issues/62#issuecomment-5388501179)
  fixes the exact-grant, data-only `startInstallation -> installedStart`
  seam under the process-runtime lock and exact-generation settlement.
- [#67's current #62 handoff](https://github.com/gtramontina/ooze/issues/67#issuecomment-5388492633)
  requires the concrete supervisor to consume that seam without bypassing
  its custody and invariant behavior.
- [#67's runtime-emergency amendment](https://github.com/gtramontina/ooze/issues/67#issuecomment-5389192995)
  and [pre-`Owned` provenance clarification](https://github.com/gtramontina/ooze/issues/67#issuecomment-5389372712)
  preserve deterministic physical-time and provenance rules even across an
  emergency arriving during launch.

The local implementation inspected was signed commit
`3dafed288d3061aaada5900aa04845ecff94e04a`: the #62 runtime in
`internal/ooze/process_runtime*.go`, the #67 lifecycle and pure reducer in
`internal/ooze/supervisor*.go`, the complete
`docs/prototypes/supervised-attempt` oracle, and the existing
`internal/cmdtestrunner` Darwin/Linux/Windows mechanisms.

### Suture identity and scope

The latest tag is
[`v4.0.6`](https://github.com/thejerf/suture/tree/v4.0.6), commit
[`8a2561c661dce2a30c484b08e865676059c1e63e`](https://github.com/thejerf/suture/commit/8a2561c661dce2a30c484b08e865676059c1e63e),
dated 2024-11-29. The current default-branch head inspected was
[`00e376b91b6b77b1b6caf74f757b3dd245e2fe60`](https://github.com/thejerf/suture/commit/00e376b91b6b77b1b6caf74f757b3dd245e2fe60),
dated 2026-07-21, eight commits after `v4.0.6`. The repository was not
archived, and its latest push was 2026-07-22 according to the official GitHub
repository API. Maintenance is not the reason for rejecting the fit.

The v4 module declares Go 1.9 and no module dependencies
([pinned `go.mod`](https://github.com/thejerf/suture/blob/v4.0.6/v4/go.mod)).
Its license is MIT
([pinned license](https://github.com/thejerf/suture/blob/v4.0.6/LICENSE)).
Ooze could legally and technically depend on it. Compatibility is not the
reason for rejecting the fit either.

### Unreleased HEAD lifecycle hooks

The eight post-release commits include
[`b6dc25e`](https://github.com/thejerf/suture/commit/b6dc25e49815acc0c4a2453dacf962cd2ee80525),
merged on 2026-07-22 through
[PR #78](https://github.com/thejerf/suture/pull/78), which adds
[`EventServiceStart` and `EventServiceStop`](https://github.com/thejerf/suture/blob/00e376b91b6b77b1b6caf74f757b3dd245e2fe60/v4/events.go#L190-L232).
They are useful logging hooks, but they are not lifecycle barriers:

- `runService` starts the service goroutine and then invokes
  `EventServiceStart`; the service may therefore enter or even return before
  the hook runs
  ([current source](https://github.com/thejerf/suture/blob/00e376b91b6b77b1b6caf74f757b3dd245e2fe60/v4/supervisor.go#L537-L575)).
- On a restartable nonpanic return, the service goroutine sends
  `serviceEnded` to the supervisor before invoking `EventServiceStop`. The
  supervisor's failure handler, when not in backoff, starts the replacement
  service before it emits `EventServiceTerminate`. A recovered-panic path
  likewise starts the replacement before `EventServicePanic`, but emits no
  old-service stop hook
  ([return path](https://github.com/thejerf/suture/blob/00e376b91b6b77b1b6caf74f757b3dd245e2fe60/v4/supervisor.go#L559-L574),
  [restart and failure-hook path](https://github.com/thejerf/suture/blob/00e376b91b6b77b1b6caf74f757b3dd245e2fe60/v4/supervisor.go#L471-L535)).
  Thus replacement-start precedes its failure hook by source order, while the
  nonpanic old service's stop hook can interleave with replacement-start and
  terminate after the channel receive. There is no total causal event order
  for a consumer to replay.

The PR describes these as observability for normal operations. That is
consistent with a logging surface; it does not claim the exact release/action
provenance Ooze requires. These hooks are not in `v4.0.6`, and the comparison
below does not credit or fault the release for unreleased behavior.

## Semantic comparison

| Ooze obligation | What Suture v4.0.6 actually supplies | Fit |
| --- | --- | --- |
| Synchronous pre-release launch custody | `Add` records a service or sends it to the running supervisor. `runService` creates a child context and starts `Serve` in a goroutine; there is no service-entry, target-release, or containment handshake ([`Add` and loop](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L259-L433), [`runService`](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L536-L572)). | No. A wrapper would still need Ooze's entire launch sequencer and #62 installation seam. |
| Exact `StartCommitted` broker linearization | Suture's opaque `ServiceToken` contains supervisor and service IDs for later removal. It has no external grant authority, prospective state, generation installation, or callback-free lock transition ([token](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L780-L801)). | No. Service registration is not prospective native custody. |
| Darwin/Linux/Windows process-tree containment | The released v4 module contains only the Suture module itself, with no dependencies. A pinned-tree scan found no `os/exec`, `syscall`, x/sys, Job Object, process-group, subreaper, kqueue, or PID mechanism. | None. Every native adapter would remain Ooze code. |
| Root exit is not drainage; emptiness must be authoritative | Returning from `Service.Serve` is the service-ended event and ordinarily triggers restart. Suture observes only that goroutine's return; it has no descendant domain or emptiness primitive ([service contract](https://github.com/thejerf/suture/blob/v4.0.6/v4/service.go#L8-L56), [restart branch](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L353-L384)). | Opposite abstraction. A root process can return while descendants remain, which is the exact state Ooze must not call drained. |
| Immediate force, one absolute drain bound, and bounded cleanup | Suture cancels a context and waits. `Timeout` only bounds the wait; it does not force the service. The service contract explicitly says both the service and stopping goroutine leak until cooperation ([service contract](https://github.com/thejerf/suture/blob/v4.0.6/v4/service.go#L40-L56), [stop path](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L574-L668)). | No. Returning after a timeout is a report, not a drainage proof or retained OS custody. |
| Immutable merged-output prefix at a causal cutoff | Suture defines service lifecycle/failure events only. Its event records contain names, restart counts, panic text, and an interface-valued error; there is no output file, cutoff, prefix length, completeness, or finality ([events](https://github.com/thejerf/suture/blob/v4.0.6/v4/events.go#L7-L148)). | None. |
| Typed terminal evidence and independent diagnostics | A normal service return or error is restart input unless it is `ErrDoNotRestart`/tree termination; errors are sent to logging events. There is no idempotent per-service `Wait` returning one immutable terminal ([service result contract](https://github.com/thejerf/suture/blob/v4.0.6/v4/service.go#L12-L38), [restart decision](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L353-L384)). | No. Mapping every Ooze result to `ErrDoNotRestart` would discard Suture's main value while still requiring Ooze's terminal model. |
| Exact generation and monotonic native action-token provenance | Suture's service ID routes add/remove/failure messages, but native actions and completions do not echo an exact generation/kind/token. Event errors are interface values and event order follows goroutine/channel delivery. | No. Adding correlation in a wrapper recreates the #67 reducer rather than replacing it. |
| Stable runtime-emergency cutoff and ordered immutable residuals | Shutdown iterates service maps, starts cancellation goroutines, and after timeout builds `UnstoppedServiceReport` from another map iteration. Go specifies map iteration order as unspecified, and Suture's docs call the report a TOCTOU observation because services may stop later ([shutdown](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L609-L668), [report warning](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L435-L458), [Go range specification](https://go.dev/ref/spec#For_statements)). | No. Unspecified order and post-report mutation are incompatible with Ooze's stable cutoff and immutable ordered residual ledger. |
| Pure deterministic reducer and replayable simulation | The core is a live `select` loop over channels and timers; starts, stops, and restarts use goroutines. When several cases are ready, Go's `select` makes a pseudo-random choice. Defaults include `time.After`, `time.Now`, and random backoff jitter ([loop](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L344-L433), [jitter](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L808-L836), [Go select specification](https://go.dev/ref/spec#Select_statements)). Private test clock hooks do not expose a replayable event reducer to consumers. | No. It would add scheduler semantics alongside, not remove, Ooze's pure reducer. |
| No hidden goroutine ownership and caller-owned waiting | Suture creates the goroutine that invokes each service. `ServeBackground` also creates the outer supervisor goroutine, and remove/shutdown creates helper goroutines that call blocking cancellation; a caller using `Serve` directly still owns that outer call. The current README explains the Suture-created goroutines visible when a service fails to stop in a `synctest` bubble ([service start](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L536-L572), [remove/shutdown](https://github.com/thejerf/suture/blob/v4.0.6/v4/supervisor.go#L574-L668), [current first-party diagnosis](https://github.com/thejerf/suture/blob/00e376b91b6b77b1b6caf74f757b3dd245e2fe60/README.md#L46-L93)). | No inside #67. Those goroutines are the mechanism Suture provides, while #61 deliberately makes waiting caller-owned and native effects explicitly correlated. |

## Concrete counterexamples

These are contract compositions, not hypothetical stylistic objections.

### A service ignores cancellation

Suppose an adapter blocks in `Serve` and ignores its context. Suture's
supervisor reaches its timeout, emits `EventStopTimeout`, returns an
`UnstoppedServiceReport`, and leaves the service plus its blocking
cancellation goroutine alive. The official service documentation states that
leak explicitly. Ooze instead must retain the native identity, force the
process domain, establish authoritative emptiness or return
`DrainUnconfirmed`, and keep residual custody. Suture's timeout cannot be
reinterpreted as either outcome.

### The command root exits while a descendant remains

A wrapper whose `Serve` returns when `cmd.Wait` returns causes Suture to treat
the service as ended and normally restart it. That transition contains no
fact about descendants. Ooze's #61 contract explicitly says root exit is not
drainage and uses different Darwin, Linux, and Windows mechanisms to prove or
deny domain emptiness. Keeping the wrapper alive until Ooze proves drainage
means Ooze already performed all the hard work; Suture adds nothing.

### Launch races the process-runtime fatal cut

Ooze must install a prospective exact generation under #62's runtime mutex
before any native work, then permit executable work only through the returned
post-unlock capability. Suture's `Add`/`runService` path has no grant,
prospective cell, release revocation, or external atomic cut. Adding them to a
`Service` wrapper leaves Suture outside the load-bearing linearization and
does not simplify it.

### Two facts arrive at the same logical instant

Ooze resolves same-time facts by a fixed priority after validating generation
and action provenance; tests replay those facts without goroutines or clocks.
Suture's loop chooses ready channel cases using Go `select`, then applies
restart/backoff behavior using live time and optional jitter. It cannot serve
as the semantic ordering engine without replacing its core, at which point it
is no longer reuse.

### Output grows after a drain cutoff

Ooze returns a fixed prefix and states whether it was complete through its
cutoff and final after proven drainage. Suture has no output ownership or
cutoff event. An `EventHook` copy of output would be ordinary live logging,
not causal immutable evidence.

## Strongest steelman: use Suture only as the driver shell

The strongest case is not to replace the reducer or native adapters. It is to
keep them and use one private Suture supervisor to run each long-lived native
driver, sampler, or reaper as a `Service`. Suture would then appear to provide
panic containment, context fan-out, bounded waits, lifecycle logging, and
restart backoff, potentially deleting custom goroutine bookkeeping while the
Ooze reducer remains the semantic authority.

That version still does not survive the concrete boundary:

1. A wait, census, sampler, or reaper failure is exact-generation evidence,
   not a generic availability failure. Restarting the service cannot erase
   the gap or prove that no process event was missed. Disabling restart with
   `ErrDoNotRestart` removes Suture's central benefit.
2. Suture's service and hook events carry no Ooze generation/action token.
   The wrapper must retain the complete action inventory and correlation
   logic, so no load-bearing driver bookkeeping is deleted.
3. Even HEAD's stronger start/stop observability is deliberately asynchronous:
   start is not service entry, replacement is launched before the failure
   hook, and the old stop hook has scheduler-dependent order. It cannot be the
   deterministic trace or release linearization.
4. A non-cooperating service still leaks after the timeout. The surrounding
   Ooze driver must therefore retain the same native identity, force, drain,
   output, and residual-custody paths it had without Suture.

The steelman wraps the intrinsic module rather than deepening it. It adds a
second state machine whose useful restart semantics are unsafe or disabled,
while preserving every safety-critical Ooze component.

### External composition

One external composition remains sound: a future application may choose to
run an entire long-lived Ooze coordinator as a Suture service for application
availability. The Suture README notes that libraries can expose
`Serve(ctx) error` without depending on Suture and remain directly usable
([current README](https://github.com/thejerf/suture/blob/00e376b91b6b77b1b6caf74f757b3dd245e2fe60/README.md#L123-L137)).
That supervision would sit outside Ooze's attempt contract and must never be
treated as process drainage or emergency settlement.

## Recommendation

Do not add Suture to #67 and do not replace the managed-mutation supervisor
with it.

Continue with one deep concrete Ooze module: the private deterministic reducer
owns portable policy and evidence; thin build-tagged adapters own OS
mechanics; the #62 lock seam owns prospective and terminal custody. Keep every
driver goroutine subordinate to a correlated reducer action rather than to a
second restart supervisor.

Revisit Suture only if a separate, long-lived, cooperative Go service with a
real restart-on-return requirement appears outside the attempt-safety
boundary. At that point prefer exposing `Serve(ctx) error` and letting an
embedding application choose Suture, instead of coupling Ooze to it.

## Reproducible inspection

Commands were run on Darwin 25.5 arm64 from Ooze commit
`3dafed288d3061aaada5900aa04845ecff94e04a`. No performance numbers were
measured.

```sh
git ls-remote https://github.com/thejerf/suture.git HEAD 'refs/tags/*'
git clone --filter=blob:none https://github.com/thejerf/suture.git /private/tmp/ooze-suture-research
git -C /private/tmp/ooze-suture-research log -1 --format='%H%n%aI%n%s'
git -C /private/tmp/ooze-suture-research describe --tags --always
git -C /private/tmp/ooze-suture-research rev-list --count v4.0.6..HEAD
git -C /private/tmp/ooze-suture-research diff --stat v4.0.6..HEAD -- v4
git -C /private/tmp/ooze-suture-research worktree add --detach /private/tmp/ooze-suture-v4.0.6 v4.0.6
env GOCACHE=/private/tmp/ooze-suture-go-cache devbox run -- \
  go -C /private/tmp/ooze-suture-v4.0.6/v4 test -count=1 ./...
env GOCACHE=/private/tmp/ooze-suture-go-cache devbox run -- \
  go -C /private/tmp/ooze-suture-v4.0.6/v4 test -race -count=1 ./...
env GOCACHE=/private/tmp/ooze-suture-go-cache devbox run -- \
  go -C /private/tmp/ooze-suture-v4.0.6/v4 list -m -json all
rg -n 'os/exec|syscall|x/sys|CreateProcess|JobObject|Setpgid|SIGKILL|Wait4|pidfd|kqueue|process group' \
  /private/tmp/ooze-suture-v4.0.6/v4
gh api repos/thejerf/suture \
  --jq '{default_branch,license:.license.spdx_id,pushed_at,updated_at,archived,disabled,open_issues_count}'
```

The original exploratory suite commands omitted `-count=1`; their results are
not used as verification evidence. Independent `-count=1` reruns on Darwin
25.5.0 arm64 with Go 1.26.6 produced one sample each: pinned `v4.0.6` normal
PASS (`0.423s` package time) and race PASS (`1.399s` package time).
`go list -m` returned only the main module. The native-process scan returned
no matches. `rev-list` reported eight commits after the tag; the v4 diff was
58 insertions and 9 deletions across `events.go`, `supervisor.go`, and
`suture_test.go`. Those are maintenance and verification facts, not evidence
that Suture has or lacks Ooze's native semantics; the pinned source comparison
establishes that.

### Counter-review of unreleased HEAD

The same platform and toolchain were used against exact current head
`00e376b91b6b77b1b6caf74f757b3dd245e2fe60`:

```sh
env GOCACHE=/private/tmp/ooze-suture-head-go1266-cache devbox run -- \
  go -C /private/tmp/ooze-suture-research/v4 test -count=1 ./...
env GOCACHE=/private/tmp/ooze-suture-head-go1266-cache devbox run -- \
  go -C /private/tmp/ooze-suture-research/v4 test -race -count=1 ./...
```

One normal sample passed (`0.186s` package time). One race sample failed
(`0.199s` package time). The first reported race was a test write to
`s.spec.EventHook` at `suture_test.go:382` against the new start-hook read at
`supervisor.go:574`; other reports include test hook mutation against the new
stop-hook read at `supervisor.go:566`. This is direct evidence that the current
HEAD test suite mutates its hook after starting concurrent use. It is not a
demonstration of an end-user race, not a maintenance conclusion, and not a
failure of released `v4.0.6`, whose separately pinned race sample passed.
