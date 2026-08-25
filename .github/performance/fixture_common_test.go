package ooze_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze"
)

const performanceHelper = "OOZE_PERFORMANCE_HELPER"

type performanceEvent struct {
	PID         int    `json:"pid"`
	Role        string `json:"role"`
	Source      string `json:"source"`
	StartedNS   int64  `json:"started_ns"`
	FinishedNS  int64  `json:"finished_ns"`
	GOMAXPROCS  int    `json:"gomaxprocs"`
	GoMemorySys uint64 `json:"go_memory_sys_bytes"`
}

type performanceSample struct {
	Label                 string  `json:"label"`
	Revision              string  `json:"revision"`
	Sample                int     `json:"sample"`
	GOOS                  string  `json:"goos"`
	GOARCH                string  `json:"goarch"`
	GoVersion             string  `json:"go_version"`
	RunnerImage           string  `json:"runner_image"`
	Race                  bool    `json:"race"`
	CommandIdentity       string  `json:"command_identity"`
	TreeIdentity          string  `json:"tree_identity"`
	Mutants               int     `json:"mutants"`
	CommandCount          int     `json:"command_count"`
	WallMilliseconds      int64   `json:"wall_ms"`
	ThroughputPerSecond   float64 `json:"mutants_per_second"`
	PeakCommandProcesses  int     `json:"peak_command_processes"`
	ExpectedPeakProcesses int     `json:"expected_peak_processes"`
	PeakGoMemorySysBytes  uint64  `json:"peak_go_memory_sys_bytes"`
	ObservedGOMAXPROCS    []int   `json:"observed_gomaxprocs"`
	ConfirmationCount     int     `json:"confirmation_count"`
	ConfirmationTimeMS    int64   `json:"confirmation_time_ms"`
}

