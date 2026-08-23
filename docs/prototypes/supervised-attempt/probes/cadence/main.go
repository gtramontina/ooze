// Command cadence measures the numbers that decide how often a supervised
// attempt must take a descendant census, and what that costs.
//
// "Choose the automatic runaway fuse" (#60) fixed the ceiling at 64 live
// descendants and left the enumeration cadence to "Define the supervised
// attempt contract" (#61). The cadence decides overshoot: a runaway keeps
// creating processes between samples, so the transient peak is the ceiling plus
// the creation rate times the cadence. That is only tolerable if it stays well
// inside the host limit the runaway would actually hit.
//
// # What the previous version of this file got wrong
//
// Four things, all of them the same mistake in different places: a number that
// was not a function of a measurement.
//
//  1. It published the bare kern.proc.all read at 115 µs as a MEAN, while the
//     census probe published the parent walk that CONTAINS that read at
//     98-102 µs as a MEDIAN. Both cost figures now come out of one
//     census.Union call through the shared internal/census and internal/measure
//     packages, with the same statistic, and this probe asserts per sample that
//     the walk exceeds the read it performs.
//  2. samplerCost() hardcoded 100 µs and 16 µs, so its percentage column
//     printed byte-identically across runs whose measured costs differed by
//     18%. Every derived column here is computed from this run's measurement.
//  3. The overshoot table it printed was not the table that got published:
//     the program truncated and someone hand-rounded 107 to 108. The rounding
//     rule is now stated, applied by the program, and the rate is printed
//     beside the table.
//  4. Its headroom compared a system-wide live count against a PER-UID cap.
//     kern.maxprocperuid caps one real uid, so the denominator is this uid's
//     own population, counted by two independent routes and cross-checked.
//
// # Three spawn rates, not one
//
// Three different quantities were being conflated, and which one counted as
// "conservative" flipped direction between uses. They are separate
// measurements here, each labelled with what it measures, and the FASTEST is
// used for both hazards that want a fast rate - the overshoot table and the
// PID-wrap estimate.
//
//	go run ./cadence
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"supervisedattemptprobes/internal/census"
	"supervisedattemptprobes/internal/check"
	"supervisedattemptprobes/internal/measure"
)

const (
	modeEnv = "OOZE_CADENCE_MODE"

	// ceiling is #60's fixed fuse ceiling, the only constant in the overshoot
	// arithmetic that is a decision rather than a measurement.
	ceiling = 64

	// recommendedCadence is the cadence #61's draft resolution proposes. It is
	// printed as a candidate and its consequences are asserted, not assumed.
	recommendedCadence = 50 * time.Millisecond

	// pidSpan is a platform constant, not a measurement: darwin allocates pids
	// up to 99999 and restarts at 100. Independently observed on this host as
	// 99998 -> 100. Everything derived from it says so.
	pidSpan = 99999 - 100 + 1

	// holdOpen bounds every process this probe creates, so nothing it spawns can
	// outlive it even if it is killed.
	holdOpen = 3 * time.Second
)

func main() {
	if os.Getenv(modeEnv) == "hold" {
		time.Sleep(holdOpen)

		return
	}

	os.Exit(run())
}

func run() int {
	samples := flag.Int("samples", 400, "timed samples per repeat for the census cost")
	repeats := flag.Int("repeats", 3, "repeats of the census cost block, so the output carries a range")
	live := flag.Int("live", 250, "concurrent live processes the creation-rate bursts may hold at once")
	reaps := flag.Int("reaps", 600, "spawn-and-reap cycles per rate measurement")
	budget := flag.Duration("budget", 4*time.Second, "wall-clock cap per rate measurement")
	flag.Parse()

	checks := check.New()
	resolution := measure.Resolution()
	clockCost := measure.ClockCost(*samples)
	peers := runtime.NumCPU()

	fmt.Printf("probe: cadence - how often a supervised attempt must take a census, and what it costs\n")
	fmt.Printf("host: %s/%s, %d logical CPUs (peer fan-out below is this number, not a constant)\n",
		runtime.GOOS, runtime.GOARCH, peers)
	fmt.Printf("clock: monotonic; measured resolution %s; resolvable floor %s (%dx);\n",
		measure.Format(resolution),
		measure.Format(time.Duration(measure.ResolvableFactor)*resolution), measure.ResolvableFactor)
	fmt.Printf("       one time.Now() call costs %s (median of %d batches of 100 reads)\n",
		measure.Format(clockCost), *samples)
	fmt.Printf("statistic: nearest-rank median, the same one internal/measure gives every probe;\n")
	fmt.Printf("       census costs: %d samples per repeat, %d repeats, n/2 warm-up discarded;\n", *samples, *repeats)
	fmt.Printf("       rates: one burst each, count and elapsed printed so the division is checkable\n\n")

	headroom := reportLimits(checks, *live)
	rates := reportRates(checks, *live, *reaps, *budget)
	cost := reportCensusCost(checks, *samples, *repeats, resolution, peers)
	reportOvershoot(checks, rates, cost, headroom, peers)
	reportPIDWrap(rates)

	fmt.Println()

	return checks.Report(os.Stdout, "\ninvariants")
}

