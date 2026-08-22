// Command campaigncalib measures the one ratio a baseline-derived mutation
// deadline depends on: how much longer the slowest legitimate mutant attempt
// takes than the single baseline attempt the formula is allowed to observe.
//
// It reproduces a campaign's shape rather than benchmarking configurations
// independently, because the quantity of interest is contaminated by everything
// that happens between the baseline and the mutants — above all the Go build and
// test-result caches, which the baseline itself warms.
//
// One invocation is one campaign: fix a cache state, materialize a workspace
// from an immutable snapshot, run the unmutated command once and keep its wall
// clock, then run mutant attempts at the profile's concurrency and report the
// distribution of their wall clocks against that baseline.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type attempt struct {
	Index       int     `json:"index"`
	Wave        int     `json:"wave"`
	Mutated     string  `json:"mutated,omitempty"`
	Seconds     float64 `json:"seconds"`
	CPUSeconds  float64 `json:"cpu_seconds"`
	ExitCode    int     `json:"exit_code"`
	RatioToBase float64 `json:"ratio_to_baseline"`
}

type campaign struct {
	Label           string    `json:"label"`
	Profile         string    `json:"profile"`
	GOMAXPROCS      int       `json:"gomaxprocs"`
	Peers           int       `json:"peers"`
	CacheState      string    `json:"cache_state"`
	Command         string    `json:"command"`
	BaselineSeconds float64   `json:"baseline_seconds"`
	BaselineCPU     float64   `json:"baseline_cpu_seconds"`
	Attempts        []attempt `json:"attempts"`
	MutantMin       float64   `json:"mutant_min_seconds"`
	MutantMedian    float64   `json:"mutant_median_seconds"`
	MutantMax       float64   `json:"mutant_max_seconds"`
	RatioMedian     float64   `json:"ratio_median"`
	RatioMax        float64   `json:"ratio_max"`
	WallClockTotal  float64   `json:"wall_clock_total_seconds"`
	AttemptsPerSec  float64   `json:"attempts_per_wall_second"`
}

func main() {
	src := flag.String("src", "", "immutable source snapshot")
	work := flag.String("work", "", "scratch directory for materialized workspaces")
	cmdline := flag.String("cmd", "go test ./...", "test command run in each workspace")
	profile := flag.String("profile", "automatic", "automatic (GOMAXPROCS=1, P peers) or serial (GOMAXPROCS=P, exclusive)")
	capacity := flag.Int("capacity", runtime.NumCPU(), "detected capacity P")
	mutants := flag.Int("mutants", 14, "mutant attempts to run after the baseline")
	cache := flag.String("cache", "shared", "shared (inherit the ambient GOCACHE) or cold (fresh GOCACHE for this campaign)")
	peerOverride := flag.Int("peers", 0, "override the profile's concurrency, to measure the contention curve")
	runID := flag.String("runid", "", "unique token mixed into every workspace path so no path is ever reused")
	label := flag.String("label", "", "label for this campaign")
	flag.Parse()

	if *src == "" || *work == "" {
		fmt.Fprintln(os.Stderr, "-src and -work are required")
		os.Exit(2)
	}

	procs, peers := 1, *capacity
	if *profile == "serial" {
		procs, peers = *capacity, 1
	}

	if *peerOverride > 0 {
		peers = *peerOverride
	}

	run := campaign{
		Label: *label, Profile: *profile, GOMAXPROCS: procs, Peers: peers,
		CacheState: *cache, Command: *cmdline,
	}

	gocache := ""
	if *cache == "cold" {
		gocache = filepath.Join(*work, "gocache-"+*label)
		must(os.RemoveAll(gocache))
		must(os.MkdirAll(gocache, 0o750))
	}

	targets, err := mutableFiles(*src)
	must(err)

	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "no mutable source files found under -src")
		os.Exit(1)
	}

	began := time.Now()

	// The baseline: exclusive, unmutated, and the only observation the formula
	// is permitted. It is also what warms the build cache for everything after.
	baseDir := filepath.Join(*work, fmt.Sprintf("ws-%s-baseline", *runID))
	must(os.RemoveAll(baseDir))
	must(materialize(*src, baseDir))

	baseSeconds, baseCPU, baseCode := execute(baseDir, *cmdline, procs, gocache)
	run.BaselineSeconds, run.BaselineCPU = baseSeconds, baseCPU

	if baseCode != 0 {
		fmt.Fprintf(os.Stderr, "warning: baseline exited %d; a real campaign would abort here\n", baseCode)
	}

	must(os.RemoveAll(baseDir))
	fmt.Fprintf(os.Stderr, "%s baseline %.2fs\n", *label, baseSeconds)

	for done, wave := 0, 0; done < *mutants; wave++ {
		size := min(peers, *mutants-done)
		dirs := make([]string, size)
		picked := make([]string, size)

		for i := range size {
			dir := filepath.Join(*work, fmt.Sprintf("ws-%s-attempt-%d", *runID, done+i))
			must(os.RemoveAll(dir))
			must(materialize(*src, dir))

			// Spread mutations across packages the way a real catalogue does;
			// recompile cost depends on which package is touched.
			picked[i] = targets[(done+i)%len(targets)]
			must(mutate(filepath.Join(dir, picked[i]), done+i))
			dirs[i] = dir
		}

		results := make([]attempt, size)
		release := make(chan struct{})

		var group sync.WaitGroup
		for i := range size {
			group.Add(1)

			go func(i int) {
				defer group.Done()
				<-release

				seconds, cpu, code := execute(dirs[i], *cmdline, procs, gocache)
				results[i] = attempt{
					Index: done + i, Wave: wave, Mutated: picked[i],
					Seconds: seconds, CPUSeconds: cpu, ExitCode: code,
					RatioToBase: seconds / baseSeconds,
				}
			}(i)
		}

		close(release)
		group.Wait()

		run.Attempts = append(run.Attempts, results...)

		for i := range size {
			must(os.RemoveAll(dirs[i]))
		}

		done += size
		fmt.Fprintf(os.Stderr, "%s wave %d: %d attempts done\n", *label, wave, done)
	}

	run.WallClockTotal = time.Since(began).Seconds()
	summarize(&run)

	out, err := json.MarshalIndent(run, "", "  ")
	must(err)
	fmt.Println(string(out))
}

