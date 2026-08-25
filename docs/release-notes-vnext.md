# Managed mutation execution

This release replaces caller-coordinated mutation subtests with synchronous managed campaigns.

## User-visible changes

- Automatic campaigns begin at the process's detected Go concurrency, coordinate aggregate admission across concurrent `Release` calls, and use `GOMAXPROCS=1` for each attempt root.
- Trustworthy capacity pressure irreversibly moves automatic execution to one admitted attempt at a time for the remainder of the process. Reports announce the transition.
- `Parallel()` is removed. Parameterless `Serial()` runs attempts process-locally exclusively while preserving the detected-capacity cooperative profile.
- `WithMutationTimeout(duration)` is the sole absolute mutation-deadline override. Without it, one passing baseline resolves the campaign's mutation deadline. Confirmations reuse their primary's full deadline.
- Reports are one deterministic inline projection in catalogue order. Per-mutant `t.Run` subtests and cleanup-time reporting are removed.
- Only `Completed` publishes `detected / total`, where detected is `Killed + TimedOut + Runaway`. `NoMutants` and `Aborted` fail without a score. Cleanup or invariant faults print one consolidated diagnostic and panic once.
- `Virus` remains public. Runner, reporter, supervisor, broker, and driver infrastructure remain internal.

## Score movement

Baseline-derived deadlines can move mutation scores without an API change because the deadline decides whether a long-running mutant is `TimedOut` or `Survived`. Tightening a deadline can move survivors into detected `TimedOut` and raise the score; loosening it can move timeouts into `Survived` and lower the score. Projects close to `WithMinimumThreshold` should review score changes when upgrading. Use `WithMutationTimeout` only when an absolute project-specific deadline is intended.

An opaque test command may enforce its own shorter timeout. If that inner timeout exits non-zero before Ooze's deadline, the mutant remains `Killed` rather than `TimedOut`. Both are detected, so this changes diagnostics but not the score.

## Output visibility

Ooze writes the report to `stdout` before `Release` returns. Go may discard stdout from a passing `go test ./...` invocation without `-v`; use `go test -v` when the report must be retained. Per-mutant test rows in `go test -json`, JUnit output, CI annotations, and IDE test trees are intentionally no longer produced.
