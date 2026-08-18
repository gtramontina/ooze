//go:build linux

package cmdtestrunner_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const descendantSupervisionSupported = true

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
