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
`4be3aa90f634e566527f46b63a8476d1802afe0f` in
[run 32863293268](https://github.com/gtramontina/ooze/actions/runs/32863293268).
All 29 jobs passed. The raw artifacts are
[Ubuntu 24.04](https://github.com/gtramontina/ooze/actions/runs/32863293268/artifacts/9569097093),
[Windows 2025](https://github.com/gtramontina/ooze/actions/runs/32863293268/artifacts/9569089176),
and [macOS 26](https://github.com/gtramontina/ooze/actions/runs/32863293268/artifacts/9569262731).
The baseline revision was
`87c15dcb180492ba96f30278efc0146dd248f09b`. All platforms used Go 1.26.6.
The native targets were Linux amd64 on `ubuntu24`, Windows amd64 on
`win25-vs2026`, and Darwin arm64 on `macos26`.

The arrays below preserve artifact sample order. Times are milliseconds,
throughput is mutants per second, and memory is bytes of summed live-helper Go
`MemStats.Sys`.

### Ubuntu 24.04 raw distribution

- Baseline wall: `[799, 787, 792, 796, 787, 791, 794, 795, 796, 795]`
- Candidate wall: `[281, 282, 286, 285, 284, 282, 283, 287, 287, 283]`
- Baseline throughput: `[20.01990604282721, 20.308969955927832, 20.183207184861487, 20.087462947968394, 20.316657944915878, 20.21629701734048, 20.148901515575815, 20.11551629421378, 20.08883117925714, 20.116117446558672]`
- Candidate throughput: `[56.80411634437336, 56.62695344098974, 55.91996323849577, 56.125018113034166, 56.23108258983132, 56.72167158595999, 56.45365559225184, 55.58844055749995, 55.73450298225746, 56.46666878416674]`
- Baseline memory: `[12540168, 12802312, 12609816, 12609816, 12540168, 12609816, 12802312, 12802312, 12540168, 12540168]`
- Candidate memory: `[39157824, 34963520, 30769216, 34963520, 30769216, 34963520, 39227472, 34963520, 30838864, 39227472]`

The median wall time was 794.5 ms for the baseline and 283.5 ms for the
candidate. Median throughput was 20.132509481067245 and 56.342369091041576.
Every candidate sample settled 16 of 16 healthy mutants, issued 17 commands,
reached the expected peak of four helpers, and reported only `GOMAXPROCS=1`.

### Windows 2025 raw distribution

- Baseline wall: `[1099, 1109, 1164, 1103, 1160, 1138, 1108, 1125, 1145, 1115]`
- Candidate wall: `[463, 471, 506, 474, 471, 504, 492, 480, 485, 493]`
- Baseline throughput: `[14.553440415122333, 14.426377910690803, 13.73786538643499, 14.494891094447622, 13.787009827639112, 14.057210562693445, 14.431883494290926, 14.210391597537553, 13.961833408625427, 14.3446785994071]`
- Candidate throughput: `[34.5011170814815, 33.90366358750769, 31.588060108129877, 33.74609019908084, 33.92196421741605, 31.712695662377126, 32.49128116776914, 33.275046543471355, 32.97187910860524, 32.438924600991086]`
- Baseline memory: `[12470520, 12732664, 12208376, 12732664, 12470520, 12732664, 12732664, 12732664, 12732664, 12732664]`
- Candidate memory: `[36250592, 36250592, 36250592, 36250592, 44639200, 40444896, 32056288, 32056288, 36250592, 40444896]`

The median wall time was 1,120 ms for the baseline and 482.5 ms for the
candidate. Median throughput was 14.277535098472327 and 33.1234628260383.
Every candidate sample settled 16 of 16 healthy mutants, issued 17 commands,
reached the expected peak of four helpers, and reported only `GOMAXPROCS=1`.

### macOS 26 raw distribution

- Baseline wall: `[982, 958, 910, 930, 943, 979, 976, 933, 915, 902]`
- Candidate wall: `[369, 380, 363, 391, 423, 373, 383, 367, 378, 368]`
- Baseline throughput: `[16.2930038830108, 16.6996259530106, 17.56892715620835, 17.190764124877564, 16.95979659335795, 16.329681290946464, 16.38768972111609, 17.14762250389588, 17.480601874428334, 17.72720927846831]`
- Candidate throughput: `[43.27738758148817, 42.07219120483525, 43.98427932581655, 40.82147263957508, 37.76048091295365, 42.843591620379364, 41.72302621740411, 43.513891770513276, 42.272852770258936, 43.372427667382205]`
- Baseline memory: `[12671256, 12863752, 12601608, 12671256, 12601608, 12601608, 12601608, 12601608, 12601608, 12671256]`
- Candidate memory: `[24435480, 34138408, 30013752, 27189792, 25749800, 29944104, 31453744, 29944104, 28504112, 31384096]`

The median wall time was 938 ms for the baseline and 375.5 ms for the candidate.
Median throughput was 17.053709548626916 and 42.558222195319146. Every
candidate sample settled 16 of 16 healthy mutants, issued 17 commands, reached
the expected peak of three helpers, and reported only `GOMAXPROCS=1`.

## Confirmation cost

Each native confirmation observation issued exactly five commands: one
baseline, two primary attempts, and two confirmations. Both hanging mutants
were confirmed once with a 120 ms resolved deadline. Summed confirmation time
was 240 ms on every platform. Total wall time was 396 ms on Ubuntu, 486 ms on
Windows, and 387 ms on macOS. These figures are kept out of the healthy
throughput distributions.

## Memory tradeoff

Mean candidate helper `MemStats.Sys` was 2.77 times the baseline on Ubuntu,
2.94 times on Windows, and 2.31 times on macOS. This is the expected cost of
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