// -------------------------------------------------------------------- limits

const (
	invUIDRoutes = "the two routes to this uid's process count agree within 2%"
	invHeadroom  = "per-uid headroom is positive and at least 4x the burst cap"
)

// headroomReport is the denominator every margin in this probe divides by.
type headroomReport struct {
	perUIDCap    int
	perUIDLive   int
	perUID       int
	systemCap    int
	systemLive   int
	systemWide   int
	uid          int
	burstCapUsed int
}

func reportLimits(checks *check.Set, burstCap int) headroomReport {
	uid := os.Getuid()
	population, err := census.Snapshot(uid)
	check.Must(err)

	perUIDCap := sysctlInt("kern.maxprocperuid")
	systemCap := sysctlInt("kern.maxproc")

	report := headroomReport{
		uid:        uid,
		perUIDCap:  perUIDCap,
		perUIDLive: population.ThisUID,
		perUID:     perUIDCap - population.ThisUID,
		systemCap:  systemCap,
		systemLive: population.SystemWide,
		systemWide: systemCap - population.SystemWide,

		burstCapUsed: burstCap,
	}

	fmt.Printf("1. host process limits, and which denominator a margin may use\n")
	var nproc unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NPROC, &nproc); err == nil {
		fmt.Printf("   RLIMIT_NPROC (per uid)            soft %d, hard %d\n", nproc.Cur, nproc.Max)
	}
	fmt.Printf("   kern.maxprocperuid (per REAL uid)  %d\n", perUIDCap)
	fmt.Printf("   kern.maxproc (system-wide)         %d\n", systemCap)
	fmt.Printf("   live now, system-wide              %d  (kern.proc.all, %d of them zombies)\n",
		population.SystemWide, population.Zombies)
	fmt.Printf("   live now, uid %-5d                %d  (kern.proc.ruid; %d by filtering kern.proc.all on real uid)\n",
		uid, population.ThisUID, population.ThisUIDFromTable)
	fmt.Printf("   PER-UID HEADROOM                   %d = %d - %d  <- the denominator a per-uid margin must use\n",
		report.perUID, perUIDCap, population.ThisUID)
	fmt.Printf("   system-wide headroom               %d = %d - %d  (not the binding limit for one uid's runaway)\n",
		report.systemWide, systemCap, population.SystemWide)
	fmt.Printf("   zombies count against both caps, so both counts include them.\n")
	fmt.Printf("   this probe's own bursts hold at most %d live processes at once, %.0fx inside that headroom.\n\n",
		burstCap, float64(report.perUID)/float64(burstCap))

	checks.Declare(invUIDRoutes, invHeadroom)
	difference := math.Abs(float64(population.ThisUID - population.ThisUIDFromTable))
	checks.Sample(invUIDRoutes, difference <= 0.02*float64(population.ThisUID), func() string {
		return fmt.Sprintf("kern.proc.ruid %d vs filtered kern.proc.all %d",
			population.ThisUID, population.ThisUIDFromTable)
	})
	checks.Sample(invHeadroom, report.perUID > 0 && report.perUID >= 4*burstCap, func() string {
		return fmt.Sprintf("per-uid headroom %d against burst cap %d", report.perUID, burstCap)
	})

	return report
}

// ---------------------------------------------------------------- spawn rates

const invRateComplete = "every rate measurement completed at least half its requested processes"

