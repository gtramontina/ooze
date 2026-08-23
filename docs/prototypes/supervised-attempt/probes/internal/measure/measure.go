// Package measure is the single timing instrument every probe in this module
// uses. It exists because two probes in an earlier draft published costs for
// the same kernel read that contradicted each other — one quoted a median, the
// other a mean, and the containing operation came out cheaper than the
// operation it contains. Nothing here is clever; the point is that no probe
// gets to invent its own statistic.
//
// # The one statistic
//
// Every duration any probe publishes is the MEDIAN of N timed samples, taken
// after N/2 further samples that are run and discarded as warm-up. The median
// is the nearest-rank median: for a sorted sample of length n, index
// ceil(0.5*n)-1. Spread is always reported alongside it as min, p25, p75 and
// max of the same sample, by the same nearest-rank rule. A probe that wants a
// different statistic has to change this file, which changes every probe.
//
// Sample counts and units are part of every string this package formats, so a
// number cannot be quoted without them.
//
// # The clock and its floor
//
// Timing uses time.Now's monotonic reading. On darwin/arm64 that clock ticks at
// 24 MHz, so its resolution is ~41.67 ns and a single tick is the smallest
// non-zero duration observable. Resolution measures it rather than asserting
// it. Any quantity whose median is not at least ResolvableFactor times the
// resolution is NOT resolvable by this instrument, and Resolvable says so;
// probes are expected to withdraw such a claim rather than print it.
//
// # The second engine
//
// PerOp runs the same closure under testing.Benchmark, which uses a different
// loop, a different clock discipline and no per-iteration clock read. Probes
// print both engines. Agreement is evidence; disagreement is printed rather
// than hidden, because an independent instrument reproducing a number is the
// whole point of publishing it.
package measure

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"
)

// ResolvableFactor is how many clock ticks a median must span before this
// package will call it resolvable. Ten is arbitrary but stated: at 41.67 ns
// per tick it puts the floor at ~417 ns.
const ResolvableFactor = 10

// clockProbeReads is the number of back-to-back clock reads Resolution uses to
// find the smallest observable non-zero delta.
const clockProbeReads = 200_000

// clockCostBatch is how many clock reads ClockCost times as one sample, so that
// the cost of a single read can be reported without leaving the one statistic.
const clockCostBatch = 100

// Summary is a sample of durations reduced to the one published statistic plus
// its spread. N is the number of timed samples the summary was computed from.
type Summary struct {
	N      int
	Min    time.Duration
	P25    time.Duration
	Median time.Duration
	P75    time.Duration
	Max    time.Duration
}

// Summarize reduces timed samples to the published statistic. It is the only
// place in this module where a central tendency is chosen.
func Summarize(samples []time.Duration) Summary {
	if len(samples) == 0 {
		return Summary{}
	}

	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	return Summary{
		N:      len(sorted),
		Min:    sorted[0],
		P25:    rank(sorted, 0.25),
		Median: rank(sorted, 0.50),
		P75:    rank(sorted, 0.75),
		Max:    sorted[len(sorted)-1],
	}
}

