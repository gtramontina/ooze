package ooze_test

import (
	"os"
	"strings"
	"testing"

	"github.com/gtramontina/ooze"
)

func workflowJob(t *testing.T, path, name string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	header := "  " + name + ":"
	start := -1
	for index, line := range lines {
		if line == header {
			start = index
			continue
		}
		if start >= 0 && strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.TrimSpace(line) != "" {
			return strings.Join(lines[start:index], "\n")
		}
	}
	if start >= 0 {
		return strings.Join(lines[start:], "\n")
	}
	t.Fatalf("workflow %s has no %q job", path, name)
	return ""
}

func requireContract(t *testing.T, subject, contract string, required ...string) {
	t.Helper()
	for _, value := range required {
		if !strings.Contains(subject, value) {
			t.Errorf("%s is missing %q", contract, value)
		}
	}
}

func workflowMatrixRows(t *testing.T, job string) map[string]map[string]string {
	t.Helper()
	rows := make(map[string]map[string]string)
	var row map[string]string
	for _, line := range strings.Split(job, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "          - ") {
			row = make(map[string]string)
			trimmed = strings.TrimPrefix(trimmed, "- ")
		}
		if row == nil || !strings.HasPrefix(line, "            ") && !strings.HasPrefix(line, "          - ") {
			continue
		}
		key, value, found := strings.Cut(trimmed, ": ")
		if !found {
			continue
		}
		row[key] = value
		if name := row["name"]; name != "" {
			rows[name] = row
		}
	}
	return rows
}

func requireMatrixRow(t *testing.T, job, name string, want map[string]string) {
	t.Helper()
	row := workflowMatrixRows(t, job)[name]
	if row == nil {
		t.Fatalf("matrix has no %q row", name)
	}
	for key, value := range want {
		if row[key] != value {
			t.Errorf("matrix %q %s = %q, want %q", name, key, row[key], value)
		}
	}
}

func requireMatrixRunner(t *testing.T, job, runner string, want map[string]string) {
	t.Helper()
	for name, row := range workflowMatrixRows(t, job) {
		if row["runner"] == runner {
			for key, value := range want {
				if row[key] != value {
					t.Errorf("matrix %q %s = %q, want %q", name, key, row[key], value)
				}
			}
			return
		}
	}
	t.Fatalf("matrix has no row for runner %q", runner)
}

func requireNativeToolchains(t *testing.T, job string) {
	t.Helper()
	requireMatrixRunner(t, job, "ubuntu-24.04", map[string]string{
		"toolchain": "devbox", "go-command": "devbox run -- go",
	})
	requireMatrixRunner(t, job, "macos-26", map[string]string{
		"toolchain": "devbox", "go-command": "devbox run -- go",
	})
	requireMatrixRunner(t, job, "windows-2025", map[string]string{
		"toolchain": "raw-go", "go-command": "go",
	})
}

func TestNativeWorkflowUsesSupportedToolchainsAndRejectsSkippedEvidence(t *testing.T) {
	testJob := workflowJob(t, ".github/workflows/os-compatibility.yml", "test")
	requireNativeToolchains(t, testJob)
	requireContract(t, testJob, "native test job",
		"toolchain: devbox",
		"toolchain: raw-go",
		"if: ${{ matrix.toolchain == 'devbox' }}",
		"if: ${{ matrix.toolchain == 'raw-go' }}",
		"go-command: devbox run -- go",
		"Verify pinned Go 1.26.6",
		`go1.26.6`,
		`grep -q '"Action":"skip"'`,
		"unexpected skip in required native evidence",
	)
	stressJob := workflowJob(t, ".github/workflows/os-compatibility.yml", "stress")
	requireNativeToolchains(t, stressJob)
	requireContract(t, stressJob, "native stress job",
		"github.event_name == 'push' || github.event_name == 'schedule' || github.event_name == 'workflow_dispatch'",
		"-count=10", `grep -q '"Action":"skip"'`, "unexpected skip in required native evidence",
	)
}

func TestMutationWorkflowUsesDevboxExceptForNativeWindows(t *testing.T) {
	mutationJob := workflowJob(t, ".github/workflows/mutation.yml", "mutation")
	requireNativeToolchains(t, mutationJob)
	for _, name := range []string{"Ubuntu 24.04", "macOS 26"} {
		requireMatrixRow(t, mutationJob, name, map[string]string{"test-command": "devbox run -- go test"})
	}
	requireMatrixRow(t, mutationJob, "Windows 2025", map[string]string{"test-command": "go test"})
	requireContract(t, mutationJob, "mutation job",
		"toolchain: devbox",
		"toolchain: raw-go",
		"if: ${{ matrix.toolchain == 'devbox' }}",
		"if: ${{ matrix.toolchain == 'raw-go' }}",
		"go-command: devbox run -- go",
		"test-command: devbox run -- go test",
		"Verify pinned Go 1.26.6",
	)
}

func TestManualPerformanceWorkflowCollectsInterleavedNativeEvidence(t *testing.T) {
	performanceJob := workflowJob(t, ".github/workflows/os-compatibility.yml", "performance")
	requireNativeToolchains(t, performanceJob)
	requireContract(t, performanceJob, "performance job",
		"PERFORMANCE_SAMPLES: 10",
		"github.event_name == 'workflow_dispatch'",
		"path: performance-baseline",
		"path: performance-candidate",
		"project-path: performance-candidate",
		"Collect interleaved A/B samples",
		"performance-evidence-${{ matrix.name }}",
		".github/performance/collect.sh",
	)
}

func TestSelfMutationCommandKeepsAutomaticProfileWithoutCrossingItsOwnFuse(t *testing.T) {
	configured := ooze.Options{}
	for _, option := range selfMutationOptions() {
		configured = option(configured)
	}
	command := strings.Join(configured.TestCommand, " ")
	if !strings.Contains(command, "-skip=^TestDarwinNativeSupervisorTripsAutomaticDescendantFuse$") {
		t.Error("self-mutation command does not exclude its deliberate 65-descendant fuse fixture")
	}
	excludesPrototype := false
	for _, pattern := range configured.IgnoreSourceFilesPatterns {
		excludesPrototype = excludesPrototype || pattern.MatchString("docs/prototypes/deadline-calibration/main.go")
	}
	if !excludesPrototype {
		t.Error("self-mutation campaign includes separately tested nested prototype modules")
	}
	if configured.Serial {
		t.Error("self-mutation campaign abandoned managed automatic admission")
	}
}

func TestManualNativeWorkflowRunsExactCandidateMutationGate(t *testing.T) {
	mutationJob := workflowJob(t, ".github/workflows/os-compatibility.yml", "mutation-evidence")
	requireNativeToolchains(t, mutationJob)
	requireContract(t, mutationJob, "manual mutation evidence job",
		"Mutation evidence / ${{ matrix.name }}",
		"Mutation campaign acceptance",
		"github.event_name == 'workflow_dispatch'",
		"-timeout=30m -count=1 -v -tags=mutation",
	)
}