func execute(dir, cmdline string, procs int, gocache string) (float64, float64, int) {
	parts := strings.Fields(cmdline)
	cmd := exec.Command(parts[0], parts[1:]...) //nolint:gosec // calibration harness runs an operator-supplied command
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOMAXPROCS=%d", procs))

	if gocache != "" {
		cmd.Env = append(cmd.Env, "GOCACHE="+gocache)
	}

	began := time.Now()
	_ = cmd.Run()
	elapsed := time.Since(began).Seconds()

	state := cmd.ProcessState
	cpu := (state.UserTime() + state.SystemTime()).Seconds()

	return elapsed, cpu, state.ExitCode()
}

// mutate appends a unique comment. That changes the file's content hash, so its
// package and every dependent recompiles and their cached test results are
// invalidated — the same work a real mutant forces — while leaving behaviour
// untouched, so the attempt runs the whole suite like a surviving mutant does.
// Surviving mutants are the worst case for a deadline; a killed one exits early.
func mutate(path string, seq int) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(body, []byte(fmt.Sprintf("\n// calibration mutant %d\n", seq))...), 0o600)
}

func mutableFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		files = append(files, rel)

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(files)

	return files, nil
}

func skipDir(name string) bool {
	switch name {
	case ".git", ".devbox", "testdata", "docs":
		return true
	default:
		return false
	}
}

func materialize(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		if entry.IsDir() {
			if skipDir(entry.Name()) {
				return filepath.SkipDir
			}

			return os.MkdirAll(target, 0o750)
		}

		if !entry.Type().IsRegular() {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		return os.WriteFile(target, body, info.Mode().Perm())
	})
}

func summarize(run *campaign) {
	if len(run.Attempts) == 0 {
		return
	}

	seconds := make([]float64, 0, len(run.Attempts))
	for _, a := range run.Attempts {
		seconds = append(seconds, a.Seconds)
	}

	sort.Float64s(seconds)
	run.MutantMin = seconds[0]
	run.MutantMedian = seconds[len(seconds)/2]
	run.MutantMax = seconds[len(seconds)-1]
	run.RatioMedian = run.MutantMedian / run.BaselineSeconds
	run.RatioMax = run.MutantMax / run.BaselineSeconds
	run.AttemptsPerSec = float64(len(run.Attempts)) / math.Max(run.WallClockTotal, 1e-9)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
