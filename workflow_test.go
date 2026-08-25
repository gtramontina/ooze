package ooze_test

import (
	"os"
	"strings"
	"testing"
)

func TestNativeWorkflowUsesSupportedToolchainsAndRejectsSkippedEvidence(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/os-compatibility.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)

	for _, required := range []string{
		"toolchain: devbox",
		"toolchain: raw-go",
		"if: ${{ matrix.toolchain == 'devbox' }}",
		"if: ${{ matrix.toolchain == 'raw-go' }}",
		"go-command: devbox run -- go",
		"Verify pinned Go 1.26.6",
		`go1.26.6`,
		`grep -q '"Action":"skip"'`,
		"unexpected skip in required native evidence",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("native workflow is missing %q", required)
		}
	}
}

func TestMutationWorkflowUsesDevboxExceptForNativeWindows(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/mutation.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)

	for _, required := range []string{
		"toolchain: devbox",
		"toolchain: raw-go",
		"if: ${{ matrix.toolchain == 'devbox' }}",
		"if: ${{ matrix.toolchain == 'raw-go' }}",
		"go-command: devbox run -- go",
		"test-command: devbox run -- go test",
		"Verify pinned Go 1.26.6",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("mutation workflow is missing %q", required)
		}
	}
}

func TestManualPerformanceWorkflowCollectsInterleavedNativeEvidence(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/os-compatibility.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)

	for _, required := range []string{
		"performance:",
		"PERFORMANCE_SAMPLES: 10",
		"github.event_name == 'workflow_dispatch'",
		"path: performance-baseline",
		"path: performance-candidate",
		"project-path: performance-candidate",
		"Collect interleaved A/B samples",
		"performance-evidence-${{ matrix.name }}",
		".github/performance/collect.sh",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("native workflow is missing performance evidence contract %q", required)
		}
	}
}

func TestSelfMutationCommandKeepsAutomaticProfileWithoutCrossingItsOwnFuse(t *testing.T) {
	mutationTest, err := os.ReadFile("ooze_mutation_test.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(mutationTest)
	if !strings.Contains(text, "-skip=^TestDarwinNativeSupervisorTripsAutomaticDescendantFuse$") {
		t.Error("self-mutation command does not exclude its deliberate 65-descendant fuse fixture")
	}
	if !strings.Contains(text, "^docs/prototypes/") {
		t.Error("self-mutation campaign includes separately tested nested prototype modules")
	}
	if strings.Contains(text, "ooze.Serial()") {
		t.Error("self-mutation campaign abandoned managed automatic admission")
	}
}

func TestManualNativeWorkflowRunsExactCandidateMutationGate(t *testing.T) {
	workflow, err := os.ReadFile(".github/workflows/os-compatibility.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(workflow)
	for _, required := range []string{
		"mutation-evidence:",
		"Mutation evidence / ${{ matrix.name }}",
		"github.event_name == 'workflow_dispatch'",
		"-timeout=30m -count=1 -v -tags=mutation",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("manual native workflow is missing mutation contract %q", required)
		}
	}
}
