# Managed execution performance evidence

Issue #72 uses reviewed measurements rather than a noisy percentage threshold.
The manual `os-compatibility.yml` performance job compares the exact candidate
revision with legacy revision `87c15dcb180492ba96f30278efc0146dd248f09b`
on Ubuntu 24.04, macOS 26, and Windows 2025. Linux and macOS execute Go through
the repository's Devbox environment; Windows uses pinned raw Go 1.26.6 so the
measurement exercises native Job Objects.

Each platform runs ten A/B pairs, alternating which revision runs first. The A
side is the legacy default serial behavior. The B side is managed automatic
execution. Both discover the same sixteen integer-increment mutants and run the
same healthy 40 ms Go helper command. Legacy runs sixteen mutant commands.
Managed execution additionally runs its required verified baseline, for
seventeen commands total.

The helper writes one immutable record after each healthy command. The raw
JSONL artifact records wall time, mutant throughput, peak overlapping helper
processes, the sum of the live helpers' measured Go `MemStats.Sys` bytes, and
the set of inherited `GOMAXPROCS` values. The helper tree has no descendants,
so its process peak is an exact count for the stated tree identity. The memory
number is Go runtime reserved memory for those helpers, not whole-runner RSS;
it is measured evidence only for that named command tree.

The fixture holds the first mutant wave until the expected admission width is
present. This makes the stable structural claim explicit: managed automatic
must reach `min(P, 16)` immediately, without a calibration ramp, while every
managed helper observes `GOMAXPROCS=1`. It does not turn wall-time variation
into a correctness threshold.

After the healthy samples, one separate candidate observation runs two hanging
mutants with a 120 ms resolved deadline. Its record reports primary count,
confirmation count, summed confirmation command time, total wall time, and the
five-command envelope (one baseline, two primaries, two confirmations). This
keeps the deliberate confirmation cost separate from healthy throughput.

The workflow uploads `performance.jsonl` even when collection fails. Missing,
malformed, short, skipped, or inconclusive native evidence blocks the landing
review; it is never converted into a green timing claim.

## Final native run

