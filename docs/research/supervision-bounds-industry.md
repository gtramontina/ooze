# Supervision progress bounds across mutation runners and process supervisors

Research date: 2026-08-23. This is documentary research for
[#61](https://github.com/gtramontina/ooze/issues/61), not an implementation
specification. No timings were measured. Every behavioral claim below comes
from first-party documentation or pinned source; the recommendation is argued
from those observations.

## Executive finding

There is no industry number that Ooze can honestly copy for launch progress,
authoritative post-kill drainage, or runtime-wide emergency drainage.

The comparables instead expose four different concepts:

1. command execution time, commonly derived from workload measurements;
2. launch or readiness time, commonly a separately resolved policy;
3. cooperative shutdown grace, commonly configurable because application work
   happens during it; and
4. post-force observation, usually private supervisor policy or the remainder
   of an owning request deadline.

Ooze has already chosen immediate forced termination, so application shutdown
grace is not part of this decision. Its authoritative process-domain drainage
is also stronger than the direct-child waits and inherited-pipe checks offered
by most test runners.

The simplest defensible #61 contract is therefore:

- require a positive, resolved **launch-progress deadline**;
- require a positive, resolved **local drain deadline** for each owned attempt;
- require a positive, resolved **runtime-emergency deadline** for the one
  concurrent global sweep;
- resolve those deadlines centrally before giving them to the deterministic
  state machine and record the absolute instants in its trace;
- keep mutation command execution deadlines separate; and
- choose no numeric supervision default and add no public supervision knob in
  #61.

There are three semantic absolute deadlines, but only two duration-policy
classes are presently justified:

```text
launch start       + launch-progress policy = launch by
local drain start  + drain-epoch policy      = local drain by
emergency start    + drain-epoch policy      = emergency by
```

Local and emergency drainage are separate epochs and therefore have separate
absolute instants. Reusing the drain-epoch *policy class* does not reuse an
expired deadline. It avoids inventing a third duration without evidence. The
runtime sweep dispatches independent stops concurrently and applies one
absolute deadline to the whole epoch; it must not multiply the budget by
attempt, process, native step, or retry.

This recommendation retracts five seconds as a #61 contract number while
retaining the useful part of the earlier decision: every drain epoch has one
absolute deadline and every native step receives only its remaining time.

## What expiry means

A bound limits how long Ooze waits. It does not manufacture a containment
fact.

| Expired bound | Contract result |
| --- | --- |
| Launch progress | The prospective launch remains unresolved. Ooze retains custody for late adoption and starts fatal containment; it does not claim that target code never ran. |
| Local authoritative drain | `DrainUnconfirmed` is a fatal seed. It is not an empty-domain observation. |
| Runtime emergency | The stable non-empty residual becomes `CleanupUnconfirmed`; the process runtime stays permanently closed and the testing integration fails closed. |

An enclosing caller cancellation or test-harness deadline may request that
work stop, but it cannot be the sole cleanup policy. It may be absent, already
expired when cleanup starts, or terminate the host before Ooze settles its
ledger. Moby makes the same ownership distinction by removing caller
cancellation from its stop operation before applying its own finite waits
([pinned stop path](https://github.com/moby/moby/blob/b985e2313a8676e18072b442911fc8f64f17d440/daemon/stop.go)).

## Mutation runners and Go process libraries

These tools strongly inform command execution policy. They provide much weaker
evidence for Ooze's supervision bounds.

| Source | Resolved policy | What happens afterward | Limit of comparison |
| --- | --- | --- | --- |
| cargo-mutants 27.1.0 | Mutant tests default to `max(5 × baseline, 20s)` with fixed-timeout and multiplier/floor overrides. A skipped baseline falls back to 300s. Derived build timeouts were removed in 24.7.1 because project variance caused flakes. | The timed-out build/test is killed and the run continues. | This is workload execution, not launch or authoritative drain. The Unix process-group path does not establish a recursive emptiness proof. ([timeouts](https://mutants.rs/timeouts.html), [27.1.0 changelog](https://mutants.rs/changelog.html#2710), [24.7.1 reversal](https://mutants.rs/changelog.html#2471), accessed 2026-08-23.) |
| StrykerJS v9.6.1 | A mutant run gets `net time × 1.5 + 5000ms + measured overhead`; its factor and constant are configurable. The initial dry run has a separate configurable five-minute timeout. | The test runner is killed/recovered and the mutant is classified `Timeout`. | Separate scopes are useful precedent; the JS values are not portable supervision measurements. ([pinned configuration](https://github.com/stryker-mutator/stryker-js/blob/v9.6.1/docs/configuration.md#timeoutms-number).) |
| PIT 1.25.8 | Per-test stuck detection uses normal runtime × 1.25 + 4000ms; both terms are configurable. | The test/mutant is classified stuck. | The official surface defines no launch or authoritative drain timer. ([official Maven options](https://pitest.org/quickstart/maven/), [first-party releases](https://github.com/hcoles/pitest/releases), accessed 2026-08-23.) |
| cargo-nextest 0.9.143 | Test termination is opt-in. Its force-kill grace defaults to 10s and is configurable. A separate inherited-output `leak-timeout` defaults to 100ms and is configurable per test. | Unix escalates to `SIGKILL`; Windows terminates a Job. The leak timer merely stops waiting on inherited output handles. | The documentation says leak detection misses detached descendants. Its 100ms is not an emptiness bound. ([timeout reference](https://nexte.st/docs/configuration/reference/#timeout-configuration), [leaky tests](https://nexte.st/docs/features/leaky-tests/), [0.9.143 changelog](https://nexte.st/changelog/#cargo-nextest-09143).) |
| Bazel Test Encyclopedia, accessed 2026-08-23 | Test execution uses size-based timeout categories and permits rule/CLI overrides. | A signaled test cannot pass, but the root exit may remain authoritative while children live. | Bazel explicitly accepts a weaker stray-child contract and restricts tests from creating sessions/process groups; Ooze accepts opaque commands. ([normative test specification](https://bazel.build/reference/test-encyclopedia).) |
| Go 1.26.6 `os/exec` | `CommandContext` delegates execution cancellation to the caller context and defaults to direct-child `Kill`. `WaitDelay` is caller-selected and defaults to zero, meaning no additional bound. `Start` has no timeout surface. | `WaitDelay` may kill the direct child and close I/O pipes, returning `ErrWaitDelay`. | There is no standard-library number or descendant-domain proof to copy. ([pinned `Cmd` contract](https://github.com/golang/go/blob/go1.26.6/src/os/exec/exec.go#L238-L310), [pinned `CommandContext`](https://github.com/golang/go/blob/go1.26.6/src/os/exec/exec.go#L475-L504).) |
| HashiCorp go-plugin v1.7.0 | Plugin readiness has a one-minute default and caller override. | Startup failure defers force-kill. Shutdown gives the owned plugin a fixed two-second cooperative wait before runner kill. | This is the closest launch precedent, but its handshake includes application readiness and its cleanup is not an Ooze-style portable census. ([pinned configuration](https://github.com/hashicorp/go-plugin/blob/v1.7.0/client.go#L128-L177), [pinned default](https://github.com/hashicorp/go-plugin/blob/v1.7.0/client.go#L359-L366), [pinned startup enforcement](https://github.com/hashicorp/go-plugin/blob/v1.7.0/client.go#L677-L690), [pinned kill path](https://github.com/hashicorp/go-plugin/blob/v1.7.0/client.go#L454-L528).) |

The test-runner range—unbounded, 100ms, 2s, 10s, and minutes—is evidence of
different obligations, not evidence for averaging them into an Ooze value.

## Durable service and container supervisors

These systems provide stronger containment, but their ownership model is not
Ooze's. PID 1, dockerd/containerd, kubelet, or a shim remains alive after a
client request times out and can continue reconciliation.

| Source | Resolved policy | Forced termination and observation | Limit of comparison |
| --- | --- | --- | --- |
| systemd v260.2, revision `f1d0952` | Build defaults for service start and stop are 90s; manager-wide and per-unit overrides may be finite or infinite. Runtime maximum is separate. | The default control-group stop escalates to `SIGKILL`. No separate numeric post-`SIGKILL` drain timeout is documented; PID 1 retains cgroup custody. | The 90s includes service readiness and cooperative shutdown, not only kernel termination observation. ([build default](https://github.com/systemd/systemd/blob/f1d0952a125b96b7ab2f1ff29a87448ade8ac29b/meson_options.txt), [service timers](https://github.com/systemd/systemd/blob/f1d0952a125b96b7ab2f1ff29a87448ade8ac29b/man/systemd.service.xml), [kill policy](https://github.com/systemd/systemd/blob/f1d0952a125b96b7ab2f1ff29a87448ade8ac29b/man/systemd.kill.xml).) |
| Docker docs and Moby revision `b985e23` | Cooperative stop defaults to 10s on Linux and 30s on Windows and can be set per container or request, including infinite. | The current force path waits for a not-running event for 10s (75s on Windows), direct-kills, performs a final two-second wait, then returns an error rather than claiming exit. | dockerd/containerd remains the durable owner. Its public timeout mostly grants application grace; its private final wait is closer to Ooze's concern but still observes a stronger container runtime. ([Docker stop contract](https://docs.docker.com/reference/cli/docker/container/stop/), [pinned stop path](https://github.com/moby/moby/blob/b985e2313a8676e18072b442911fc8f64f17d440/daemon/stop.go), [pinned kill path](https://github.com/moby/moby/blob/b985e2313a8676e18072b442911fc8f64f17d440/daemon/kill.go).) |
| Kubernetes v1.36.2, revision `24e2b02` | Kubelet's configurable `runtimeRequestTimeout` defaults to 2m. `StartContainer` gets that bound and `RunPodSandbox` twice it. Pod termination grace defaults to 30s. | `StopContainer` receives grace plus the runtime request timeout; expiry is an error. Force deletion may remove the API object without confirming node termination. | The durable kubelet/runtime keeps reconciling. Force deletion's identity trade-off is unsafe for Ooze. ([pinned CRI client](https://github.com/kubernetes/kubernetes/blob/24e2b02af5543d7910c2bb074c7264df5a8f0467/staging/src/k8s.io/cri-client/pkg/remote_runtime.go), [v1.36 pod lifecycle](https://v1-36.docs.kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/).) |
| containerd v2.3.4, revision `db88095` | The caller supplies cooperative grace; the outer RPC context supplies the final bound. | After `SIGKILL`, containerd waits for `Stopped` until that context ends and returns an error on expiry. | A daemon and shim retain custody; an embedding test library does not have that owner. ([pinned stop path](https://github.com/containerd/containerd/blob/db8809540e1a7a9da5d518876894933ff55692ab/internal/cri/server/container_stop.go).) |
| runc v1.5.1, revision `8f2685a` | No built-in numeric readiness or drain policy. | It supplies kill mechanisms, using group/cgroup facilities where available. | It is a one-shot mechanism layer; the higher-level owner supplies waiting and policy. ([pinned kill command](https://github.com/opencontainers/runc/blob/8f2685a471d3347a686ad3909783d8aafc6bb208/kill.go), [first-party changelog](https://github.com/opencontainers/runc/blob/8f2685a471d3347a686ad3909783d8aafc6bb208/CHANGELOG.md).) |
| Windows Job Objects | `TerminateJobObject` has no grace or numeric timeout. `WaitForSingleObject` requires the caller to choose milliseconds or `INFINITE`. | Job handles retain ownership; `KILL_ON_JOB_CLOSE` can bind termination to the last handle. We infer that termination request and accounting emptiness remain distinct observations; the APIs document actions and waits, not their equivalence. | Windows supplies mechanism, not a portable policy number. ([Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects), [`TerminateJobObject`](https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-terminatejobobject), [`WaitForSingleObject`](https://learn.microsoft.com/en-us/windows/win32/api/synchapi/nf-synchapi-waitforsingleobject).) |

The Moby force path is the closest evidence for a small private post-kill wait,
but one implementation's two-second choice is not a cross-platform
measurement. Kubernetes' two minutes and systemd's 90 seconds mostly budget
RPC, readiness, or cooperative application work. Copying any of them would
mislabel policy as measurement.

## Policy alternatives

| Alternative | Assessment |
| --- | --- |
| Put one numeric constant in #61 | Reject. No comparable measures the same obligation across Ooze's three platforms, and a contract number would make later evidence unnecessarily breaking. |
| Give launch, local drain, and emergency drain three public options | Reject for the first slice. These are supervisor mechanics, not ordinary project workload choices; three knobs expose the shallow implementation surface and weaken deterministic defaults. |
| Use only a caller context or `testing.T` deadline | Reject. It can be absent or exhausted before cleanup and cannot authorize abandoning a live obligation. Cancellation should trigger stopping, not erase the supervisor's cleanup budget. |
| Derive supervision bounds from the mutation baseline | Reject. Baseline duration predicts project command work, not OS launch handshakes, force termination, reaping, census, or fatal-ledger settlement. |
| One private fixed number for all three instants | Reject as a contract rule. Launch and drainage are different mechanisms; equality would be accidental coupling. |
| Central private resolver; distinct launch and drain policy classes; three resolved absolute instants | Recommend. It keeps arithmetic and defaults out of native adapters, provides exact replay inputs, and does not expose policy until field evidence shows users need control. |

The production integration will eventually need concrete defaults. That does
not make their values part of #61's attempt contract. Resolve defaults once
into immutable internal configuration, inject the same resolved policy into
deterministic simulation, and test boundary events without sleeping. If field
evidence later shows that unusual hosts need control, a narrow
supervisor-construction override can be added without putting timing mechanics
into every attempt specification.

## Independent-specialist disagreement and reconciliation

The mutation/test-runner pass recommended two supervisor duration policies:
launch progress and drainage. The service/container pass described three
semantic bounds: launch, local drain, and outer emergency.

Both are correct at different layers. The state machine needs three absolute
instants because they begin at different logical events and must replay
independently. The policy resolver does not yet need three independently
configurable durations: local and emergency operations perform the same
authoritative-drain obligation, while the emergency scope is concurrent and
gets a fresh epoch. A future measurement may justify a different emergency
duration, but #61 should neither forbid that nor invent it now.

## Answer for #61

Accept the substance of Q20 with this sharper wording:

> Every launch, local drain epoch, and runtime-emergency epoch receives a
> positive resolved absolute deadline. Launch progress and drainage remain
> separate policy classes; local and emergency drainage use the same policy
> class but separate absolute epochs. The contract fixes no numeric default,
> exposes no per-attempt supervision knob, and never treats expiry as proof of
> emptiness. Exact resolved instants are trace input for deterministic replay.

This is the simple path because it preserves the three intrinsic lifecycle
boundaries without creating three public controls or pretending that unrelated
industry grace periods measure Ooze's stronger obligation.
