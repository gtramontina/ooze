package ooze_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const releaseDispositionHelper = "OOZE_RELEASE_DISPOSITION_HELPER"

const managedReleaseHelper = "OOZE_MANAGED_RELEASE_HELPER"

const observedReleaseMarker = "OOZE_OBSERVED_RELEASE_MARKER"

const (
	managedSerialExpected = "OOZE_MANAGED_SERIAL_EXPECTED"
	managedSerialLock     = "OOZE_MANAGED_SERIAL_LOCK"
	managedSerialOverlap  = "OOZE_MANAGED_SERIAL_OVERLAP"
)

func TestReleaseRunsManagedBaselineAndMutation(t *testing.T) {
	repository := t.TempDir()
	{
		err := os.WriteFile(
			filepath.Join(repository, "source.go"),
			[]byte("package fixture\nvar number = 0\n"),
			0o600,
		)
		require.NoError(t, err)
	}
	t.Setenv(managedReleaseHelper, "1")

	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0]+" -test.run=^TestManagedReleaseCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
	)
}

func TestReleaseReportsCompletedCampaignThroughConfiguredReporter(t *testing.T) {
	repository := t.TempDir()
	err := os.WriteFile(
		filepath.Join(repository, "source.go"),
		[]byte("package fixture\nvar number = 0\n"),
		0o600,
	)
	require.NoError(t, err)
	t.Setenv(managedReleaseHelper, "1")
	reporter := &recordingReporter{}

	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0]+" -test.run=^TestManagedReleaseCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
		ooze.WithMinimumThreshold(0),
		ooze.WithReporter(reporter),
	)

	require.Len(t, reporter.results, 1)
	result := reporter.results[0]
	assert.Equal(t, ooze.Completed, result.Outcome)
	require.NotNil(t, result.Score)
	assert.Equal(t, ooze.Score{Detected: 1, Total: 1, Value: 1, Minimum: 0, Passed: true}, *result.Score)
	require.Len(t, result.Mutations, 1)
	mutation := result.Mutations[0]
	assert.Equal(t, ooze.Killed, mutation.Outcome)
	assert.Contains(t, mutation.Label, "source.go")
	assert.Contains(t, mutation.Diff, "var number = 1")
	assert.Equal(t, ooze.AttemptSettled, mutation.Primary.Kind)
	assert.False(t, mutation.Primary.Passed)
	assert.Nil(t, mutation.Confirmation)
}

func TestReleasePublishesCampaignEventsInAcceptedOrder(t *testing.T) {
	repository := t.TempDir()
	err := os.WriteFile(
		filepath.Join(repository, "source.go"),
		[]byte("package fixture\nvar number = 0\n"),
		0o600,
	)
	require.NoError(t, err)
	t.Setenv(managedReleaseHelper, "1")
	var events []ooze.CampaignEvent
	observer := observerFunc(func(event ooze.CampaignEvent) error {
		events = append(events, event)

		return nil
	})

	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0]+" -test.run=^TestManagedReleaseCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
		ooze.WithMinimumThreshold(0),
		ooze.WithReporter(&recordingReporter{}),
		ooze.WithObserver(observer),
	)

	assert.Equal(t, []ooze.CampaignEvent{
		ooze.CampaignStarted{},
		ooze.CatalogueDiscovered{Total: 1},
		ooze.BaselineStarted{},
		ooze.BaselineFinished{Passed: true},
		ooze.MutationStarted{Mutation: ooze.Mutation{Label: "source.go → Integer Increment"}},
		ooze.MutationFinished{
			Mutation: ooze.Mutation{Label: "source.go → Integer Increment"},
			Outcome:  ooze.Killed,
		},
		ooze.CampaignCompleted{},
	}, events)
}

func TestReleaseExecutionDoesNotWaitForObserver(t *testing.T) {
	repository := t.TempDir()
	err := os.WriteFile(
		filepath.Join(repository, "source.go"),
		[]byte("package fixture\nvar number = 0\n"),
		0o600,
	)
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "executed")
	t.Setenv(observedReleaseMarker, marker)
	entered := make(chan struct{})
	blocked := make(chan struct{})
	var once sync.Once
	observer := observerFunc(func(ooze.CampaignEvent) error {
		once.Do(func() { close(entered) })
		<-blocked

		return nil
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ooze.Release(t,
			ooze.WithRepositoryRoot(repository),
			ooze.WithTestCommand(os.Args[0]+" -test.run=^TestObservedReleaseCommandHelper$"),
			ooze.WithViruses(integerincrement.New()),
			ooze.WithMinimumThreshold(0),
			ooze.WithReporter(&recordingReporter{}),
			ooze.WithObserver(observer),
		)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(blocked)
		require.FailNow(t, "observer did not receive the campaign start")
	}
	executed := assert.Eventually(t, func() bool {
		_, statErr := os.Stat(marker)

		return statErr == nil
	}, 5*time.Second, 10*time.Millisecond)
	close(blocked)
	require.True(t, executed, "native execution waited for the blocked observer")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "release did not join observer delivery")
	}
}

