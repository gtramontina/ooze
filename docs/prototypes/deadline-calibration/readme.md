# Deadline calibration harness

Throwaway prototype built for
[Calibrate baseline-derived mutation deadlines](https://github.com/gtramontina/ooze/issues/59).
It measures the one ratio a baseline-derived deadline depends on: how much longer the slowest
legitimate mutant attempt takes than the single baseline attempt the formula is allowed to
observe.

It reproduces a campaign's shape rather than benchmarking configurations independently,
because the quantity of interest is contaminated by everything that happens between the
baseline and the mutants — above all the Go build cache, which the baseline itself warms.
One invocation is one campaign.

```sh
go build -o calib .

# Automatic profile: GOMAXPROCS=1 per attempt, 14 concurrent.
./calib -src /path/to/snapshot -work /tmp/calib -runid a1 \
        -profile automatic -capacity 14 -mutants 14 -cmd 'go test -count=1 ./...'

# Serial profile: GOMAXPROCS=14, one attempt at a time.
./calib -src /path/to/snapshot -work /tmp/calib -runid a2 \
        -profile serial -capacity 14 -mutants 5 -cmd 'go test -count=1 ./...'
```

`-runid` must differ between invocations. It makes every workspace path unique, including the
baseline's. This is not cosmetic: `cmd/go/internal/work/exec.go` writes the absolute package
directory into the build action ID whenever `-trimpath` is absent, so a reused baseline path
collects build-cache hits that fresh-path mutants never get. An earlier version of this
harness reused one baseline directory and inflated every measured ratio by roughly 2-3x.

Findings are recorded in
[`docs/research/baseline-derived-deadlines.md`](../../research/baseline-derived-deadlines.md).
