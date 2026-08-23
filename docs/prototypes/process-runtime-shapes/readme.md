# Process-runtime API shape

This cheap prototype compared nine typed transition methods with one sealed-event
`Advance` method over the same value state and the same seven ordered scenarios.
Construction is separate: the runtime therefore has nine dispatchable event
families and ten total core entry points.

The typed-method shape won. It keeps each input/output correlation at the call
site and lets complex observations deepen behind their own helpers. The sealed
shape added nine event wrappers, nine marker methods, a central type switch, and
a wide result correlation. Its rejected `advance.go` was deleted before the
production implementation began.

## Recorded comparison

All measurements used the uncommitted isolated prototype tree rooted at signed
commit `14c532d389b6cbff05af4a93dfb434c67f8196b5`, on Darwin 25.5.0 arm64.

- Correctness artifact: both prototype implementations plus `shapes_test.go`.
  `env GOCACHE=/private/tmp/ooze-62-go-cache devbox run -- go -C
  docs/prototypes/process-runtime-shapes test -count=20 ./...` passed once,
  exercising 2 shapes × 7 scenarios × 20 repetitions = 280 scenario runs.
- Size artifact: `methods.go` and the subsequently deleted `advance.go`.
  `wc -l docs/prototypes/process-runtime-shapes/methods.go
  docs/prototypes/process-runtime-shapes/advance.go
  docs/prototypes/process-runtime-shapes/shapes_test.go` ran once and reported
  333, 343, and 176 lines respectively. `git diff --no-index --stat
  docs/prototypes/process-runtime-shapes/methods.go
  docs/prototypes/process-runtime-shapes/advance.go` ran once and reported 218
  insertions and 208 deletions between shapes.
- Complexity artifact: both implementation files. `env
  GOCACHE=/private/tmp/ooze-62-go-cache devbox run -- sh -c 'cd
  docs/prototypes/process-runtime-shapes && golangci-lint run --no-config
  --enable-only funlen,cyclop ./...'` ran once. The sealed central `advance`
  measured cyclomatic complexity 82; the largest typed transition in the
  deliberately undecomposed prototype measured 21. These are static
  single-sample statistics, not timing measurements.

Line count did not decide the result. Ownership locality, precise return types,
and local deepening did. Production retains one pure process-runtime state core;
typed methods do not create additional reducers.

## Start-installation seam correction

The API-shape result remains unchanged, but its first production application exposed a separate
counterexample: a private generation-installer callback could capture and re-enter the broker while
the runtime mutex was held. That callback seam is rejected and deleted. Production instead accepts
a private concrete `startInstallation` containing only inert exact grant identity and a pending cell;
broker-owned code validates that identity and installs its generation under the lock before returning
the narrower `installedStart`. The native thunk is not an
input to the locked operation at all: it can be supplied only to `installedStart` after return. Go
still requires runtime rejection of zero, copied, or reused installation values, but native work is
absent from the pre-installation call path.

The returned `installedStart` carries the exact generation and a private runtime fatal guard. It
claims the shared cell before native work, so cross-pairing or copying that post-return capability
cannot execute the wrong thunk or execute it twice, and passes the exact installed generation into
the post-unlock thunk. Validation failure is not an ignorable return:
the guard closes and transfers residual custody before panicking. This dynamic one-shot assertion is
the minimal compensation for Go's lack of linear values; it is separate from the static phase boundary.
The core's candidate prospective state is published only after cell installation succeeds, making
malformed or reused installation failure atomic with respect to emergency custody.

The final fatal-return traces also forced one production refinement after the comparison: ordinary
late cancellation and the authority-bearing acknowledgement of an emitted known-grant return are
different transitions. Production therefore has ten dispatchable semantic method families plus
construction, while this recorded two-shape prototype compared the original nine-family contract.
Folding the acknowledgement back into cancellation would make stale or wrong return authority an
ignorable expected result, so the extra typed method is retained. It strengthens rather than reverses
the shape choice: an `Advance` form would now require ten central arms and a wider correlated result.

The shell supplies the one broadcast that is deliberately not a per-request one-shot: one
process-wide receive-only emergency channel, closed once at the `Open` to fatal transition. This
wakes registered campaigns that have no current admission waiter and makes retained known-grant
return authority discoverable even when an invariant guard must re-panic instead of returning a
closure value. Later fatal causes join without replacing or closing the channel again.
Attempt-originated seeds retain their exact generation in ingress order, so two equal containment
kinds do not collapse and a duplicate observation for one generation remains an invariant.
Post-unlock launch invariants are likewise generation-attributed and are recorded once: guarded
closure returns and unlocks before the original invariant is re-panicked.
Emergency-transferred prospective custody is final for runtime accounting: a later no-release report
cannot delete it or turn `ClosedUnconfirmed` into `ClosedDrained`.
Conversely, a still-unresolved prospective that is later proven not released settles normally and
derives `ClosedDrained` when it was the final fatal obligation.

The supporting language and analyzer research is recorded in
`docs/research/static-non-reentrant-start-installation.md`. This correction does not alter the
single-core typed-method choice or introduce #67 native supervision.
