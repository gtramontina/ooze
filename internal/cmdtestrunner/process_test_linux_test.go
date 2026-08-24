//go:build linux

package cmdtestrunner_test

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const descendantSupervisionSupported = true

const directRootProcessGroupEscapeDefeatsContainment = false

// Linux contains a session escape: the guardian arms PR_SET_CHILD_SUBREAPER
// before starting the command, so an orphaned descendant reparents to the
// guardian rather than to process 1, whatever session it moved to.
const sessionEscapeDefeatsContainment = false

// descendantIdentity reports the parent and session of a live process, read from
// the kernel rather than from anything the process itself claimed.
func descendantIdentity(processID int) (int, int, error) {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", processID))
	if err != nil {
		return 0, 0, fmt.Errorf("inspect process %d: %w", processID, err)
	}

	session, err := unix.Getsid(processID)
	if err != nil {
		return 0, 0, fmt.Errorf("read session of process %d: %w", processID, err)
	}

	for line := range strings.Lines(string(status)) {
		parent, found := strings.CutPrefix(strings.TrimSpace(line), "PPid:")
		if !found {
			continue
		}

		parentProcessID, convertErr := strconv.Atoi(strings.TrimSpace(parent))
		if convertErr != nil {
			return 0, session, fmt.Errorf("read parent of process %d: %w", processID, convertErr)
		}

		return parentProcessID, session, nil
	}

	return 0, session, nil
}

// descendantCanStillExecute reports whether the process remains able to run.
// A killed but unreaped process is still in the process table, so a bare
// kill(pid, 0) probe reports it as present and cannot tell it apart from a live
// one. The fixture therefore asks the question the containment contract asks --
// whether any process can still execute -- rather than whether a PID exists.
func descendantCanStillExecute(processID int) (bool, error) {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", processID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("inspect process %d: %w", processID, err)
	}

	return !strings.Contains(string(status), "State:\tZ"), nil
}

func observeProcessExit(t *testing.T, processID int) (processExitObservation, error) {
	t.Helper()

	processDescriptor, err := unix.PidfdOpen(processID, 0)
	if err != nil {
		return nil, fmt.Errorf("open process %d to observe its exit: %w", processID, err)
	}
	t.Cleanup(func() { require.NoError(t, unix.Close(processDescriptor)) })

	return func() {
		pollDescriptors := []unix.PollFd{{
			Fd:      int32(processDescriptor), //nolint:gosec // File descriptors fit int32 on Linux.
			Events:  unix.POLLIN,
			Revents: 0,
		}}
		observed, pollErr := unix.Poll(pollDescriptors, 0)
		require.NoError(t, pollErr)
		require.Equal(t, 1, observed)
		require.NotZero(t, pollDescriptors[0].Revents&unix.POLLIN)
	}, nil
}
