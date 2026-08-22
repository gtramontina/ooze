# Raw calibration results

The six campaigns the deadline formula on
[#59](https://github.com/gtramontina/ooze/issues/59) rests on, as emitted by the harness in
the parent directory. Every workspace path in these runs is unique, baseline included, and
each campaign ran a single wave.

Per attempt: wall clock, combined child-tree CPU, exit code, the file mutated, and the ratio
to that campaign's baseline. Machine and toolchain are recorded in
[`../../../research/baseline-derived-deadlines.md`](../../../research/baseline-derived-deadlines.md);
these timings are specific to that host and are kept for audit, not as a portable baseline.

| file | fixture | peers |
| --- | --- | ---: |
| `s-sym-plain-p14-a.json`, `s-sym-plain-p14-b.json` | 30 packages | 14 |
| `s-sym-plain-p7.json` | 30 packages | 7 |
| `s-sym-cpu-p14.json` | 12 packages | 14 |
| `s-sym-tiny-p14.json` | 1 package | 14 |
| `s-sym-plain-serial.json` | 30 packages | 1 (serial) |
