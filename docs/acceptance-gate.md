# Managed mutation execution acceptance gate

This is the landing policy resolved by [Set the cross-platform acceptance
gate](https://github.com/gtramontina/ooze/issues/65). It defines evidence; it
does not implement campaign work owned by #68, #69, or #70, or collect the
final landing matrix owned by #72.

## Evidence rule

A required observation is either conclusive and green or it blocks landing.
An unreadable identity, unstable enumeration, Job query or handle failure,
fixpoint expiry, unexpected skip, unavailable required runner, or unexecuted
supported platform is **inconclusive**. Preserve its diagnostics, but never
convert it to a pass or a manually waived green check. Root exit, a successful
termination request, process-group `ESRCH`, deadline expiry, or handle release
never substitutes for authoritative emptiness.

The native supported set is Linux on the repository runner architecture,
Darwin on the available GitHub-hosted architecture, and Windows AMD64 (the
Windows race detector does not support ARM64). Maintained additional
architectures are cross-compiled. The unsupported sentinel target is compiled
and must reject supervisor construction before admission, launch, or native
work; it is not treated as a fourth native implementation.

The documented Darwin direct-root/pre-freeze and check-to-signal identity
windows remain platform limits. A fixture that positively constructs one of
those limits is conclusive evidence of the limit, not an inconclusive skip and
not evidence of containment.

## Automated cadence

Every pull request requires:

1. the normal race, coverage, and shuffled suite on native Linux, Darwin, and
   Windows AMD64;
2. one independently bounded `test.acceptance` pass on each of those kernels;
3. the pure reducer, driver, replay, violation, semantic shrink, and production
   conformance suite that exists at that revision;
4. Linux/Darwin/Windows and unsupported-sentinel cross-compilation; and
5. isolated `golangci-lint run --no-config ./...` evidence.

The existing mutation workflow remains a post-CI `main` gate across Linux,
Darwin, and Windows. Before the final landing decision, #72 runs it against the
exact candidate revision and also supplies isolated deliberate
broken-instrument proof for every affected fixture or production path.

The ten-repeat `test.adversarial.stress` target runs on pushes to `main`, once
weekly, and by manual dispatch. Pull requests deliberately run one bounded
pass. #72 may change that cadence only with measured evidence, not preference.

The full suite is indivisible landing evidence. A focused command may isolate
a fixture, but cannot waive another failure. In particular,
`TestSupervisorDriverReadyEarlierRootExitBeatsDeadlineAndSamples` was recorded
as inherited from the signed #67 tip. It remains a blocking regression until a
complete gate passes or binding authority explicitly reclassifies it.

## Native visibility matrices

Each row proves the subject's exact identity. Columns are native instruments,
not portable aliases for one platform's mechanism, and are observed with the
attempt root alive and exited.

### Darwin

The existing Darwin matrix keeps separate columns for the process-group
census, the parent-identity walk from the root, and the parent closure seeded
from live group members. It covers a plain child, double-forked orphan,
direct-root escapee, and escapee behind a live group member in both root
states. Deliberately conflating the instruments must make the affected cells
red.

Native drain evidence additionally proves this order through the real
supervisor: capture live group members and their parent closure before any
destructive kill; freeze the group and each captured escapee; enumerate and
freeze to a fixpoint under one absolute bound; then kill by group and by
captured PID plus birth identity. Every by-PID action immediately revalidates
birth identity. Mismatch or unreadability signals nothing and cannot prove
drainage. Generation/action correlation, capture-before-kill,
freeze-before-fixpoint, identity reuse, and the green behind-live-member shape
are mandatory.

### Linux

The Linux matrix constructs the per-shape column rather than translating the
Darwin one. Its exact depth-one subreaper/`wait4` visibility is:

| subject | waitable, root alive | waitable, root exited |
| --- | --- | --- |
| plain child | no | yes |
| double-forked session orphan | yes | yes |
| session escapee parented by root | no | yes |
| session escapee behind live middle | no | no; middle is the visible seed |

The live-root parent walk is retained as a separate automatic-fuse instrument:
after the exact PPid instrument confirms orphan adoption, it sees the plain
child and both live-ancestry escapees, misses the adopted subject, and sees none
after root exit. Repeated depth-one kill/reap sweeps must reveal and reap the
final row after its middle dies, then reach `ECHILD`. Stopping after one sweep
or replacing adoption with a root walk is the required deliberate mutation.

### Windows

Job membership/accounting must contain the exact direct child, deep
descendant, and nested-Job descendant both before and after root exit.
Terminated roots may remain represented while handles exist, so root state is
established by the exact retained root handle rather than inferred from a
counter disappearing. The gate separately requires denied
`CREATE_BREAKAWAY_FROM_JOB` with `ERROR_ACCESS_DENIED`, nested-Job termination,
transitive parent-Job accounting, and kill-on-close handle-lifetime evidence.
Successful `TerminateJobObject` or `CloseHandle` is not by itself an emptiness
observation; a confirming Job query and retained subject handle are required.

## Simulation and mutation evidence

The accepted #64 module has four gate seams: `Explore`, `ReplayLegal`,
`ReplayViolation`, and `Shrink`. Ordinary exploration emits only enabled legal
facts. Violation replay injects exactly one typed malformed fact after a legal
prefix, performs deterministic cleanup, and re-panics the original invariant.
Semantic shrinking removes records and definition members to a fixpoint while
preserving the alpha-normalized `FailureKey`, then replays the result twice.

Conformance uses the production campaign, runtime/broker, and supervisor
transitions. It compares exact owner state and ordered effects at every record,
and complete composed state only at recorder-controlled quiescent barriers and
terminal settlement. Only clocks, native observations, filesystem, and output
are faked. Go byte minimization, final-result equality, or a second model
implementation is insufficient.

Focused traces include all #61/#64 boundaries: launch and deadline
before/equality/after; start versus closure; late grant, release revocation and
adoption; stale/duplicate generation and action facts; multiple provisionals
and FIFO barriers; full-to-single admission; terminal versus fatal commitment;
empty and residual emergency settlement; mixed invariant/containment
dominance; immutable output cutoff; and ordered prospective/owned residuals.

Every absence-oriented fixture has named broken-instrument proof. Historical
proof remains valid only while its subject construction, assertion,
instrument, and exercised production path are unchanged. A change to any of
those regenerates the red artifact. The artifact names the mutation, expected
red rows, observed failure, platform, revision, and command.

## Reviewed performance evidence

Timing is reviewed landing evidence, not a hard CI percentage threshold.
Stable structural regressions remain automated: healthy execution adds no
probe, calibration, retry, or recovery command; automatic work begins at `P`;
active leases are not revoked; one provisional receives at most one exclusive
confirmation; and drain work remains one bounded concurrent epoch.

#72 collects the final Linux, Darwin, and Windows A/B matrix. For each platform
it records at least ten interleaved samples per revision, raw distributions,
command and tree identity, architecture, Go version, race mode, runner image,
and whether a claim is measured or argued. It reports healthy wall time,
attempt throughput, peak process count, and memory separately from confirmation
count/time and cleanup escalation. No noisy timing delta can waive a
correctness failure or become a new threshold without a later explicit
decision.
