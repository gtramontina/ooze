// Command census measures whether a parent-identity walk, a process-group
// census, or their union sees every live descendant of a supervised attempt
// root on darwin, and what each view costs.
//
// It exists because two closed decisions point in opposite directions.
// "Choose the automatic runaway fuse" (#60) ruled that the census must walk
// parent identity and that "group membership is a containment handle for
// signalling, not a count", citing a measured 26% disagreement. Independent
// research for "Define the supervised attempt contract" (#61) found the
// reciprocal failure: on darwin a descendant whose intermediate parent exits
// is reparented to launchd and leaves the parent walk, while keeping its
// process group.
//
// This program builds a tree containing one instance of each escape shape and
// asserts which view sees which, on every timed sample rather than once.
//
// # What the previous version of this file got wrong
//
// It printed the words "consistency check: union should equal walk + group"
// followed by two numbers, with no comparison, no assertion and no non-zero
// exit. The invariant inverted in 4 of 11 runs and the program exited 0 every
// time. It could not have worked as written: the map merge that makes the union
// exceed the walk plus the group costs about one tick of the monotonic clock,
// while the walk's own interquartile range is tens of microseconds. Nothing
// that compares medians of separately timed runs can resolve a difference a
// thousand times smaller than its own noise. Warming does not fix it, and
// measuring forwards and backwards does not fix it.
//
// So the comparison is now PAIRED: one census.Union call reads the process
// table, indexes it, walks it, reads the process group and merges the two sets,
// with the clock read at every boundary. Every published cost, including the
// bare table read and the walk that contains it, comes out of the same call, so
// the parts cannot contradict the whole. Each derived quantity is computed per
// sample and then summarized, because a median is not additive.
//
//	go run ./census
//	go run ./census -samples 1000 -repeats 5
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"supervisedattemptprobes/internal/census"
	"supervisedattemptprobes/internal/check"
	"supervisedattemptprobes/internal/measure"
)

const (
	modeEnv   = "OOZE_CENSUS_MODE"
	dirEnv    = "OOZE_CENSUS_DIR"
	shapeEnv  = "OOZE_CENSUS_SHAPE"
	setsidEnv = "OOZE_CENSUS_SETSID"

	settle = 400 * time.Millisecond

	// linger bounds every planted process, so nothing this probe creates can
	// outlive it even if the supervisor is killed. The orphan-setsid leaf leaves
	// both the parent walk and the process group, so a group signal cannot
	// reach it; the supervisor kills it by pid and this is the backstop.
	linger = 60 * time.Second
)

// shape is one escape shape, with the view expectations that #60 and the #61
// research between them predict. Every expectation is asserted on every sample.
type shape struct {
	name  string
	child string // which mode the root starts for this shape
	walk  bool   // the parent-identity walk is expected to see the leaf
	group bool   // the process-group census is expected to see the leaf
	why   string
}

var shapes = []shape{
	{"plain", "leaf", true, true, "direct child, root's group"},
	{"orphan", "middle", false, true, "double fork, no setsid: reparented to launchd, group kept"},
	{"regrouped", "leaf", true, false, "direct child that calls setpgid: left the group"},
	{"orphan-setsid", "middle", false, false, "double fork + setsid: left both (the platform limit)"},
}

func main() {
	switch os.Getenv(modeEnv) {
	case "root":
		root()
	case "middle":
		middle()
	case "leaf":
		leaf()
	default:
		os.Exit(supervise())
	}
}

// ---------------------------------------------------------------- supervisor

