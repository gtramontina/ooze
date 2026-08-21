# Go test parallelism under Ooze automatic mode

Research date: 2026-08-21

Tested toolchain: Go 1.26.6 (`darwin/arm64`), through this repository's Devbox environment

Question: what does setting `GOMAXPROCS=1` on an Ooze test-command process actually constrain, especially when the command contains explicit Go test flags or a wrapper?

> **Decision status:** Issue [#58](https://github.com/gtramontina/ooze/issues/58) retained the
> cooperative `GOMAXPROCS=1` automatic profile and resolved outer admission to exactly two states:
> start at aggregate `P`, then irreversibly enter single-admission automatic after trustworthy
> pressure. There is no ramp or numeric backoff. Explicit child overrides remain accepted as part of
> the opaque command contract; process-fuse behavior and future `OOZE_*`/selection features are not
> part of this decision.

## Finding

`GOMAXPROCS=1` is valuable, but it is **cooperative per-process shaping, not a one-CPU sandbox around an attempt**.

With no override, it collapses all three Go defaults relevant to a normal `go test ./...` invocation:

- the `go` command itself executes user-level Go code on at most one CPU;
- `go test` defaults `-p` to one, so it runs at most one build command or test binary at a time; and
- each generated test binary defaults `-parallel` to one.

Those defaults come from three separate mechanisms. `cmd/go` initializes its build-process limit from its own `runtime.GOMAXPROCS(0)`, while each test binary separately initializes its `-parallel` limit from its own runtime value. [The public `-p` contract](https://pkg.go.dev/cmd/go@go1.26.6#hdr-Compile_packages_and_dependencies) and [Go 1.26.6's initialization](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/cfg/cfg.go#L87) make the first relationship explicit; [`testing.Init`](https://github.com/golang/go/blob/go1.26.6/src/testing/testing.go#L477-L478) implements the second.

An explicit override breaks only the layer it controls:

- `-p=4` permits four build commands or package test binaries even though every resulting Go process independently has `GOMAXPROCS=1`.
- `-parallel=4` admits four `t.Parallel` test functions inside each test binary, but that binary still executes user-level Go code on at most one CPU at a time.
- `-cpu=4` is stronger: the test harness calls `runtime.GOMAXPROCS(4)` inside the test binary, overriding the environment-derived value for that run.

Therefore, setting `GOMAXPROCS=1` on `go test -p=4` does **not** force the invocation as a whole onto one CPU. It can produce four compiler or test processes, each entitled to one CPU. The Go runtime's contract is explicitly per process: it limits threads executing user-level Go code, not a process tree. It also does not limit threads blocked in syscalls. [Runtime documentation](https://pkg.go.dev/runtime@go1.26.6#hdr-Environment_Variables)

## The five different limits

| Limit | Scope | Default after Ooze sets `GOMAXPROCS=1` | What can override it |
|---|---|---:|---|
| Ooze admission `L` | Concurrent mutation-attempt command trees | Policy-controlled | Ooze mode/policy |
| `go test -p` | Concurrent build commands and package test binaries in one `go` command | `1` | command line or `GOFLAGS` |
| `go test -parallel` | `t.Parallel` functions admitted in one test binary | `1` | command line or `GOFLAGS` |
| runtime `GOMAXPROCS` | Threads executing user-level Go code in one Go process | `1` | `-cpu`, test/application code, or a changed environment |
| OS processes/threads and foreign work | Whole attempt tree | no bound from `GOMAXPROCS` | arbitrary subprocesses, syscalls, cgo, wrappers |

These limits must not be multiplied indiscriminately. `-parallel=4` means four tests may be live, not that four CPUs execute their Go code. Conversely, `-p=4` creates separate processes, so the per-process `GOMAXPROCS=1` entitlement may be exercised four times simultaneously.

### `-p`: package/build process concurrency

The public definition of `-p` is “the number of programs, such as build commands or test binaries, that can be run in parallel”; its default is `GOMAXPROCS`. [Go command documentation](https://pkg.go.dev/cmd/go@go1.26.6#hdr-Compile_packages_and_dependencies)

Internally, `cmd/go` starts `BuildP` workers over its build action graph. [Go 1.26.6 builder source](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/work/exec.go#L216-L245) Explicit `-p=4` changes `BuildP` to four; setting the process environment to `GOMAXPROCS=1` does not take precedence over that flag.

Compiler subprocesses inherit the environment, and Go 1.26.6 also derives its compiler backend-concurrency ceiling from `runtime.GOMAXPROCS(0)`. Thus the individual compiler is shaped to one backend lane, but `-p=4` can still run several compiler processes. [Compiler concurrency source](https://github.com/golang/go/blob/go1.26.6/src/cmd/go/internal/work/gc.go#L184-L234)

The build cache can make the realized process count lower. The limits here are admission ceilings, not promises that enough independent actions always exist.

### `-parallel`: test-function admission

`-parallel=N` controls only test functions that call `t.Parallel` (and related fuzz behavior), within a single test binary. It does not parallelize ordinary tests, cap goroutines that tests create themselves, or constrain other package test binaries. The documentation separately calls out package-level `-p` concurrency. [Testing flags](https://pkg.go.dev/cmd/go@go1.26.6#hdr-Testing_flags) [`T.Parallel` semantics](https://pkg.go.dev/testing@go1.26.6#T.Parallel)

With `GOMAXPROCS=1 -parallel=4`, four parallel tests can be admitted and overlap while sleeping, waiting on I/O, making cgo calls, or launching children. For pure CPU-bound Go work inside that binary, only one thread executes user-level Go code at once.

### `-cpu`: a runtime override, not another admission limit

`-cpu=1,2,4` asks the test harness to execute the suite at each listed `GOMAXPROCS` value. [Testing flags](https://pkg.go.dev/cmd/go@go1.26.6#hdr-Testing_flags) The harness implements this by calling `runtime.GOMAXPROCS(procs)` for each value. [Testing source](https://github.com/golang/go/blob/go1.26.6/src/testing/testing.go#L2553-L2564)

The `-parallel` default is captured earlier, when testing flags are registered. Consequently:

```text
GOMAXPROCS=1 go test -cpu=4
```

runs the package test binary with runtime `GOMAXPROCS=4`, while its default `-parallel` remains `1`. Test-created goroutines may use four CPUs even though only one `t.Parallel` test is admitted. Adding `-parallel=4` raises both independently. A list such as `-cpu=1,4` repeats the suite sequentially for those values; it does not run both copies simultaneously.

### `GOFLAGS` precedence

`GOFLAGS` supplies default flags to Go commands. Command-line flags are applied afterward and override it. [Go environment-variable documentation](https://pkg.go.dev/cmd/go@go1.26.6#hdr-Environment_variables)

Therefore:

- inherited `GOFLAGS=-p=4` or `GOFLAGS=-parallel=4` defeats the corresponding default even when Ooze sets `GOMAXPROCS=1`;
- setting those values to one in `GOFLAGS` would still not override an explicit `-p=4` or `-parallel=4` in `WithTestCommand`; and
- a compiled test binary does not interpret `GOFLAGS` at all—it understands its `-test.*` flags and its runtime environment, not `cmd/go` configuration.

Ooze must not describe `GOMAXPROCS=1` as “forcing” inner parallelism to one. It shapes Go's defaults. A strict guarantee would require understanding and rewriting the opaque command, preventing the program from changing its own runtime, and constraining its whole process tree at the OS level.

## Wrappers, test binaries, grandchildren, and non-Go commands

The environment is applied first to Ooze's direct child. A wrapper such as `gotestsum` normally inherits it and invokes `go test` with the supplied Go flags. `gotestsum` documents that it runs `go test -json`, accepts any `go test` flag after `--`, supports arbitrary raw commands, and can execute compiled test binaries. [gotestsum v1.13.0 documentation](https://github.com/gotestyourself/gotestsum/tree/v1.13.0#custom-go-test-command)

The same conditional inheritance applies to grandchildren. Go's `os/exec` uses the current process environment when `Cmd.Env` is nil. [Go `os/exec` contract](https://pkg.go.dev/os/exec@go1.26.6#Cmd) Thus a Go child spawned normally by a test sees `GOMAXPROCS=1` and independently initializes its runtime to one. This is propagation of a default, not aggregation: ten such grandchildren may each execute one CPU simultaneously. Any wrapper or test can remove/replace the variable, set `Cmd.Env`, call `runtime.GOMAXPROCS`, or invoke a non-Go binary that ignores it.

Running a compiled `pkg.test` binary removes the `cmd/go` layer entirely:

- there is no `-p` layer (`-p` is rejected as an unknown test-binary flag);
- `GOMAXPROCS=1` still initializes that binary's Go runtime;
- `-test.parallel=4` still admits four parallel tests; and
- `-test.cpu=4` still raises its runtime setting to four.

## Syscalls, OS threads, cgo, and subprocesses

`GOMAXPROCS` is not an OS-thread limit. The runtime may create additional threads when goroutines block in syscalls, cgo, or locked-thread work. Its documentation says blocked syscall threads do not count, and the scheduler design has exactly `GOMAXPROCS` logical processors but any number of OS threads. [Runtime documentation](https://pkg.go.dev/runtime@go1.26.6#hdr-Environment_Variables) [Scheduler design](https://github.com/golang/go/blob/go1.26.6/src/runtime/HACKING.md#L19-L38)

cgo deliberately enters syscall state before executing foreign code, placing that work outside Go's `GOMAXPROCS` accounting until it returns to Go. [cgo runtime source](https://github.com/golang/go/blob/go1.26.6/src/runtime/cgocall.go#L134-L184) Multiple C calls can therefore overlap at `GOMAXPROCS=1`, and CPU-intensive foreign code is not a one-CPU guarantee.

Subprocess count is likewise unrestricted by `GOMAXPROCS`. A test can start any number of commands;
non-Go children need not recognize the variable. Ooze can respond only at its own outer-admission
boundary; it cannot retrospectively make an opaque command subtree respect `P`.

## Concrete full-automatic example: `P=4`

Assume Ooze has admitted four mutation attempts and sets `GOMAXPROCS=1` on each attempt command. The
table describes ceilings while all four attempts are active. “Pure-Go CPU” excludes the controlling
`go` processes, cgo, syscalls, and arbitrary grandchildren; it is not an OS-enforced total.

| User test command | Per attempt | Across full automatic `P=4` | Approximate pure-Go child-work ceiling |
|---|---|---|---:|
| `go test ./...` | at most 1 build/test program; 1 admitted `t.Parallel` test per binary | at most 4 build/test programs | 4 CPUs |
| `go test -p=4 ./...` | at most 4 build/test programs, each runtime `1` | at most 16 build/test programs | 16 CPUs |
| `go test -parallel=4 ./...` | at most 1 build/test program; up to 4 admitted parallel tests in its test binary | at most 4 package programs and 16 admitted tests | 4 CPUs for pure Go, despite 16 live tests |
| `go test -p=4 -parallel=4 ./...` | at most 4 package programs × 4 admitted parallel tests | at most 16 package programs and 64 admitted tests | 16 CPUs |
| `go test -cpu=4 ./...` | at most 1 package program; test binary changes runtime to `4`; default parallel remains `1` | at most 4 package programs | 16 CPUs during test execution |
| `go test -cpu=4 -parallel=4 ./...` | at most 1 package program; runtime `4`; 4 admitted parallel tests | at most 4 package programs and 16 admitted tests | 16 CPUs |

This answers the direct question: with `P=4` and an explicit `-p=4`, merely forcing the environment
to `GOMAXPROCS=1` can still allow roughly sixteen child Go processes to execute simultaneously. The
variable limits each process, not their shared subtree. If trustworthy pressure moves the runtime to
single-admission automatic, the same opaque command can still run four such child processes; Ooze
removes its own outer overlap but does not rewrite the user's command.

## Controlled experiments

All commands ran through Devbox. The fixture used four packages whose only test logged PID, timestamp, and `runtime.GOMAXPROCS(0)` before sleeping for one second, plus this parallel-admission probe:

```go
func TestParallelAdmission(t *testing.T) {
	var active, maximum atomic.Int64
	for i := range 4 {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			t.Parallel()
			n := active.Add(1)
			for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
			}
			time.Sleep(250 * time.Millisecond)
			active.Add(-1)
		})
	}
	t.Cleanup(func() {
		t.Logf("parallel-max=%d runtime=%d", maximum.Load(), runtime.GOMAXPROCS(0))
	})
}
```

Representative commands (the scratch module was `/private/tmp/ooze-go-parallelism-experiment`):

```sh
env GOMAXPROCS=1 GOCACHE=/private/tmp/ooze-go-parallelism-gocache \
  devbox run -- go -C /private/tmp/ooze-go-parallelism-experiment \
  test -count=1 -v -run 'Test(Runtime|ParallelAdmission)$' .

env GOMAXPROCS=1 GOCACHE=/private/tmp/ooze-go-parallelism-gocache \
  devbox run -- go -C /private/tmp/ooze-go-parallelism-experiment \
  test -count=1 -v -parallel=4 -run 'Test(Runtime|ParallelAdmission)$' .

env GOMAXPROCS=1 GOCACHE=/private/tmp/ooze-go-parallelism-gocache \
  devbox run -- go -C /private/tmp/ooze-go-parallelism-experiment \
  test -count=1 -v -cpu=4 -run 'Test(Runtime|ParallelAdmission)$' .

env GOMAXPROCS=1 GOCACHE=/private/tmp/ooze-go-parallelism-gocache \
  devbox run -- go -C /private/tmp/ooze-go-parallelism-experiment \
  test -count=1 -v -p=4 ./p1 ./p2 ./p3 ./p4
```

Observed:

| Case | Runtime reported by test binary | Maximum admitted parallel tests | Package-test overlap |
|---|---:|---:|---:|
| defaults | 1 | 1 | `-p` default 1 |
| `-parallel=4` | 1 | 4 | unchanged |
| `-cpu=4` | 4 | 1 | unchanged |
| `-cpu=4 -parallel=4` | 4 | 4 | unchanged |
| `-p=4` across four packages | 1 in every PID | not relevant | 4 distinct PIDs overlapped |

The `-p=1` fixture ran package tests sequentially from `15:00:21.543` through `15:00:27.112`; `-p=4` started the four distinct PIDs between `15:00:29.610` and `15:00:29.969`, with all four overlapping. This directly disproves a subtree interpretation of `GOMAXPROCS=1`.

Additional checks:

- `GOFLAGS=-p=4` with command-line `-p=1` ran sequentially; the reverse ran concurrently, confirming documented command-line precedence.
- `GOFLAGS=-parallel=4` admitted four tests; adding command-line `-parallel=2` admitted two.
- `gotestsum -- -p=4 ./p1 ./p2` preserved the environment and flag: two runtime-1 test PIDs overlapped.
- a normally spawned Go grandchild reported `env="1" runtime=1`.
- a compiled test binary ignored `GOFLAGS=-parallel=4` (maximum one), honored explicit `-test.parallel=4` (maximum four), honored `-test.cpu=4` (runtime four), and rejected `-p=4`.
- eight goroutines blocked in direct syscalls increased the process's thread-create profile from 3 to 10 while `GOMAXPROCS` remained one.
- four concurrent blocking cgo calls of 250 ms completed in about 255 ms while `GOMAXPROCS` remained one, demonstrating that cgo calls were not serialized by the setting.

## Design consequence for Ooze

Keep `GOMAXPROCS=1` in automatic mode: for ordinary Go test commands it removes the accidental `P × P` default and costs no extra command or retry. State the guarantee narrowly:

> Ooze sets `GOMAXPROCS=1` for automatic attempts, shaping cooperating Go processes and Go's default `-p` and `-parallel` values. Explicit flags, `-cpu`, runtime changes, wrappers, cgo, syscalls, and subprocesses may exceed that profile.

For an opaque `WithTestCommand`, accepting explicit overrides and documenting them is the only
general cross-platform behavior that does not pretend Ooze can understand arbitrary programs. The
one-way automatic fallback can reduce future outer concurrency to one attempt, but it cannot make an
explicitly internally-parallel command into a one-CPU process tree. `Serial()` is a separate user
mode with an exclusive attempt and the full-`P` cooperative profile; it is not the fallback.

Capacity invariants and simulation tests should therefore be stated over **Ooze-owned admissions and
leases**. The child Go profile is cooperative, with explicit user command configuration treated as an
override. No ramp, parser, command rewrite, or subtree-quota claim follows from this research.