// rank is the nearest-rank quantile: index ceil(q*n)-1, clamped.
func rank(sorted []time.Duration, quantile float64) time.Duration {
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// IQR is the interquartile range, the spread figure a probe should compare a
// difference against before claiming the difference is real.
func (s Summary) IQR() time.Duration { return s.P75 - s.P25 }

// String renders the statistic, the sample count and the units, so no caller
// can print the number without them.
func (s Summary) String() string {
	if s.N == 0 {
		return "no samples"
	}

	return fmt.Sprintf("median %s (n=%d; min %s, p25 %s, p75 %s, max %s, IQR %s)",
		Format(s.Median), s.N,
		Format(s.Min), Format(s.P25), Format(s.P75), Format(s.Max), Format(s.IQR()))
}

// Time runs f n times with the clock read around each call, after n/2 warm-up
// calls that are timed and thrown away. Every probe that times a whole
// operation goes through here.
func Time(n int, f func()) Summary {
	Warm(n, f)

	samples := make([]time.Duration, 0, n)
	for range n {
		start := time.Now()
		f()
		samples = append(samples, time.Since(start))
	}

	return Summarize(samples)
}

// Collect is Time for probes that must time phases INSIDE one call, where the
// comparison between phases has to be paired within a sample rather than made
// across independent timing runs. sample performs one operation and returns
// the phase duration the caller cares about; the caller keeps its other phases
// from the same call. Warm-up follows the same n/2 rule as Time.
func Collect(n int, sample func() time.Duration) Summary {
	for range n / 2 {
		sample()
	}

	samples := make([]time.Duration, 0, n)
	for range n {
		samples = append(samples, sample())
	}

	return Summarize(samples)
}

// Warm runs the n/2 discarded warm-up calls Time would run. Probes that
// collect their own per-sample phases call it directly, so that warm-up is the
// same everywhere.
func Warm(n int, f func()) {
	for range n / 2 {
		f()
	}
}

// PerOp is the second, independent engine: testing.Benchmark's own loop. It
// returns the reported ns/op and the iteration count that produced it.
func PerOp(f func()) (time.Duration, int) {
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			f()
		}
	})
	if result.N == 0 {
		return 0, 0
	}

	return time.Duration(result.NsPerOp()), result.N
}

// Resolution measures the smallest non-zero delta between two consecutive
// readings of the monotonic clock. It is the floor below which this instrument
// cannot resolve anything.
func Resolution() time.Duration {
	smallest := time.Duration(math.MaxInt64)
	for range clockProbeReads {
		before := time.Now()
		delta := time.Since(before)
		if delta > 0 && delta < smallest {
			smallest = delta
		}
	}
	if smallest == time.Duration(math.MaxInt64) {
		return 0
	}

	return smallest
}

// ClockCost is the cost of one time.Now() call: the median over n samples of a
// batch of clockCostBatch reads, divided by the batch size. A probe that
// instruments phases inside a call pays this at every boundary, so any phase
// smaller than a few multiples of it is measuring the instrument.
func ClockCost(n int) time.Duration {
	batch := Time(n, func() {
		for range clockCostBatch {
			_ = time.Now()
		}
	})

	return batch.Median / clockCostBatch
}

// Resolvable reports whether a median is far enough above the clock floor to
// be worth publishing, and by what multiple of the floor.
func Resolvable(median, resolution time.Duration) (bool, float64) {
	if resolution <= 0 {
		return false, 0
	}
	multiple := float64(median) / float64(resolution)

	return multiple >= ResolvableFactor, multiple
}

// Format renders a duration with an explicit unit and a fixed number of
// significant digits, so two probes cannot print the same duration
// differently. Sub-microsecond values are printed in whole nanoseconds because
// the clock cannot resolve a fraction of a tick.
func Format(d time.Duration) string {
	switch {
	case d < 0:
		return "-" + Format(-d)
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1e3)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Nanoseconds())/1e6)
	default:
		return fmt.Sprintf("%.3fs", d.Seconds())
	}
}

// Span is the low-high range of a set of per-repeat statistics. A single
// figure from a single invocation is not reproducible by a stranger; a range
// is what probes publish.
type Span struct {
	Lo, Hi time.Duration
}

// SpanOf returns the range of the given values.
func SpanOf(values []time.Duration) Span {
	if len(values) == 0 {
		return Span{}
	}
	span := Span{Lo: values[0], Hi: values[0]}
	for _, value := range values[1:] {
		if value < span.Lo {
			span.Lo = value
		}
		if value > span.Hi {
			span.Hi = value
		}
	}

	return span
}

// String renders the range with units on both ends.
func (s Span) String() string {
	if s.Lo == s.Hi {
		return Format(s.Lo)
	}

	return Format(s.Lo) + "-" + Format(s.Hi)
}