// rate is one labelled creation-rate measurement. The count and the elapsed
// time are published beside the rate so the division can be checked.
type rate struct {
	label     string
	what      string
	child     string
	processes int
	requested int
	elapsed   time.Duration
	perSecond float64

	// pidsConsumed is how far the kernel's pid counter advanced during the
	// burst, which includes every other process the machine started. It is only
	// recorded for the spawn-and-reap bursts, which are the pid-consuming ones.
	pidsConsumed int
	pidRate      float64
}

type rateReport struct {
	all     []rate
	fastest rate
	pidRate float64
}

func reportRates(checks *check.Set, liveCap, reaps int, budget time.Duration) rateReport {
	checks.Declare(invRateComplete)

	self, err := os.Executable()
	check.Must(err)

	trueBinary := firstExisting("/usr/bin/true", "/bin/true")
	sleepBinary := firstExisting("/bin/sleep", "/usr/bin/sleep")

	measured := []rate{
		serialSpawnReap(trueBinary, reaps, budget),
		parallelSpawnReap(trueBinary, reaps, runtime.NumCPU(), budget),
		concurrentLive(sleepBinary, []string{"3"}, "/bin/sleep", 1, liveCap, budget),
		concurrentLive(self, nil, "this Go binary", 1, liveCap, budget),
		concurrentLive(sleepBinary, []string{"3"}, "/bin/sleep", runtime.NumCPU(), liveCap, budget),
	}

	fmt.Printf("2. process-creation rates: three different quantities, measured separately\n")
	fmt.Printf("   %-48s %-16s %8s %10s %12s\n", "what it measures", "child", "procs", "elapsed", "rate")
	report := rateReport{all: measured}
	for _, measurement := range measured {
		fmt.Printf("   %-48s %-16s %8d %10s %10.0f/s\n",
			measurement.what, measurement.child, measurement.processes,
			measure.Format(measurement.elapsed), measurement.perSecond)

		checks.Sample(invRateComplete, measurement.processes*2 >= measurement.requested, func() string {
			return fmt.Sprintf("%s: %d of %d requested", measurement.label, measurement.processes, measurement.requested)
		})
		if measurement.perSecond > report.fastest.perSecond {
			report.fastest = measurement
		}
		if measurement.pidRate > report.pidRate {
			report.pidRate = measurement.pidRate
		}
	}

	fmt.Printf("\n   spawn-and-reap consumes a pid per process and holds none; concurrent-live creation\n")
	fmt.Printf("   accumulates live processes, which is what a fuse ceiling counts. They are not the\n")
	fmt.Printf("   same quantity and neither is \"conservative\" in every direction.\n")
	for _, measurement := range measured {
		if measurement.pidsConsumed > 0 {
			fmt.Printf("   pid counter advanced %d during %q in %s = %.0f pids/s consumed system-wide\n",
				measurement.pidsConsumed, measurement.label,
				measure.Format(measurement.elapsed), measurement.pidRate)
		}
	}
	fmt.Printf("   FASTEST measured creation rate: %.0f/s (%s).\n", report.fastest.perSecond, report.fastest.label)
	fmt.Printf("   That single rate is used for BOTH hazards below - the overshoot table and the pid\n")
	fmt.Printf("   wrap - because both are worst-case bounds. The slower rates are reported and unused.\n\n")

	return report
}

// serialSpawnReap measures the pid-consuming rate: one process created and
// reaped at a time, nothing held open.
func serialSpawnReap(binary string, count int, budget time.Duration) rate {
	result := rate{
		label:     "serial spawn-and-reap",
		what:      "spawn-and-reap,  1 goroutine  (pid-consuming)",
		child:     binary,
		requested: count,
	}
	if binary == "" {
		return result
	}

	firstPID, lastPID := 0, 0
	start := time.Now()
	for range count {
		if time.Since(start) > budget {
			break
		}
		command := exec.Command(binary)
		if err := command.Start(); err != nil {
			break
		}
		if firstPID == 0 {
			firstPID = command.Process.Pid
		}
		lastPID = command.Process.Pid
		_ = command.Wait()
		result.processes++
	}
	result.elapsed = time.Since(start)
	result.finish(firstPID, lastPID)

	return result
}

