//go:build windows

package cmdtestrunner_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

const descendantSupervisionSupported = true

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
