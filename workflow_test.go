package ooze_test

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workflowJob(t *testing.T, path, name string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
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
	require.FailNowf(t, "workflow job not found", "workflow %s has no %q job", path, name)
	return ""
}

func TestSelfMutationSubprocessDoesNotExcludeRootEntryPoints(t *testing.T) {
	configured := ooze.Options{}
	for _, option := range selfMutationOptions() {
		configured = option(configured)
	}
	var skip *regexp.Regexp
	for _, argument := range configured.TestCommand {
		if expression, found := strings.CutPrefix(argument, "-skip="); found {
			skip = regexp.MustCompile(expression)
		}
	}
	require.NotNil(t, skip, "self-mutation subprocess has no test-selection exclusion")
	assert.False(t, skip.MatchString("TestMutation"), "self-mutation subprocess excludes the root entry point")
	assert.False(t, skip.MatchString("TestOptions"), "self-mutation subprocess excludes an ordinary production test")
}

func TestSelfMutationSubprocessSkipsFilesystemReentrantNativeFixture(t *testing.T) {
	configured := ooze.Options{}
	for _, option := range selfMutationOptions() {
		configured = option(configured)
	}
	var skip *regexp.Regexp
	for _, argument := range configured.TestCommand {
		if expression, found := strings.CutPrefix(argument, "-skip="); found {
			skip = regexp.MustCompile(expression)
		}
	}
	require.NotNil(t, skip, "self-mutation subprocess has no test-selection exclusion")
	fixtures := []string{
		"TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember",
		"TestNativeSupervisorDrainsWideFanout",
	}
	for _, fixture := range fixtures {
		assert.True(t, skip.MatchString(fixture), "self-mutation subprocess selects filesystem-reentrant native fixture %q", fixture)
	}
	assert.False(t, skip.MatchString("TestManagedCampaignRunsBaselineBeforeOneAutomaticPrimary"), "self-mutation subprocess excludes an in-memory managed campaign fixture")
}

func TestSelfMutationSubprocessExecutesManagedCampaignFixture(t *testing.T) {
	configured := ooze.Options{}
	for _, option := range selfMutationOptions() {
		configured = option(configured)
	}
	arguments := make([]string, 0, len(configured.TestCommand)+1)
	for _, argument := range configured.TestCommand[1:] {
		switch argument {
		case "--format-hide-empty-pkg":
			argument = "--format=testname"
		}
		assert.NotContains(t, argument, `"`, "self-mutation subprocess argument contains literal quote characters: %q", argument)
		if strings.HasPrefix(argument, "./") {
			if argument == "./internal/ooze" {
				arguments = append(arguments,
					"-run=^(TestManagedProcessReturnsInvariantPresentationAfterEmergencySettlement|TestMutationAttemptPlanUsesAbsoluteOverrideUnchanged)$",
					argument,
				)
			}
			continue
		}
		arguments = append(arguments, argument)
	}
	command := exec.Command(configured.TestCommand[0], arguments...)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "run self-mutation subprocess selection probe: %v\n%s", err, output)
	fixtures := []string{
		"TestManagedProcessReturnsInvariantPresentationAfterEmergencySettlement",
		"TestMutationAttemptPlanUsesAbsoluteOverrideUnchanged",
	}
	for _, fixture := range fixtures {
		assert.Contains(t, string(output), fixture, "self-mutation subprocess skipped ordinary owning-package fixture %q:\n%s", fixture, output)
	}
}