The final collection ran at candidate revision
`9544f38f6d737c059fc19cfaa549f9498407ec9f` in
[run 32859862542](https://github.com/gtramontina/ooze/actions/runs/32859862542).
All 29 jobs passed. The raw artifacts are
[Ubuntu 24.04](https://github.com/gtramontina/ooze/actions/runs/32859862542/artifacts/9567804632),
[Windows 2025](https://github.com/gtramontina/ooze/actions/runs/32859862542/artifacts/9567742144),
and [macOS 26](https://github.com/gtramontina/ooze/actions/runs/32859862542/artifacts/9567903704).
The baseline revision was
`87c15dcb180492ba96f30278efc0146dd248f09b`. All platforms used Go 1.26.6.
The native targets were Linux amd64 on `ubuntu24`, Windows amd64 on
`win25-vs2026`, and Darwin arm64 on `macos26`.

The arrays below preserve artifact sample order. Times are milliseconds,
throughput is mutants per second, and memory is bytes of summed live-helper Go
`MemStats.Sys`.

### Ubuntu 24.04 raw distribution

- Baseline wall: `[778, 780, 780, 778, 779, 777, 777, 776, 773, 777]`
- Candidate wall: `[280, 279, 279, 280, 277, 282, 278, 278, 277, 279]`
- Baseline throughput: `[20.558708499053868, 20.498528728349786, 20.494163414684838, 20.540354805770072, 20.520116966564817, 20.570192255856412, 20.57571181147759, 20.596800905398293, 20.694057797530807, 20.566591444272763]`
- Candidate throughput: `[56.98319998504761, 57.19644387830768, 57.25464807009932, 57.07259242452437, 57.58109347828078, 56.67803511152556, 57.432746675414826, 57.43047655848931, 57.564623476435756, 57.19136338494748]`
- Baseline memory: `[12540168, 12609816, 12609816, 12540168, 13064456, 12540168, 12540168, 12802312, 12540168, 12540168]`
- Candidate memory: `[39157824, 39157824, 39227472, 34963520, 34963520, 30769216, 34963520, 34963520, 34963520, 30769216]`

The median wall time was 777.5 ms for the baseline and 279 ms for the
candidate. Median throughput was 20.562649971663316 and 57.2255459742035.
Every candidate sample settled 16 of 16 healthy mutants, issued 17 commands,
reached the expected peak of four helpers, and reported only `GOMAXPROCS=1`.

### Windows 2025 raw distribution

- Baseline wall: `[1175, 1117, 1143, 1163, 1129, 1141, 1140, 1111, 1101, 1130]`
- Candidate wall: `[553, 519, 511, 513, 495, 514, 520, 503, 491, 480]`
- Baseline throughput: `[13.607383774657643, 14.320965598265945, 13.992229765005748, 13.752633951729116, 14.163445810567808, 14.013934054630518, 14.031209443775673, 14.388942097997688, 14.52210563996832, 14.149852956496808]`
- Candidate throughput: `[28.89387910259223, 30.798435978424926, 31.26516849190115, 31.16374581212819, 32.282315303794746, 31.101708028050627, 30.7274650532699, 31.775354598062616, 32.5240469590321, 33.31900616068424]`
- Baseline memory: `[12208376, 12208376, 12732664, 12732664, 12732664, 12732664, 12732664, 8538360, 12208376, 12732664]`
- Candidate memory: `[40444896, 32056288, 32056288, 36250592, 36250592, 32056288, 36250592, 40444896, 36250592, 40444896]`

The median wall time was 1,135 ms for the baseline and 512 ms for the
candidate. Median throughput was 14.09053120013624 and 31.21445715201467.
Every candidate sample settled 16 of 16 healthy mutants, issued 17 commands,
reached the expected peak of four helpers, and reported only `GOMAXPROCS=1`.

### macOS 26 raw distribution

- Baseline wall: `[868, 865, 870, 882, 869, 895, 914, 890, 870, 922]`
- Candidate wall: `[358, 363, 370, 367, 379, 360, 361, 365, 371, 365]`
- Baseline throughput: `[18.423098850018885, 18.482672679767028, 18.3741552265377, 18.1260069088884, 18.40229456738777, 17.867948060010207, 17.504392407272434, 17.96808222868231, 18.38741068325316, 17.345618909976917]`
- Candidate throughput: `[44.612816435709554, 44.06595158184717, 43.16394885267378, 43.57885436541177, 42.18088805105577, 44.44181077335822, 44.200502607230185, 43.75261126180346, 43.11789544247213, 43.76250404146725]`
- Baseline memory: `[12671256, 12863752, 12601608, 12601608, 12863752, 12601608, 12671256, 12601608, 12671256, 12601608]`
- Candidate memory: `[29944104, 34138408, 25749800, 29944104, 25819448, 29944104, 27189792, 31453744, 27189792, 25749800]`

The median wall time was 876 ms for the baseline and 365 ms for the candidate.
Median throughput was 18.250081067713047 and 43.75755765163535. Every
candidate sample settled 16 of 16 healthy mutants, issued 17 commands, reached
the expected peak of three helpers, and reported only `GOMAXPROCS=1`.

## Confirmation cost

Each native confirmation observation issued exactly five commands: one
baseline, two primary attempts, and two confirmations. Both hanging mutants
were confirmed once with a 120 ms resolved deadline. Summed confirmation time
was 240 ms on every platform. Total wall time was 395 ms on Ubuntu, 473 ms on
Windows, and 390 ms on macOS. These figures are kept out of the healthy
throughput distributions.

## Memory tradeoff

Mean candidate helper `MemStats.Sys` was 2.80 times the baseline on Ubuntu,
2.98 times on Windows, and 2.27 times on macOS. This is the expected cost of
holding three or four Go helpers concurrently rather than one. It is reserved
Go runtime memory summed across the named helper tree, not resident memory for
the runner or the whole job. The evidence supports higher throughput with a
small, bounded process fanout. It does not claim lower memory use.

## macOS outlier investigation

Three earlier hosted macOS collections contained one roughly 20-second
candidate sample among otherwise sub-second samples.

- [Run 32811353284, artifact 9549951941](https://github.com/gtramontina/ooze/actions/runs/32811353284/artifacts/9549951941) recorded sample 2 at 20,312 ms and 0.7876731385298087 mutants per second. That artifact did not yet record `survived_count`. It was red because the fixture could not prove that every nominally healthy mutant settled successfully. Commit `9e79c7e` added that fail-closed invariant.
- [Run 32848028628, artifact 9563203632](https://github.com/gtramontina/ooze/actions/runs/32848028628/artifacts/9563203632) recorded sample 8 at 20,291 ms and 0.7885027154304727 mutants per second. It settled 16 of 16 mutants with 17 commands, peak width three, and `GOMAXPROCS=1`.
- [Run 32855860454, artifact 9566253075](https://github.com/gtramontina/ooze/actions/runs/32855860454/artifacts/9566253075) recorded sample 4 at 20,181 ms and 0.7927885607446107 mutants per second. It also settled 16 of 16 mutants with the same command, peak, and nested-Go invariants.

Two bounded local Darwin reproductions exercised 1,440 candidate mutant
commands without the pause. With automatic width 14, the 30 candidate wall
times were `[190, 172, 190, 190, 185, 186, 188, 188, 170, 187, 189, 184, 181, 186, 187, 181, 190, 184, 186, 183, 176, 187, 185, 185, 185, 189, 181, 180, 185, 183]`.
With an outer `GOMAXPROCS=3`, the 60 candidate wall times were
`[370, 371, 368, 368, 354, 367, 372, 371, 375, 374, 373, 371, 372, 374, 375, 368, 373, 372, 361, 366, 370, 370, 370, 369, 369, 371, 384, 363, 371, 372, 370, 364, 372, 370, 378, 369, 371, 373, 373, 370, 371, 372, 363, 369, 374, 371, 371, 369, 381, 374, 383, 370, 352, 377, 359, 369, 371, 367, 376, 368]`.
Every candidate helper still inherited `GOMAXPROCS=1`.

The pause did not reproduce through public managed-execution behavior, moved
between sample positions, and left the later correctness records green. The
retained evidence therefore supports only a narrow conclusion: a roughly
20-second hosted macOS wall-clock pause occurred inside the measured interval,
but its cause remains unresolved. There is no reproduced runtime defect to
justify a production change. The final exact-head distribution is clean, and
the raw historical outliers remain linked rather than hidden by its median.
