# Cross-platform acceptance-gate evidence

_Captured 2026-08-25 for [issue #65](https://github.com/gtramontina/ooze/issues/65). This is an in-progress landing artifact, not authority to close the issue._

## Candidate and dependency history

- Candidate revision: `05378622f971a94e702a9038a4c15a590de0e33f`.
- The candidate descends from completed #66 revision
  `ac964ed7027b784877c7a4e89808405ae4209000`.
- Merge `cf911e99def0b86540a2d83443da350f5efea483` retains both #66 and the signed
  #64 decision artifact `638a0f67d5b91d2c894b88262222ca6bbb06aebd` as parents.
- `git verify-commit` reported a good signature from RSA key
  `11F40C561EA67DE952A652E704D8737E49D558DE` for every #65 commit through the
  candidate.

## Native evidence

[OS Compatibility run 32793370083](https://github.com/gtramontina/ooze/actions/runs/32793370083)
ran Go 1.26.6 on Ubuntu 24.04 amd64, macOS 26 arm64, and Windows Server 2025
amd64 at the candidate revision.

| Lane | Result | Observation |
| --- | --- | --- |
| Ubuntu native acceptance | pass | Ran after the ordinary suite failed; includes the Linux per-shape matrix and production census seam. |
| macOS native acceptance | pass | Ran after the ordinary suite failed; includes `TestDarwinCensusInstrumentsPerDescendantShape`. |
| Windows native acceptance | pass | Ran after the ordinary suite failed; includes accounting, breakaway, nesting, and exact retained-handle kill-on-close evidence. |
| Ubuntu 10-repeat stress | pass, 2m31s | Bounded race/shuffle native set. |
| macOS 10-repeat stress | pass, 58s | Bounded race/shuffle native set. |
| Windows 10-repeat stress | pass, 2m3s | Bounded race/shuffle native set. |

The required aggregate is correctly **red**. Each ordinary OS suite timed out
only in `TestSupervisorDriverReadyEarlierRootExitBeatsDeadlineAndSamples`.
That regression is inherited from #67, was reproduced at `2bd4a97`, has no
waiver authority, and blocks landing even though every focused native lane is
green.

## Focused and supplemental evidence

- Native Darwin `test.acceptance`: 12 tests pass, including all four Darwin
  census shapes.
- Live Linux arm64 container: corrected per-shape matrix passes 10/10 with
  shuffle seed `1787616897629497697`.
- Full `internal/ooze` race/coverage/shuffle suite and the repository test gate
  pass locally through Devbox.
- #64 nested simulation module passes with race enabled; `Explore`, legal and
  violation replay, and shrink remain separately callable.
- Isolated `golangci-lint run --no-config ./...`: 0 issues.
- Cross-compilation passes for Linux amd64, Darwin amd64/arm64, Windows
  amd64/arm64, and unsupported sentinel Plan 9 amd64.

## Deliberate broken-instrument evidence

At candidate test shape, `nativeDomainEmpty` was deliberately changed to
return `true` without reading the Linux guardian. The command was compiled
through Devbox and run on a live Linux arm64 kernel. All four matrix rows failed
at the post-root observation (`Linux matrix did not observe the post-root
subreaper state`) under shuffle seed `1787617568240919997`. Restoring the exact
production instrument returned the matrix to green and left no production
diff.

The unchanged #66 absence fixtures retain their named proof: enabling Windows
breakaway fails the required `ERROR_ACCESS_DENIED` evidence in
[run 32731491570](https://github.com/gtramontina/ooze/actions/runs/32731491570),
and disabling Windows Job termination fails nested containment in
[run 32732358150](https://github.com/gtramontina/ooze/actions/runs/32732358150).
The new kill-on-close fixture additionally proves positive subject liveness
before close and authoritative exit afterward through a separately retained
process handle; `CloseHandle` success alone is not its oracle.

## Outstanding landing dependencies

1. Integrate the separately owned #67 timing repair and obtain a fully green
   required OS Compatibility run.
2. Have #72 collect the reviewed performance matrix: at least ten interleaved
   A/B samples per native platform, raw distributions, and environment.
3. Repeat independent parallel Standards and Spec review on the integrated
   candidate and resolve all material findings.

Until all three complete, issue #65 stays open and its Project status must not
be Done.