// parallelSpawnReap is serialSpawnReap with workers goroutines, which is the
// fastest pid-consuming shape a trivial spawner can take without holding
// processes open. At most workers processes exist at once.
func parallelSpawnReap(binary string, count, workers int, budget time.Duration) rate {
	result := rate{
		label:     "parallel spawn-and-reap",
		what:      fmt.Sprintf("spawn-and-reap, %2d goroutines (pid-consuming)", workers),
		child:     binary,
		requested: count,
	}
	if binary == "" {
		return result
	}

	var mutex sync.Mutex
	firstPID, lastPID, started := 0, 0, 0

	start := time.Now()
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				mutex.Lock()
				done := started >= count || time.Since(start) > budget
				if !done {
					started++
				}
				mutex.Unlock()
				if done {
					return
				}

				command := exec.Command(binary)
				if err := command.Start(); err != nil {
					return
				}
				mutex.Lock()
				if firstPID == 0 {
					firstPID = command.Process.Pid
				}
				lastPID = command.Process.Pid
				result.processes++
				mutex.Unlock()
				_ = command.Wait()
			}
		}()
	}
	group.Wait()
	result.elapsed = time.Since(start)
	result.finish(firstPID, lastPID)

	return result
}

// concurrentLive measures the rate at which live processes accumulate: every
// child stays alive until the burst ends. This is the quantity a fuse ceiling
// counts. The burst is capped well below the measured per-uid headroom.
func concurrentLive(binary string, args []string, label string, workers, count int, budget time.Duration) rate {
	result := rate{
		label:     fmt.Sprintf("concurrent live creation, %s, %d goroutine(s)", label, workers),
		what:      fmt.Sprintf("live creation,  %2d goroutine(s) (accumulating)", workers),
		child:     label,
		requested: count,
	}
	if binary == "" {
		return result
	}

	var mutex sync.Mutex
	children := make([]*exec.Cmd, 0, count)
	defer func() {
		for _, child := range children {
			if child.Process != nil {
				_ = child.Process.Kill()
				_, _ = child.Process.Wait()
			}
		}
	}()

	// The cap is what keeps this safe: at most count processes exist at once,
	// and reportLimits asserts that count is far inside the per-uid headroom.
	started := 0
	start := time.Now()
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				mutex.Lock()
				done := started >= count || time.Since(start) > budget
				if !done {
					started++
				}
				mutex.Unlock()
				if done {
					return
				}

				command := exec.Command(binary, args...)
				command.Env = append(os.Environ(), modeEnv+"=hold")
				command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				if err := command.Start(); err != nil {
					return
				}
				mutex.Lock()
				children = append(children, command)
				mutex.Unlock()
			}
		}()
	}
	group.Wait()
	result.elapsed = time.Since(start)
	result.processes = len(children)
	result.finish(0, 0)

	return result
}

func (r *rate) finish(firstPID, lastPID int) {
	if r.elapsed > 0 {
		r.perSecond = float64(r.processes) / r.elapsed.Seconds()
	}
	if firstPID > 0 && lastPID > 0 {
		advance := lastPID - firstPID
		if advance < 0 {
			advance += pidSpan
		}
		r.pidsConsumed = advance
		if r.elapsed > 0 {
			r.pidRate = float64(advance) / r.elapsed.Seconds()
		}
	}
}

// --------------------------------------------------------------- census cost

const (
	invWalkOverRead = "walk total > the bare kern.proc.all read it performs (paired, per sample)"
	invGroupFloor   = "the kern.proc.pgrp phase is above the clock floor"
	invTickFits     = "one sampler tick at peer fan-out fits inside the recommended cadence"
)

// costReport is the sampler-tick model, every term measured.
type costReport struct {
	read        measure.Summary // kern.proc.all: once per tick for the whole runtime
	index       measure.Summary // children+live maps: once per tick
	search      measure.Summary // BFS from one root: once per attempt
	group       measure.Summary // kern.proc.pgrp: once per attempt
	walk        measure.Summary
	searchBound time.Duration // search, floored at the resolvable floor
	perRepeat   map[string][]time.Duration
	samples     int
}

// tick is the modelled cost of one shared sampler tick serving peers attempts.
func (c costReport) tick(peers int) time.Duration {
	return c.read.Median + c.index.Median + time.Duration(peers)*(c.searchBound+c.group.Median)
}

