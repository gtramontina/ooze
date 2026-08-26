package ooze_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const releaseDispositionHelper = "OOZE_RELEASE_DISPOSITION_HELPER"

const managedReleaseHelper = "OOZE_MANAGED_RELEASE_HELPER"

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
	assert.Equal(t, ooze.Killed, result.Mutations[0].Outcome)
	assert.Contains(t, result.Mutations[0].Label, "source.go")
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
			want:   []string{"No mutants were discovered. Nothing to score.", "AFTER RELEASE"},
			absent: []string{"Score:"},
		},
		{
			name: "failed baseline", role: "baseline", wantFailure: true,
			want: []string{
				"Campaign aborted. No mutation score.", "Cause: the unmutated baseline failed.",
				"FULL BASELINE FAILURE OUTPUT", "AFTER RELEASE",
			},
			absent: []string{"Score:"},
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
	ooze.Release(t, options...)
	fmt.Println("AFTER RELEASE")
}

type outputReporter struct{}

func (outputReporter) Report(result ooze.Result) error {
	fmt.Printf("CUSTOM REPORT: score=%.2f passed=%t\n", result.Score.Value, result.Score.Passed)

	return nil
}

func TestReleaseAlwaysPassCommandHelper(t *testing.T) {
	if os.Getenv(releaseDispositionHelper) == "baseline" {
		fmt.Println("FULL BASELINE FAILURE OUTPUT")
		os.Exit(1)
	}
}