func TestPerformanceEvidence(t *testing.T) {
	if os.Getenv(performanceHelper) == "1" {
		runPerformanceCommandHelper(t)
		return
	}

	repository := t.TempDir()
	source := `package fixture
var n00 = 0
var n01 = 0
var n02 = 0
var n03 = 0
var n04 = 0
var n05 = 0
var n06 = 0
var n07 = 0
var n08 = 0
var n09 = 0
var n10 = 0
var n11 = 0
var n12 = 0
var n13 = 0
var n14 = 0
var n15 = 0
`
	if err := os.WriteFile(filepath.Join(repository, "source.go"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	events := t.TempDir()
	t.Setenv(performanceHelper, "1")
	t.Setenv("OOZE_PERFORMANCE_EVENTS", events)
	t.Setenv("OOZE_PERFORMANCE_EXPECTED_PEAK", strconv.Itoa(performanceExpectedPeak()))
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	command += " -test.run=^TestPerformanceEvidence$"
	started := time.Now()
	t.Cleanup(func() {
		writePerformanceSample(t, events, time.Since(started))
	})

	ooze.Release(t, performanceOptions(repository, command)...)
}

func runPerformanceCommandHelper(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("source.go")
	if err != nil {
		t.Fatal(err)
	}
	role := "baseline"
	if strings.Contains(string(data), "= 1") {
		role = "mutant"
	}
	digest := sha256.Sum256(data)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	marker := filepath.Join(
		os.Getenv("OOZE_PERFORMANCE_EVENTS"),
		fmt.Sprintf("started-%d-%d", os.Getpid(), started.UnixNano()),
	)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(marker) //nolint:errcheck // The temporary fixture directory is removed by testing.
	expectedPeak, err := strconv.Atoi(os.Getenv("OOZE_PERFORMANCE_EXPECTED_PEAK"))
	if err != nil || expectedPeak < 1 {
		t.Fatalf("invalid expected peak %q", os.Getenv("OOZE_PERFORMANCE_EXPECTED_PEAK"))
	}
	if role == "baseline" {
		expectedPeak = 1
	}
	if role == "mutant" {
		capacityReached := filepath.Join(os.Getenv("OOZE_PERFORMANCE_EVENTS"), "capacity-reached")
		deadline := time.Now().Add(3 * time.Second)
		for {
			if _, statErr := os.Stat(capacityReached); statErr == nil {
				break
			}
			startedCommands, globErr := filepath.Glob(filepath.Join(os.Getenv("OOZE_PERFORMANCE_EVENTS"), "started-*"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(startedCommands) >= expectedPeak {
				if writeErr := os.WriteFile(capacityReached, nil, 0o600); writeErr != nil {
					t.Fatal(writeErr)
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("only %d command processes started, want %d without a ramp", len(startedCommands), expectedPeak)
			}
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(40 * time.Millisecond)
	runtime.ReadMemStats(&after)
	memory := before.Sys
	if after.Sys > memory {
		memory = after.Sys
	}
	event := performanceEvent{
		PID: os.Getpid(), Role: role, Source: hex.EncodeToString(digest[:8]),
		StartedNS: started.UnixNano(), FinishedNS: time.Now().UnixNano(),
		GOMAXPROCS: runtime.GOMAXPROCS(0), GoMemorySys: memory,
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%d-%d.json", event.PID, event.StartedNS)
	if err := os.WriteFile(filepath.Join(os.Getenv("OOZE_PERFORMANCE_EVENTS"), name), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePerformanceSample(t *testing.T, directory string, wall time.Duration) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	events := make([]performanceEvent, 0, len(paths))
	baselines := 0
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var event performanceEvent
		if unmarshalErr := json.Unmarshal(data, &event); unmarshalErr != nil {
			t.Fatalf("decode %s: %v", path, unmarshalErr)
		}
		if event.Role == "baseline" {
			baselines++
		}
		events = append(events, event)
	}
	expectedBaselines := performanceExpectedBaselines()
	if baselines != expectedBaselines || len(events) != 16+expectedBaselines {
		t.Fatalf(
			"performance commands = %d with %d baselines, want %d/%d",
			len(events), baselines, 16+expectedBaselines, expectedBaselines,
		)
	}
	peakProcesses, peakMemory := performancePeaks(events)
	if peakProcesses != performanceExpectedPeak() {
		t.Fatalf("peak command processes = %d, want %d", peakProcesses, performanceExpectedPeak())
	}
	procs := make(map[int]struct{})
	for _, event := range events {
		procs[event.GOMAXPROCS] = struct{}{}
	}
	observed := make([]int, 0, len(procs))
	for value := range procs {
		observed = append(observed, value)
	}
	sort.Ints(observed)
	if len(observed) != 1 || observed[0] != performanceExpectedGOMAXPROCS() {
		t.Fatalf("observed GOMAXPROCS = %v, want [%d]", observed, performanceExpectedGOMAXPROCS())
	}
	mutants := 16
	sample := performanceSample{
		Label: os.Getenv("OOZE_PERFORMANCE_LABEL"), Revision: os.Getenv("OOZE_PERFORMANCE_REVISION"),
		Sample: performanceSampleNumber(t), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		GoVersion: runtime.Version(), RunnerImage: os.Getenv("ImageOS"), Race: false,
		CommandIdentity: "current test binary; TestPerformanceEvidence helper; 40ms healthy command",
		TreeIdentity:    "one Go helper process per attempt; no helper descendants",
		Mutants:         mutants, CommandCount: len(events), WallMilliseconds: wall.Milliseconds(),
		ThroughputPerSecond:  float64(mutants) / wall.Seconds(),
		PeakCommandProcesses: peakProcesses, ExpectedPeakProcesses: performanceExpectedPeak(),
		PeakGoMemorySysBytes: peakMemory,
		ObservedGOMAXPROCS:   observed, ConfirmationCount: 0, ConfirmationTimeMS: 0,
	}
	encoded, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("OOZE_PERF %s\n", encoded)
}

func performanceSampleNumber(t *testing.T) int {
	t.Helper()
	var sample int
	if _, err := fmt.Sscanf(os.Getenv("OOZE_PERFORMANCE_SAMPLE"), "%d", &sample); err != nil || sample < 1 {
		t.Fatalf("invalid OOZE_PERFORMANCE_SAMPLE %q", os.Getenv("OOZE_PERFORMANCE_SAMPLE"))
	}
	return sample
}

func performancePeaks(events []performanceEvent) (int, uint64) {
	type point struct {
		at     int64
		start  bool
		memory uint64
	}
	points := make([]point, 0, len(events)*2)
	for _, event := range events {
		points = append(points,
			point{at: event.StartedNS, start: true, memory: event.GoMemorySys},
			point{at: event.FinishedNS, start: false, memory: event.GoMemorySys},
		)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].at == points[j].at {
			return !points[i].start && points[j].start
		}
		return points[i].at < points[j].at
	})
	active, peak := 0, 0
	var memory, peakMemory uint64
	for _, point := range points {
		if point.start {
			active++
			memory += point.memory
			if active > peak {
				peak = active
			}
			if memory > peakMemory {
				peakMemory = memory
			}
			continue
		}
		active--
		memory -= point.memory
	}
	return peak, peakMemory
}
