package ooze_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses/integerincrement"
)

const confirmationHelper = "OOZE_CONFIRMATION_HELPER"

type confirmationSample struct {
	Label                  string `json:"label"`
	Revision               string `json:"revision"`
	GOOS                   string `json:"goos"`
	GOARCH                 string `json:"goarch"`
	GoVersion              string `json:"go_version"`
	RunnerImage            string `json:"runner_image"`
	Race                   bool   `json:"race"`
	CommandIdentity        string `json:"command_identity"`
	TreeIdentity           string `json:"tree_identity"`
	Mutants                int    `json:"mutants"`
	CommandCount           int    `json:"command_count"`
	PrimaryCount           int    `json:"primary_count"`
	ConfirmationCount      int    `json:"confirmation_count"`
	ConfirmationTimeMS     int64  `json:"confirmation_time_ms"`
	CleanupEscalationCount int    `json:"cleanup_escalation_count"`
	CleanupEscalationBasis string `json:"cleanup_escalation_basis"`
	WallMilliseconds       int64  `json:"wall_ms"`
}

func TestPerformanceConfirmationEvidence(t *testing.T) {
	if os.Getenv(confirmationHelper) == "1" {
		runConfirmationCommandHelper(t)
		return
	}
	if runtime.GOMAXPROCS(0) < 2 {
		t.Fatal("confirmation evidence requires detected admission capacity of at least two")
	}
	repository := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(repository, "source.go"),
		[]byte("package fixture\nvar first = 0\nvar second = 0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	events := t.TempDir()
	t.Setenv(confirmationHelper, "1")
	t.Setenv("OOZE_CONFIRMATION_EVENTS", events)
	command, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	command += " -test.run=^TestPerformanceConfirmationEvidence$"

	captured, err := os.CreateTemp(t.TempDir(), "report-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = captured
	t.Cleanup(func() { os.Stdout = originalStdout })
	started := time.Now()
	ooze.Release(t,
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(command),
		ooze.WithMinimumThreshold(0),
		ooze.WithMutationTimeout(120*time.Millisecond),
		ooze.WithViruses(integerincrement.New()),
	)
	wall := time.Since(started)
	os.Stdout = originalStdout
	if err := captured.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := os.ReadFile(captured.Name())
	if err != nil {
		t.Fatal(err)
	}

	confirmationDurations := regexp.MustCompile(`exclusive confirmation [^\n]+ in ([0-9.]+(?:ns|us|µs|ms|s))`).FindAllStringSubmatch(string(report), -1)
	var confirmationTime time.Duration
	for _, match := range confirmationDurations {
		duration, parseErr := time.ParseDuration(match[1])
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		confirmationTime += duration
	}
	paths, err := filepath.Glob(filepath.Join(events, "*.start"))
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		counts[string(data)]++
	}
	confirmations := 0
	for identity, count := range counts {
		if strings.HasPrefix(identity, "mutant:") {
			confirmations += count - 1
		}
	}
	if len(paths) != 5 || confirmations != 2 || len(confirmationDurations) != 2 {
		t.Fatalf(
			"confirmation commands/count/report = %d/%d/%d, want 5/2/2\n%s",
			len(paths), confirmations, len(confirmationDurations), report,
		)
	}
	sample := confirmationSample{
		Label: "confirmation", Revision: os.Getenv("OOZE_PERFORMANCE_REVISION"),
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(),
		RunnerImage: os.Getenv("ImageOS"), Race: false,
		CommandIdentity: "current test binary; two hanging integer-increment mutants; 120ms resolved deadline",
		TreeIdentity:    "one Go helper process per attempt; no helper descendants",
		Mutants:         2, CommandCount: len(paths), PrimaryCount: 2,
		ConfirmationCount: confirmations, ConfirmationTimeMS: confirmationTime.Milliseconds(),
		CleanupEscalationCount: 4,
		CleanupEscalationBasis: "argued from fixture: four hanging commands reached their deadline and required forced execution-domain drainage",
		WallMilliseconds:       wall.Milliseconds(),
	}
	encoded, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("OOZE_CONFIRMATION %s\n", encoded)
}

func runConfirmationCommandHelper(t *testing.T) {
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
	identity := role + ":" + hex.EncodeToString(digest[:8])
	name := strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".start"
	if err := os.WriteFile(filepath.Join(os.Getenv("OOZE_CONFIRMATION_EVENTS"), name), []byte(identity), 0o600); err != nil {
		t.Fatal(err)
	}
	if role == "mutant" {
		time.Sleep(10 * time.Second)
	}
}
