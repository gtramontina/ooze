# Cross-platform acceptance-gate evidence

_Captured 2026-08-25 for [issue #65](https://github.com/gtramontina/ooze/issues/65)._

## Candidate and dependency history

- Tested revision: `cf2589171f8d55ac6ddbfd3c6535fa32e8ccede9`.
- The candidate descends from completed #66 revision
  `ac964ed7027b784877c7a4e89808405ae4209000`.
- Merge `cf911e99def0b86540a2d83443da350f5efea483` retains both #66 and the signed
  #64 decision artifact `638a0f67d5b91d2c894b88262222ca6bbb06aebd` as parents.
- `git verify-commit` reported a good signature from RSA key
  `11F40C561EA67DE952A652E704D8737E49D558DE` for every #65 and integrated
  repair commit through the tested revision.

## Native evidence

[OS Compatibility run 32796096480](https://github.com/gtramontina/ooze/actions/runs/32796096480)
ran Go 1.26.6 on Ubuntu 24.04 amd64, macOS 26 arm64, and Windows Server 2025
amd64 at the tested revision. Every ordinary suite, bounded native pass,
ten-repeat stress lane, and the required aggregate passed.

| Lane | Result | Observation |
| --- | --- | --- |
| Ubuntu ordinary + native acceptance | pass, 1m0s | Includes the Linux per-shape matrix, production census seam, and repaired observation-ordering regression. |
| macOS ordinary + native acceptance | pass, 1m14s | Includes `TestDarwinCensusInstrumentsPerDescendantShape`. |
| Windows ordinary + native acceptance | pass, 1m47s | Includes accounting, breakaway, nesting, and exact retained-handle kill-on-close evidence. |
| Ubuntu 10-repeat stress | pass, 2m26s | Bounded race/shuffle native set. |
| macOS 10-repeat stress | pass, 1m14s | Bounded race/shuffle native set. |
| Windows 10-repeat stress | pass, 2m3s | Bounded race/shuffle native set. |
| Required aggregate | pass, 4s | All supported operating systems completed successfully. |

The previously inherited
`TestSupervisorDriverReadyEarlierRootExitBeatsDeadlineAndSamples` regression
was integrated as its five separately signed repair commits. It passed in all
three ordinary suites; no waiver was used.

## Focused and supplemental evidence

- Native Darwin `test.acceptance`: 12 tests pass, including all four Darwin
  census shapes.
- Live Linux arm64 container: corrected per-shape matrix passes 10/10 with
  shuffle seed `1787616897629497697`.
- The focused regression passed with race detection for 20 repetitions; the
  broader supervisor driver/reducer set passed 10 shuffled race repetitions.
- The full repository test gate passed locally through Devbox: 879 tests, one
  longstanding skip, 11.031s.
- #64 nested simulation module passes with race enabled; `Explore`, legal and
  violation replay, and shrink remain separately callable.
- Isolated `golangci-lint run --no-config ./...`: 0 issues.
- Cross-compilation passes for Linux amd64, Darwin amd64/arm64, Windows
  amd64/arm64, and unsupported sentinel Plan 9 amd64.

## Deliberate broken-instrument evidence

At the final Linux matrix shape, `nativeDomainEmpty` was deliberately changed to
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

## Allocated landing evidence

Issue #72 owns collection of the reviewed performance matrix at final landing:
at least ten interleaved A/B samples per native platform, with raw distributions
and environment recorded. Issue #65 deliberately supplies the reusable command
and policy rather than duplicating that collection or creating a statistical
framework. Independent Standards and Spec review is performed against the
actual final #65 diff and published on the issue before closure.