func reportCensusCost(checks *check.Set, samples, repeats int, resolution time.Duration, peers int) costReport {
	checks.Declare(invWalkOverRead, invGroupFloor, invTickFits)

	self, group := os.Getpid(), syscall.Getpgrp()

	var read, index, search, groupPhase, walk []time.Duration
	perRepeat := map[string][]time.Duration{}
	for range repeats {
		measure.Warm(samples, func() {
			_, _, err := census.Union(self, group)
			check.Must(err)
		})
		var repeatRead, repeatWalk, repeatGroup []time.Duration
		for range samples {
			_, phases, err := census.Union(self, group)
			check.Must(err)

			total := phases.Read + phases.Index + phases.Search
			read = append(read, phases.Read)
			index = append(index, phases.Index)
			search = append(search, phases.Search)
			groupPhase = append(groupPhase, phases.Group)
			walk = append(walk, total)
			repeatRead = append(repeatRead, phases.Read)
			repeatWalk = append(repeatWalk, total)
			repeatGroup = append(repeatGroup, phases.Group)

			checks.Sample(invWalkOverRead, total > phases.Read, func() string {
				return fmt.Sprintf("walk %s <= read %s", measure.Format(total), measure.Format(phases.Read))
			})
			checks.Sample(invGroupFloor, phases.Group > resolution, func() string {
				return "group phase " + measure.Format(phases.Group)
			})
		}
		perRepeat["read"] = append(perRepeat["read"], measure.Summarize(repeatRead).Median)
		perRepeat["walk"] = append(perRepeat["walk"], measure.Summarize(repeatWalk).Median)
		perRepeat["group"] = append(perRepeat["group"], measure.Summarize(repeatGroup).Median)
	}

	report := costReport{
		read:      measure.Summarize(read),
		index:     measure.Summarize(index),
		search:    measure.Summarize(search),
		group:     measure.Summarize(groupPhase),
		walk:      measure.Summarize(walk),
		perRepeat: perRepeat,
		samples:   len(read),
	}
	report.searchBound = report.search.Median
	floor := time.Duration(measure.ResolvableFactor) * resolution
	if report.searchBound < floor {
		report.searchBound = floor
	}

	fmt.Printf("3. census cost, from the same census.Union call the census probe measures, %d samples\n", report.samples)
	fmt.Printf("   pooled from %d repeats. Phases are paired within one call, so no part can exceed the whole.\n", repeats)
	fmt.Printf("   %-42s %10s %10s %10s  %s\n", "phase", "median", "p25", "p75", "per-repeat medians")
	fmt.Printf("   %-42s %10s %10s %10s  %s\n",
		"kern.proc.all read (once per tick)",
		measure.Format(report.read.Median), measure.Format(report.read.P25), measure.Format(report.read.P75),
		measure.SpanOf(perRepeat["read"]).String())
	fmt.Printf("   %-42s %10s %10s %10s\n",
		"index maps (once per tick)",
		measure.Format(report.index.Median), measure.Format(report.index.P25), measure.Format(report.index.P75))
	fmt.Printf("   %-42s %10s %10s %10s  %s\n",
		"walk total = read+index+search",
		measure.Format(report.walk.Median), measure.Format(report.walk.P25), measure.Format(report.walk.P75),
		measure.SpanOf(perRepeat["walk"]).String())
	fmt.Printf("   %-42s %10s %10s %10s\n",
		"BFS from one root (once per attempt)",
		measure.Format(report.search.Median), measure.Format(report.search.P25), measure.Format(report.search.P75))
	fmt.Printf("   %-42s %10s %10s %10s  %s\n",
		"kern.proc.pgrp read (once per attempt)",
		measure.Format(report.group.Median), measure.Format(report.group.P25), measure.Format(report.group.P75),
		measure.SpanOf(perRepeat["group"]).String())

	if report.search.Median < floor {
		fmt.Printf("   the BFS median is below the resolvable floor (%s), so the model below charges the\n",
			measure.Format(floor))
		fmt.Printf("   floor for it as an UPPER bound rather than publishing an unresolvable figure.\n")
	}

	fmt.Printf("\n   sampler tick model, every term from the medians above:\n")
	fmt.Printf("     tick(peers) = read + index + peers x (BFS + group)\n")
	fmt.Printf("     %-24s %12s %14s\n", "peers (attempts)", "tick", "% of one core at "+recommendedCadence.String())
	for _, count := range peerCounts(peers) {
		tick := report.tick(count)
		fmt.Printf("     %-24d %12s %13.2f%%\n",
			count, measure.Format(tick), 100*float64(tick)/float64(recommendedCadence))
	}
	fmt.Println()

	tick := report.tick(peers)
	checks.Sample(invTickFits, tick < recommendedCadence, func() string {
		return fmt.Sprintf("tick at %d peers is %s, cadence is %s",
			peers, measure.Format(tick), recommendedCadence)
	})

	return report
}