func requireContract(t *testing.T, subject, contract string, required ...string) {
	t.Helper()
	for _, value := range required {
		assert.Contains(t, subject, value, "%s is missing %q", contract, value)
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
	require.NotNil(t, row, "matrix has no %q row", name)
	for key, value := range want {
		assert.Equal(t, value, row[key], "matrix %q %s = %q, want %q", name, key, row[key], value)
	}
}

func requireMatrixRunner(t *testing.T, job, runner string, want map[string]string) {
	t.Helper()
	for _, violation := range matrixRunnerContractViolations(t, job, runner, want) {
		assert.Fail(t, violation)
	}
}

func matrixRunnerContractViolations(t *testing.T, job, runner string, want map[string]string) []string {
	t.Helper()
	var violations []string
	found := false
	for name, row := range workflowMatrixRows(t, job) {
		if row["runner"] == runner {
			found = true
			for key, value := range want {
				if row[key] != value {
					violations = append(violations,
						fmt.Sprintf("matrix %q %s = %q, want %q", name, key, row[key], value))
				}
			}
		}
	}
	if !found {
		violations = append(violations, fmt.Sprintf("matrix has no row for runner %q", runner))
	}
	return violations
}

func TestMatrixRunnerContractChecksEveryMatchingRow(t *testing.T) {
	job := strings.Join([]string{
		"          - name: first",
		"            runner: ubuntu-24.04",
		"            toolchain: devbox",
		"            go-command: devbox run -- go",
		"          - name: second",
		"            runner: ubuntu-24.04",
		"            toolchain: raw-go",
		"            go-command: devbox run -- go",
	}, "\n")
	violations := matrixRunnerContractViolations(t, job, "ubuntu-24.04", map[string]string{
		"toolchain": "devbox", "go-command": "devbox run -- go",
	})
	assert.EqualValues(t, 1, len(violations), "runner contract violations=%#v, want the drifted second row", violations)
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

func requireMutationCampaignRows(t *testing.T, job string) {
	t.Helper()
	want := []string{
		"Ubuntu 24.04",
		"macOS 26",
		"Windows 2025",
	}
	rows := workflowMatrixRows(t, job)
	assert.Equal(t, len(want), len(rows), "mutation matrix has %d rows, want %d", len(rows), len(want))
	for _, name := range want {
		requireMatrixRow(t, job, name, nil)
	}
	assert.NotContains(t, job, "catalogue-shard:")
	assert.NotContains(t, job, "mutation-test:")
	assert.NotContains(t, job, "OOZE_", "mutation workflow uses a forbidden OOZE_* selector")
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
		"Install gotestsum 1.13.0",
		"go install gotest.tools/gotestsum@v1.13.0",
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

func TestMutationWorkflowRunsOneCampaignPerOperatingSystem(t *testing.T) {
	mutationJob := workflowJob(t, ".github/workflows/mutation.yml", "mutation")
	requireNativeToolchains(t, mutationJob)
	requireMutationCampaignRows(t, mutationJob)
	for _, name := range []string{
		"Ubuntu 24.04",
		"macOS 26",
	} {
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
		"-timeout=60m",
		"-run=^TestMutation$",
	)
}

func TestCIWorkflowRunsProductionSimulationContract(t *testing.T) {
	testJob := workflowJob(t, ".github/workflows/ci.yml", "test")
	requireContract(t, testJob, "CI production simulation contract",
		`name: "🎲 Production deterministic simulation"`,
		`devbox run -- go test -race -count=1 -run='^(TestSimulation|FuzzSimulation)' ./internal/ooze`,
	)
}

func TestLintTargetUsesAcceptedNoConfigGate(t *testing.T) {
	contents, err := os.ReadFile("makefile")
	require.NoError(t, err)
	makefile := strings.ReplaceAll(string(contents), "\r\n", "\n")
	requireContract(t, makefile, "lint target",
		"lint:\n\t@golangci-lint run --no-config ./...",
	)
}

func TestMutationTargetRunsOneCampaign(t *testing.T) {
	contents, err := os.ReadFile("makefile")
	require.NoError(t, err)
	makefile := strings.ReplaceAll(string(contents), "\r\n", "\n")
	requireContract(t, makefile, "mutation target",
		"test.mutation: $(pre-reqs)",
		"go test -timeout=60m -count=1 -v -tags=mutation -run=^TestMutation$",
	)
	assert.NotContains(t, makefile, "for test_name in")
}

func TestCompatibilityWorkflowInstallsSelfMutationTestTool(t *testing.T) {
	testJob := workflowJob(t, ".github/workflows/compatibility.yml", "test")
	requireContract(t, testJob, "compatibility test job",
		"Install gotestsum 1.13.0",
		"go install gotest.tools/gotestsum@v1.13.0",
	)
}

func TestSelfMutationCommandKeepsAutomaticProfileWithinOwningPackages(t *testing.T) {
	configured := ooze.Options{}
	for _, option := range selfMutationOptions() {
		configured = option(configured)
	}
	command := strings.Join(configured.TestCommand, " ")
	assert.Contains(t, command, "TestDarwinNativeSupervisorTripsAutomaticDescendantFuse", "self-mutation command does not exclude its deliberate 65-descendant fuse fixture")
	for _, argument := range configured.TestCommand {
		assert.NotEqual(t, ".", argument, "self-mutation command selects root or full-module package %q", argument)
		assert.NotEqual(t, "./...", argument, "self-mutation command selects root or full-module package %q", argument)
	}
	assert.False(t, configured.Serial, "self-mutation campaign abandoned managed automatic admission")
}