func supervise() int {
	samples := flag.Int("samples", 400, "timed samples per repeat")
	repeats := flag.Int("repeats", 3, "repeats of the whole timing block, so the output carries a range")
	bench := flag.Bool("bench", true, "also measure with testing.Benchmark as a second engine")
	flag.Parse()

	checks := check.New()
	resolution := measure.Resolution()
	clockCost := measure.ClockCost(*samples)

	fmt.Printf("probe: census - which darwin census view sees which escape shape, and what each costs\n")
	fmt.Printf("host: %s/%s, %d logical CPUs\n", runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("clock: monotonic; measured resolution %s (smallest non-zero delta over 200000 reads);\n",
		measure.Format(resolution))
	fmt.Printf("       resolvable floor %s (%dx resolution) - a median below it is not published;\n",
		measure.Format(time.Duration(measure.ResolvableFactor)*resolution), measure.ResolvableFactor)
	fmt.Printf("       one time.Now() call costs %s (median of %d batches of 100 reads), paid at every phase boundary\n",
		measure.Format(clockCost), *samples)
	fmt.Printf("statistic: nearest-rank median over pooled samples; every figure carries its n and unit;\n")
	fmt.Printf("       %d timed samples per repeat, %d repeats, n/2 warm-up samples discarded per run;\n",
		*samples, *repeats)
	fmt.Printf("       derived quantities are computed per sample and then summarized (a median is not additive)\n\n")

	population, err := census.Snapshot(os.Getuid())
	check.Must(err)
	fmt.Printf("population: %d processes system-wide (kern.proc.all), of which %d zombies;\n",
		population.SystemWide, population.Zombies)
	fmt.Printf("            %d for uid %d (kern.proc.ruid), %d by filtering kern.proc.all on real uid\n\n",
		population.ThisUID, population.UID, population.ThisUIDFromTable)

	rootPID, dir, cleanup := plant()
	defer cleanup()

	labels := readLabels(dir)
	fmt.Printf("attempt root pid %d, process group %d, descendants planted %d of %d\n\n",
		rootPID, rootPID, len(labels), len(shapes))

	const invPlanted = "every escape shape was planted"
	checks.Declare(invPlanted)
	checks.Sample(invPlanted, len(labels) == len(shapes), func() string {
		return fmt.Sprintf("planted %d of %d: %v", len(labels), len(shapes), labels)
	})

	pooled, perRepeat := sampleCensus(*samples, *repeats, rootPID, labels, resolution, checks)
	reportVisibility(labels, pooled)
	reportPairedCost(pooled, perRepeat, resolution, population)

	if *bench {
		reportEngines(rootPID, *samples, *repeats, pooled, *repeats)
	}

	reportUnionClaim(pooled, resolution, clockCost)

	fmt.Println()

	return checks.Report(os.Stdout,
		fmt.Sprintf("\ninvariants (each asserted on every one of %d paired samples)", pooled.count))
}

// ------------------------------------------------------------ paired samples

// pool holds every per-sample series the probe summarizes. The slices have one
// entry per sample and are index-aligned, which is what makes any comparison
// between them paired rather than a comparison of separate timing runs.
type pool struct {
	count int

	read   []time.Duration // kern.proc.all
	index  []time.Duration // children + liveness maps
	search []time.Duration // BFS from the attempt root
	group  []time.Duration // kern.proc.pgrp + liveness filter
	merge  []time.Duration // map union
	total  []time.Duration // clock-to-clock over the whole union call

	walk      []time.Duration // read + index + search, per sample
	overhead  []time.Duration // total - sum(phases), per sample
	unionOnly []time.Duration // total - walk, per sample: what the union adds

	first    census.Views
	firstSet bool
}

func sampleCensus(
	samples, repeats, rootPID int,
	labels map[string]int,
	resolution time.Duration,
	checks *check.Set,
) (*pool, []*pool) {
	declareInvariants(checks)

	all := &pool{}
	perRepeat := make([]*pool, 0, repeats)

	for range repeats {
		measure.Warm(samples, func() {
			_, _, err := census.Union(rootPID, rootPID)
			check.Must(err)
		})

		repeat := &pool{}
		for range samples {
			views, phases, err := census.Union(rootPID, rootPID)
			check.Must(err)
			repeat.add(views, phases)
			all.add(views, phases)
			assertSample(checks, views, phases, labels, resolution)
		}
		perRepeat = append(perRepeat, repeat)
	}

	return all, perRepeat
}

func (p *pool) add(views census.Views, phases census.Phases) {
	p.count++
	p.read = append(p.read, phases.Read)
	p.index = append(p.index, phases.Index)
	p.search = append(p.search, phases.Search)
	p.group = append(p.group, phases.Group)
	p.merge = append(p.merge, phases.Merge)
	p.total = append(p.total, phases.Total)

	walk := phases.Read + phases.Index + phases.Search
	p.walk = append(p.walk, walk)
	p.overhead = append(p.overhead, phases.Overhead())
	p.unionOnly = append(p.unionOnly, phases.Total-walk)

	if !p.firstSet {
		p.first, p.firstSet = views, true
	}
}

const (
	invAttribution = "union total >= read+index+search+group+merge (phase attribution)"
	invContainsRd  = "union total > the bare kern.proc.all read it performs"
	invWalkOverRd  = "walk total > the bare kern.proc.all read it performs"
	invUnionOverWk = "union total > walk total, by at least group+merge"
	invReadPos     = "kern.proc.all read phase is above the clock floor"
	invIndexPos    = "index phase is above the clock floor"
	invGroupPos    = "kern.proc.pgrp phase is above the clock floor"
	invUnionSet    = "union set == walk set U group set (recomputed independently)"
	invUnionSize   = "|union| >= |walk| and |union| >= |group|"
	invUnionCover  = "a shape is in the union exactly when some view sees it"
)

func declareInvariants(checks *check.Set) {
	checks.Declare(
		invAttribution, invContainsRd, invWalkOverRd, invUnionOverWk,
		invReadPos, invIndexPos, invGroupPos,
		invUnionSet, invUnionSize, invUnionCover,
	)
	for _, s := range shapes {
		checks.Declare(visibilityInvariant(s))
	}
}

func visibilityInvariant(s shape) string {
	return fmt.Sprintf("visibility: %-13s walk=%-6s group=%s", s.name, seenWord(s.walk), seenWord(s.group))
}

func assertSample(
	checks *check.Set,
	views census.Views,
	phases census.Phases,
	labels map[string]int,
	resolution time.Duration,
) {
	walk := phases.Read + phases.Index + phases.Search

	checks.Sample(invAttribution, phases.Total >= phases.Sum(), func() string {
		return fmt.Sprintf("total %s < phases %s", measure.Format(phases.Total), measure.Format(phases.Sum()))
	})
	checks.Sample(invContainsRd, phases.Total > phases.Read, func() string {
		return fmt.Sprintf("total %s <= read %s", measure.Format(phases.Total), measure.Format(phases.Read))
	})
	checks.Sample(invWalkOverRd, walk > phases.Read, func() string {
		return fmt.Sprintf("walk %s <= read %s", measure.Format(walk), measure.Format(phases.Read))
	})
	checks.Sample(invUnionOverWk, phases.Total-walk >= phases.Group+phases.Merge, func() string {
		return fmt.Sprintf("union-walk %s < group+merge %s",
			measure.Format(phases.Total-walk), measure.Format(phases.Group+phases.Merge))
	})
	checks.Sample(invReadPos, phases.Read > resolution, func() string {
		return "read " + measure.Format(phases.Read)
	})
	checks.Sample(invIndexPos, phases.Index > resolution, func() string {
		return "index " + measure.Format(phases.Index)
	})
	checks.Sample(invGroupPos, phases.Group > resolution, func() string {
		return "group " + measure.Format(phases.Group)
	})

	expected := map[int]bool{}
	for pid := range views.Walk {
		expected[pid] = true
	}
	for pid := range views.Group {
		expected[pid] = true
	}
	checks.Sample(invUnionSet, sameSet(views.Union, expected), func() string {
		return fmt.Sprintf("union %d entries, walk U group %d entries", len(views.Union), len(expected))
	})
	checks.Sample(invUnionSize,
		len(views.Union) >= len(views.Walk) && len(views.Union) >= len(views.Group),
		func() string {
			return fmt.Sprintf("|union| %d, |walk| %d, |group| %d",
				len(views.Union), len(views.Walk), len(views.Group))
		})

	cover := true
	for _, s := range shapes {
		pid, planted := labels[s.name]
		if !planted {
			continue
		}
		inWalk, inGroup, inUnion := views.Walk[pid], views.Group[pid], views.Union[pid]
		checks.Sample(visibilityInvariant(s), inWalk == s.walk && inGroup == s.group, func() string {
			return fmt.Sprintf("pid %d: walk=%s group=%s", pid, seenWord(inWalk), seenWord(inGroup))
		})
		if inUnion != (s.walk || s.group) {
			cover = false
		}
	}
	checks.Sample(invUnionCover, cover, func() string {
		return "a shape's union membership did not match either view"
	})
}

// ------------------------------------------------------------------- reports

func reportVisibility(labels map[string]int, pooled *pool) {
	fmt.Printf("view visibility (expectation asserted on every one of %d samples; observed column is sample 1)\n",
		pooled.count)
	fmt.Printf("  %-14s %-9s %-9s %-9s %-9s %-7s %s\n",
		"shape", "walk exp", "walk obs", "grp exp", "grp obs", "pid", "why")
	for _, s := range shapes {
		pid, planted := labels[s.name]
		if !planted {
			fmt.Printf("  %-14s %-9s %-9s %-9s %-9s %-7s %s\n",
				s.name, seenWord(s.walk), "NOT PLANTED", seenWord(s.group), "NOT PLANTED", "-", s.why)

			continue
		}
		fmt.Printf("  %-14s %-9s %-9s %-9s %-9s %-7d %s\n",
			s.name,
			seenWord(s.walk), seenWord(pooled.first.Walk[pid]),
			seenWord(s.group), seenWord(pooled.first.Group[pid]),
			pid, s.why)
	}

	fmt.Printf("\ncounts from sample 1 (live descendants, zombies excluded)\n")
	fmt.Printf("  parent walk from root : %d\n", len(pooled.first.Walk))
	fmt.Printf("  process-group census  : %d\n", len(pooled.first.Group))
	fmt.Printf("  union                 : %d\n", len(pooled.first.Union))
	fmt.Printf("  walk misses           : %d\n", len(missing(pooled.first.Union, pooled.first.Walk)))
	fmt.Printf("  group misses          : %d\n", len(missing(pooled.first.Union, pooled.first.Group)))
	fmt.Println()
}

func reportPairedCost(pooled *pool, perRepeat []*pool, resolution time.Duration, population census.Population) {
	fmt.Printf("A. paired cost: every phase timed inside ONE census.Union call, %d samples pooled from %d repeats\n",
		pooled.count, len(perRepeat))
	fmt.Printf("   process table at the start of the run: %d entries system-wide, %d for this uid\n",
		population.SystemWide, population.ThisUID)

	type row struct {
		name   string
		series func(*pool) []time.Duration
	}
	rows := []row{
		{"kern.proc.all read", func(p *pool) []time.Duration { return p.read }},
		{"index (children+live maps)", func(p *pool) []time.Duration { return p.index }},
		{"search (BFS from root)", func(p *pool) []time.Duration { return p.search }},
		{"walk total = read+index+search", func(p *pool) []time.Duration { return p.walk }},
		{"kern.proc.pgrp read + filter", func(p *pool) []time.Duration { return p.group }},
		{"merge (union of the two sets)", func(p *pool) []time.Duration { return p.merge }},
		{"union total (clock to clock)", func(p *pool) []time.Duration { return p.total }},
		{"union total - walk total", func(p *pool) []time.Duration { return p.unionOnly }},
		{"clock reads outside the phases", func(p *pool) []time.Duration { return p.overhead }},
	}

	fmt.Printf("   %-32s %10s %10s %10s  %-19s %s\n",
		"quantity", "median", "p25", "p75", "per-repeat medians", "publishable")
	for _, r := range rows {
		summary := measure.Summarize(r.series(pooled))
		ok, multiple := measure.Resolvable(summary.Median, resolution)
		verdict := fmt.Sprintf("yes (%.0fx clock)", multiple)
		if !ok {
			verdict = fmt.Sprintf("NO (%.1fx clock): below floor", multiple)
		}
		fmt.Printf("   %-32s %10s %10s %10s  %-19s %s\n",
			r.name,
			measure.Format(summary.Median),
			measure.Format(summary.P25),
			measure.Format(summary.P75),
			measure.SpanOf(medians(perRepeat, r.series)).String(),
			verdict)
	}
	fmt.Println()
}

func reportEngines(rootPID, samples, repeats int, pooled *pool, engineRepeats int) {
	fmt.Printf("B. cross-engine check: the same operations timed as whole calls by two instruments\n")
	fmt.Printf("   engine A: measure.Time, median of n=%d, %d repeats, one clock read per sample\n",
		samples, engineRepeats)
	fmt.Printf("   engine B: testing.Benchmark ns/op, its own loop, no per-iteration clock read, >=1s per operation\n")
	fmt.Printf("   engine C: the paired phase median from block A (n=%d)\n", pooled.count)
	fmt.Printf("   %-26s %-20s %-22s %-11s %s\n",
		"operation", "A per-repeat medians", "B ns/op (iterations)", "C paired", "published range A+B+C")

	operations := []struct {
		name   string
		call   func()
		paired []time.Duration
	}{
		{"bare kern.proc.all read", func() { _, _, err := census.Table(); check.Must(err) }, pooled.read},
		{"walk (read+index+search)", func() { _, _, err := census.Walk(rootPID); check.Must(err) }, pooled.walk},
		{"group (kern.proc.pgrp)", func() { _, _, err := census.Group(rootPID); check.Must(err) }, pooled.group},
		{"union (walk+group+merge)", func() { _, _, err := census.Union(rootPID, rootPID); check.Must(err) }, pooled.total},
		{"walk, index maps growing", func() { _, _, err := census.WalkGrowing(rootPID); check.Must(err) }, nil},
		{"union, index maps growing", func() { _, _, err := census.UnionGrowing(rootPID, rootPID); check.Must(err) }, nil},
	}

	for _, operation := range operations {
		repeatMedians := make([]time.Duration, 0, repeats)
		for range repeats {
			repeatMedians = append(repeatMedians, measure.Time(samples, operation.call).Median)
		}
		perOp, iterations := measure.PerOp(operation.call)

		candidates := append([]time.Duration{perOp}, repeatMedians...)
		paired := "n/a"
		if operation.paired != nil {
			median := measure.Summarize(operation.paired).Median
			paired = measure.Format(median)
			candidates = append(candidates, median)
		}
		fmt.Printf("   %-26s %-20s %-22s %-11s %s\n",
			operation.name,
			measure.SpanOf(repeatMedians).String(),
			fmt.Sprintf("%s (%d)", measure.Format(perOp), iterations),
			paired,
			measure.SpanOf(candidates).String())
	}
	fmt.Printf("   Quote the range. Engine B allocates a fresh table per iteration with no clock read\n")
	fmt.Printf("   between iterations, so it carries more garbage-collection work per operation than A.\n")
	fmt.Printf("   The last two rows are the same censuses with the index maps left to grow from empty,\n")
	fmt.Printf("   as an earlier draft built them, testing whether map growth explains a walk cost.\n")
	fmt.Printf("   This row-to-row comparison is NOT paired, so read it only if the gap exceeds the\n")
	fmt.Printf("   run-to-run spread in the same row.\n\n")
}

func reportUnionClaim(pooled *pool, resolution, clockCost time.Duration) {
	merge := measure.Summarize(pooled.merge)
	overhead := measure.Summarize(pooled.overhead)
	walk := measure.Summarize(pooled.walk)
	added := measure.Summarize(pooled.unionOnly)
	group := measure.Summarize(pooled.group)

	resolvable, multiple := measure.Resolvable(merge.Median, resolution)

	const boundaries = 6 // clock reads a union call makes: 2 outer plus 4 phase boundaries.

	fmt.Printf("C. the claim \"union should equal walk + group\"\n")
	fmt.Printf("   merge median %s over %d paired samples; clock resolution %s, so %.1fx the clock floor.\n",
		measure.Format(merge.Median), merge.N, measure.Format(resolution), multiple)
	fmt.Printf("   one union call reads the clock %d times at %s each = %s of instrumentation, of which\n",
		boundaries, measure.Format(clockCost), measure.Format(boundaries*clockCost))
	fmt.Printf("   %s falls outside the phases and the rest is charged to the phase that follows it.\n",
		measure.Format(overhead.Median))
	fmt.Printf("   So the merge term is the same order as this probe's own instrumentation.\n")
	fmt.Printf("   The walk's own IQR is %s, which is %.0fx the merge median. That ratio is why no\n",
		measure.Format(walk.IQR()), float64(walk.IQR())/float64(max(merge.Median, time.Duration(1))))
	fmt.Printf("   comparison of independently timed medians can resolve this term, at any sample count.\n")

	if resolvable {
		fmt.Printf("   VERDICT: RESOLVED on this host. union total = walk + group + merge, merge = %s.\n",
			measure.Format(merge.Median))
	} else {
		fmt.Printf("   VERDICT: the equality as stated is WITHDRAWN. The merge term sits below this\n")
		fmt.Printf("   instrument's resolvable floor (%dx %s = %s). Do not quote \"union = walk + group\"\n",
			measure.ResolvableFactor, measure.Format(resolution),
			measure.Format(time.Duration(measure.ResolvableFactor)*resolution))
		fmt.Printf("   or any comparison of separately timed medians of these three quantities.\n")
	}

	fmt.Printf("   WHAT SURVIVES, paired and per-sample over %d samples: the union strictly exceeds the\n", added.N)
	fmt.Printf("   walk it contains, by median %s, and the group read (median %s) accounts for it.\n",
		measure.Format(added.Median), measure.Format(group.Median))
	fmt.Printf("   That is invariant %q below - asserted, not printed.\n", invUnionOverWk)
}

// ------------------------------------------------------------------ the tree

func plant() (int, string, func()) {
	dir, err := os.MkdirTemp("", "ooze-census-")
	check.Must(err)

	self, err := os.Executable()
	check.Must(err)

	command := exec.Command(self)
	command.Env = append(os.Environ(), modeEnv+"=root", dirEnv+"="+dir)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	// Setpgid with Pgid 0 makes the child the leader of a new group, exactly as
	// process_tree_darwin.go:187 does to the launcher.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	check.Must(command.Start())

	rootPID := command.Process.Pid

	time.Sleep(settle)

	return rootPID, dir, func() {
		// Kill the group, then every planted pid by name: orphan-setsid left
		// both the group and the walk, so only its recorded pid can reach it.
		_ = syscall.Kill(-rootPID, syscall.SIGKILL)
		for _, pid := range readLabels(dir) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_, _ = command.Process.Wait()
		_ = os.RemoveAll(dir)
	}
}

func root() {
	dir := os.Getenv(dirEnv)
	self, _ := os.Executable()
	for _, s := range shapes {
		spawn(self, dir, s.child, s.name, s.name == "orphan-setsid")
	}
	time.Sleep(linger)
}

func middle() {
	dir := os.Getenv(dirEnv)
	self, _ := os.Executable()
	spawn(self, dir, "leaf", os.Getenv(shapeEnv), os.Getenv(setsidEnv) == "1")
	// Exit immediately, orphaning the leaf.
}

func leaf() {
	shapeName := os.Getenv(shapeEnv)
	if shapeName == "regrouped" {
		_ = syscall.Setpgid(0, 0)
	}
	if os.Getenv(setsidEnv) == "1" {
		_, _ = unix.Setsid()
	}
	_ = os.WriteFile(
		filepath.Join(os.Getenv(dirEnv), shapeName),
		[]byte(strconv.Itoa(os.Getpid())),
		0o600,
	)
	time.Sleep(linger)
}

func spawn(self, dir, mode, shapeName string, setsid bool) {
	command := exec.Command(self)
	command.Env = append(os.Environ(),
		modeEnv+"="+mode,
		dirEnv+"="+dir,
		shapeEnv+"="+shapeName,
		setsidEnv+"="+boolFlag(setsid),
	)
	_ = command.Start()
	if mode == "middle" {
		go func() { _, _ = command.Process.Wait() }()
	}
}

// ------------------------------------------------------------------ plumbing

func readLabels(dir string) map[string]int {
	labels := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return labels
	}
	for _, entry := range entries {
		contents, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(string(contents))
		if err != nil {
			continue
		}
		labels[entry.Name()] = pid
	}

	return labels
}

func medians(perRepeat []*pool, series func(*pool) []time.Duration) []time.Duration {
	out := make([]time.Duration, 0, len(perRepeat))
	for _, repeat := range perRepeat {
		out = append(out, measure.Summarize(series(repeat)).Median)
	}

	return out
}

func sameSet(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for pid := range a {
		if !b[pid] {
			return false
		}
	}

	return true
}

func missing(from, view map[int]bool) []int {
	var out []int
	for pid := range from {
		if !view[pid] {
			out = append(out, pid)
		}
	}
	sort.Ints(out)

	return out
}

func seenWord(seen bool) string {
	if seen {
		return "sees"
	}

	return "MISSES"
}

func boolFlag(value bool) string {
	if value {
		return "1"
	}

	return "0"
}