func TestObservedReleaseCommandHelper(t *testing.T) {
	marker := os.Getenv(observedReleaseMarker)
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte("executed"), 0o600); err != nil {
		os.Exit(2)
	}
	data, err := os.ReadFile("source.go")
	if err != nil {
		os.Exit(2)
	}
	if strings.Contains(string(data), "number = 1") {
		os.Exit(1)
	}
}

type recordingReporter struct {
	results []ooze.Result
}

func (reporter *recordingReporter) Report(result ooze.Result) error {
	reporter.results = append(reporter.results, result)

	return nil
}

func TestManagedReleaseCommandHelper(t *testing.T) {
	role := os.Getenv(managedReleaseHelper)
	if role == "serial" {
		runManagedSerialHelper()
		return
	}
	if role != "1" {
		return
	}
	data, err := os.ReadFile("source.go")
	if err != nil {
		os.Exit(2)
	}
	if strings.Contains(string(data), "number = 1") {
		os.Exit(1)
	}
}

func TestReleaseSerialUsesDetectedCapacityWithoutMutantOverlap(t *testing.T) {
	repository := t.TempDir()
	{
		err := os.WriteFile(
			filepath.Join(repository, "source.go"),
			[]byte("package fixture\nvar first = 0\nvar second = 0\n"),
			0o600,
		)
		require.NoError(t, err)
	}
	coordination := t.TempDir()
	t.Setenv(managedReleaseHelper, "serial")
	t.Setenv(managedSerialExpected, strconv.Itoa(runtime.GOMAXPROCS(0)))
	t.Setenv(managedSerialLock, filepath.Join(coordination, "active"))
	t.Setenv(managedSerialOverlap, filepath.Join(coordination, "overlap"))

	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0]+" -test.run=^TestManagedReleaseCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
		ooze.Serial(),
	)
	{
		_, err := os.Stat(os.Getenv(managedSerialOverlap))
		assert.True(t, os.IsNotExist(err), "serial mutant commands overlapped: %v", err)
	}
}

func runManagedSerialHelper() {
	if os.Getenv("GOMAXPROCS") != os.Getenv(managedSerialExpected) {
		os.Exit(2)
	}
	data, err := os.ReadFile("source.go")
	if err != nil {
		os.Exit(2)
	}
	if !strings.Contains(string(data), " = 1") {
		return
	}
	lock, err := os.OpenFile(os.Getenv(managedSerialLock), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = os.WriteFile(os.Getenv(managedSerialOverlap), []byte("overlap"), 0o600)
		os.Exit(1)
	}
	_ = lock.Close()
	time.Sleep(50 * time.Millisecond)
	_ = os.Remove(os.Getenv(managedSerialLock))
	os.Exit(1)
}

func TestReleaseReportsInlineWithRealTestingDisposition(t *testing.T) {
	for _, test := range []struct {
		name, role   string
		wantFailure  bool
		want, absent []string
	}{
		{
			name: "completed pass", role: "pass",
			want: []string{"Mutant survived:", "✓ Score:     0.00 (minimum: 0.00)", "AFTER RELEASE"},
		},
		{
			name: "forced colors", role: "colors",
			want: []string{"\x1b[", "Mutant survived:", "AFTER RELEASE"},
		},
		{
			name: "threshold failure", role: "threshold", wantFailure: true,
			want: []string{"⨯ Score:     0.00 (minimum: 1.00)", "AFTER RELEASE"},
		},
		{
			name: "custom reporter preserves threshold failure", role: "custom-threshold", wantFailure: true,
			want:   []string{"CUSTOM REPORT: score=0.00 passed=false", "AFTER RELEASE"},
			absent: []string{"⨯ Score:"},
		},
		{
			name: "no mutants", role: "no-mutants", wantFailure: true,
			want: []string{
				"EVENT: ooze.CampaignFoundNoMutants", "No mutants were discovered. Nothing to score.", "AFTER RELEASE",
			},
			absent: []string{"Score:"},
		},
		{
			name: "failed baseline", role: "baseline", wantFailure: true,
			want: []string{
				"EVENT: ooze.CampaignAborted", "Campaign aborted. No mutation score.",
				"Cause: the unmutated baseline failed.",
				"FULL BASELINE FAILURE OUTPUT", "AFTER RELEASE",
			},
			absent: []string{"Score:"},
		},
		{
			name: "custom reporter receives baseline abort evidence", role: "custom-baseline", wantFailure: true,
			want: []string{
				"CUSTOM ABORT: baseline failed with output FULL BASELINE FAILURE OUTPUT", "AFTER RELEASE",
			},
			absent: []string{"Campaign aborted. No mutation score."},
		},
		{
			name: "reporter failure", role: "reporter-error", wantFailure: true,
			want:   []string{"AFTER RELEASE", "reporter failed: report failed"},
			absent: []string{"Score:"},
		},
		{
			name: "observer failure", role: "observer-error", wantFailure: true,
			want: []string{
				"Score:     0.00 (minimum: 0.00)", "AFTER RELEASE", "observer failed: observation failed",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestReleaseDispositionHelper$")
			command.Env = append(os.Environ(), releaseDispositionHelper+"="+test.role)
			output, err := command.CombinedOutput()
			require.Equal(t, test.wantFailure, (err != nil), "subprocess error = %v, want failure %t:\n%s", err, test.wantFailure, output)
			text := string(output)
			last := -1
			for _, fragment := range test.want {
				at := strings.Index(text, fragment)
				assert.False(t, at < 0, "output missing or reordered %q:\n%s", fragment, text)
				assert.False(t, at < last, "output missing or reordered %q:\n%s", fragment, text)
				last = at
			}
			for _, fragment := range test.absent {
				assert.NotContains(t, text, fragment, "output unexpectedly contains %q:\n%s", fragment, text)
			}
		})
	}
}

