//go:build windows

package cmdtestrunner_test

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

const descendantSupervisionSupported = true

// Windows has no process group or session a descendant can drop: job-object
// membership is inherited and cannot be shed without JOB_OBJECT_LIMIT_BREAKAWAY_OK,
// which the supervisor does not set. Breakaway is its own fixture.
const (
	descendantCanEscapeSupervision  = false
	sessionEscapeDefeatsContainment = false
)

func detachDescendantProcessGroup(*exec.Cmd) {}

func detachDescendantSession(*exec.Cmd) {}

// descendantIdentity has no meaning on Windows, which has neither a parent-based
// process hierarchy a supervisor can walk nor sessions of this kind. Escape
// fixtures skip before reaching it.
func descendantIdentity(int) (int, int, error) { return 0, 0, nil }

// stillActiveExitCode is the exit code Windows reports for a running process.
const stillActiveExitCode = 259

// descendantCanStillExecute reports whether the process remains able to run.
func descendantCanStillExecute(processID int) (bool, error) {
	process, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(processID), //nolint:gosec // Windows process IDs are 32-bit values.
	)
	if err != nil {
		return false, nil // No handle: the process is gone.
	}
	defer func() { _ = windows.CloseHandle(process) }()

	var code uint32
	err = windows.GetExitCodeProcess(process, &code)
	if err != nil {
		return false, fmt.Errorf("inspect process %d: %w", processID, err)
	}

	return code == stillActiveExitCode, nil
}

// terminateDescendant kills the process and waits, against the fixture's own
// budget, for it to signal exit. Fixtures own their teardown rather than
// relying on the supervisor they exercise.
func terminateDescendant(t *testing.T, processID int) {
	t.Helper()

	if processID <= 0 {
		return
	}

	access := uint32(windows.PROCESS_TERMINATE | windows.SYNCHRONIZE)
	process, err := windows.OpenProcess(access, false, uint32(processID)) //nolint:gosec // Windows process IDs are 32-bit values.
	if err != nil {
		return // Already gone: nothing to terminate.
	}
	defer func() { _ = windows.CloseHandle(process) }()

	err = windows.TerminateProcess(process, 1)
	if err != nil {
		t.Errorf("terminate descendant %d: %v", processID, err)

		return
	}

	budget := uint32(fixtureCleanupTimeout / time.Millisecond)
	status, err := windows.WaitForSingleObject(process, budget)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Errorf("descendant %d outlived the fixture by more than %s", processID, fixtureCleanupTimeout)
	}
}

func observeProcessExit(t *testing.T, processID int) (processExitObservation, error) {
	t.Helper()

	process, err := windows.OpenProcess(
		windows.SYNCHRONIZE,
		false,
		uint32(processID), //nolint:gosec // Windows process IDs are 32-bit values.
	)
	if err != nil {
		return nil, fmt.Errorf("open process %d to observe its exit: %w", processID, err)
	}
	t.Cleanup(func() { require.NoError(t, windows.CloseHandle(process)) })

	return func() {
		status, waitErr := windows.WaitForSingleObject(process, 0)
		require.NoError(t, waitErr)
		require.Equal(t, uint32(windows.WAIT_OBJECT_0), status)
	}, nil
}
