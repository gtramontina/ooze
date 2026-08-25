# Outcome reporting prototype

Throwaway artifacts backing [Define user-visible outcome and scoring
semantics](https://github.com/gtramontina/ooze/issues/63). Both files carry `//go:build
ignore`, so they are excluded from `go build ./...`, from linting, and — per the
build-constraint rule in source discovery — from Ooze's own mutant catalogue.

The production projection now lives in `internal/ooze/managed_report.go`. The prototype remains the decision artifact; the current user-facing sample is `.assets/report.txt`.

## `render.go`

Renders every user-visible presentation the resolution fixes, against realistic counts
from Ooze's own suite: a scored `Completed` above and below threshold, the `Serial()`
variant with the unobservable `runaway` row omitted, `NoMutants`, a baseline-failure
abort, a mid-run abort, and both fatal diagnostics.

    go run render.go

## `threshold.go`

The measurement behind keeping `float32`. Sweeps the true decision boundary for each
threshold — the smallest `detected` with `detected/total >= threshold`, and one either
side — rather than only totals where the ratio is exactly representable. That sampling
error is what made an earlier reading of this measurement wrong.

    go run threshold.go

It reports, for each threshold, the first `total` at which the production comparison
`float32(detected)/float32(total) < threshold` disagrees with exact rational comparison,
under both readings of "exact": against the decimal the user typed, and against the
`float32` actually stored. The second reading diverges at `total = 10` and is the wrong
target — the production path is the one that matches user intent.

The sweep stops at two million totals, so thresholds that are exactly representable in
`float32` — including the default `1.0` — report `none`. Their true first divergence is
16,777,217, one past the point where `float32` stops representing consecutive integers.