func TestReleaseVerboseModeObservesCampaignProgress(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestReleaseDispositionHelper$", "-test.v")
	command.Env = append(os.Environ(), releaseDispositionHelper+"=verbose")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "verbose helper failed:\n%s", output)
	text := string(output)
	for _, fragment := range []string{
		"campaign started",
		"discovered 1 mutant",
		"baseline started",
		"baseline passed",
		"mutation started: source.go → Integer Increment",
		"mutation survived: source.go → Integer Increment",
		"campaign completed",
	} {
		assert.Contains(t, text, fragment)
	}
	assert.NotContains(t, text, "setting up new temporary directory")
	assert.NotContains(t, text, "materializing all files")
}

func TestReleaseDispositionHelper(t *testing.T) {
	role := os.Getenv(releaseDispositionHelper)
	if role == "" {
		return
	}
	repository := t.TempDir()
	if role != "no-mutants" {
		{
			err := os.WriteFile(
				filepath.Join(repository, "source.go"),
				[]byte("package fixture\nvar number = 0\n"),
				0o600,
			)
			require.NoError(t, err)
		}
	}
	threshold := float32(0)
	if role == "threshold" || role == "custom-threshold" {
		threshold = 1
	}
	options := []ooze.Option{
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(os.Args[0] + " -test.run=^TestReleaseAlwaysPassCommandHelper$"),
		ooze.WithViruses(integerincrement.New()),
		ooze.WithMinimumThreshold(threshold),
	}
	if role == "colors" {
		options = append(options, ooze.ForceColors())
	}
	if role == "custom-threshold" {
		options = append(options, ooze.WithReporter(outputReporter{}))
	}
	if role == "custom-baseline" {
		options = append(options, ooze.WithReporter(abortReporter{}))
	}
	if role == "reporter-error" {
		options = append(options, ooze.WithReporter(errorReporter{}))
	}
	if role == "no-mutants" || role == "baseline" {
		options = append(options, ooze.WithObserver(eventOutputObserver{}))
	}
	if role == "observer-error" {
		options = append(options, ooze.WithObserver(errorObserver{}))
	}
	ooze.Release(t, options...)
	fmt.Println("AFTER RELEASE")
}

type outputReporter struct{}

func (outputReporter) Report(result ooze.Result) error {
	fmt.Printf("CUSTOM REPORT: score=%.2f passed=%t\n", result.Score.Value, result.Score.Passed)

	return nil
}

type eventOutputObserver struct{}

func (eventOutputObserver) Observe(event ooze.CampaignEvent) error {
	fmt.Printf("EVENT: %T\n", event)

	return nil
}

type abortReporter struct{}

func (abortReporter) Report(result ooze.Result) error {
	if result.Outcome != ooze.Aborted || result.Cause != ooze.BaselineFailed || result.Baseline == nil ||
		result.Baseline.Kind != ooze.AttemptSettled {
		return fmt.Errorf("unexpected abort result: %+v", result)
	}
	fmt.Printf("CUSTOM ABORT: baseline failed with output %s\n", strings.TrimSpace(result.Baseline.Output.Bytes))

	return nil
}

type errorReporter struct{}

func (errorReporter) Report(ooze.Result) error {
	return fmt.Errorf("report failed")
}

type errorObserver struct{}

func (errorObserver) Observe(ooze.CampaignEvent) error {
	return fmt.Errorf("observation failed")
}

func TestReleaseAlwaysPassCommandHelper(t *testing.T) {
	if role := os.Getenv(releaseDispositionHelper); role == "baseline" || role == "custom-baseline" {
		fmt.Println("FULL BASELINE FAILURE OUTPUT")
		os.Exit(1)
	}
}