func peerCounts(peers int) []int {
	counts := []int{1, 4}
	if peers != 4 && peers != 1 {
		counts = append(counts, peers)
	}
	counts = append(counts, 2*peers)

	return counts
}

// ----------------------------------------------------------------- overshoot

const invCadenceFits = "at the recommended cadence the runtime-wide peak stays inside per-uid headroom"

func reportOvershoot(checks *check.Set, rates rateReport, cost costReport, headroom headroomReport, peers int) {
	checks.Declare(invCadenceFits)

	fmt.Printf("4. transient peak per attempt = ceiling %d + rate x cadence\n", ceiling)
	fmt.Printf("   rate used: %.2f/s, measured this run as %q (%d processes in %s).\n",
		rates.fastest.perSecond, rates.fastest.label, rates.fastest.processes,
		measure.Format(rates.fastest.elapsed))
	fmt.Printf("   rounding rule: a process count is an integer and this is a hazard bound, so the\n")
	fmt.Printf("   program applies ceil() and prints the exact product beside it. Quote these digits.\n")
	fmt.Printf("   peer fan-out %d = logical CPUs; per-uid headroom %d.\n\n", peers, headroom.perUID)

	fmt.Printf("   %-9s %12s %8s %12s %10s %10s %9s\n",
		"cadence", "rate*cadence", "peak", "peak x"+fmt.Sprint(peers), "% headroom", "tick", "% core")
	for _, cadence := range []time.Duration{
		10 * time.Millisecond,
		25 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		250 * time.Millisecond,
		time.Second,
	} {
		exact := rates.fastest.perSecond * cadence.Seconds()
		peak := ceiling + int(math.Ceil(exact))
		runtimeWide := peak * peers
		tick := cost.tick(peers)
		fmt.Printf("   %-9s %12.2f %8d %12d %9.1f%% %10s %8.2f%%\n",
			cadence, exact, peak, runtimeWide,
			100*float64(runtimeWide)/float64(headroom.perUID),
			measure.Format(tick),
			100*float64(tick)/float64(cadence))

		if cadence == recommendedCadence {
			checks.Sample(invCadenceFits, runtimeWide < headroom.perUID, func() string {
				return fmt.Sprintf("peak x%d = %d against per-uid headroom %d",
					peers, runtimeWide, headroom.perUID)
			})
		}
	}
	fmt.Printf("   %% headroom is (peak x peers) / per-uid headroom, both from this run.\n\n")
}

// ------------------------------------------------------------------ pid wrap

func reportPIDWrap(rates rateReport) {
	fmt.Printf("5. pid reuse: how long the pid space takes to wrap\n")
	fmt.Printf("   pid span %d is a PLATFORM CONSTANT (darwin allocates to 99999 and restarts at 100),\n", pidSpan)
	fmt.Printf("   cited, not measured here. Everything below is that span over a measured rate.\n")

	fmt.Printf("   %-46s %10s %10s\n", "measured pid-consumption rate", "pids/s", "wrap")
	for _, measurement := range rates.all {
		if measurement.pidsConsumed == 0 {
			continue
		}
		fmt.Printf("   %-46s %10.0f %9.1fs   (pid counter advanced %d in %s)\n",
			measurement.label, measurement.pidRate, pidSpan/measurement.pidRate,
			measurement.pidsConsumed, measure.Format(measurement.elapsed))
	}
	if rates.fastest.perSecond > 0 {
		fmt.Printf("   %-46s %10.0f %9.1fs   (upper bound: a runaway as the only consumer)\n",
			"fastest measured creation rate", rates.fastest.perSecond, pidSpan/rates.fastest.perSecond)
	}
	fmt.Printf("   The pid counter advance counts every process the machine started, not only ours,\n")
	fmt.Printf("   which is why it is the honest input to a wrap estimate. Any wrap figure quoted for a\n")
	fmt.Printf("   spawn rate this probe did not measure has no source here.\n")
}

// ------------------------------------------------------------------ plumbing

func sysctlInt(name string) int {
	value, err := unix.SysctlUint32(name)
	check.Must(err)

	return int(value)
}

func firstExisting(candidates ...string) string {
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}

	return ""
}
