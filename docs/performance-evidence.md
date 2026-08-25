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
