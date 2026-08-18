//go:build darwin

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

	queue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("create process exit observation queue: %w", err)
	}
	t.Cleanup(func() { require.NoError(t, unix.Close(queue)) })

	changes := []unix.Kevent_t{{
		Ident:  uint64(processID), //nolint:gosec // Darwin process IDs are non-negative integers.
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
		Data:   0,
		Udata:  nil,
	}}
	_, err = unix.Kevent(queue, changes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("observe process %d exit: %w", processID, err)
	}

	return func() {
		events := make([]unix.Kevent_t, 1)
		observed, observeErr := unix.Kevent(queue, nil, events, &unix.Timespec{Sec: 0, Nsec: 0})
		require.NoError(t, observeErr)
		require.Equal(t, 1, observed)
		require.NotZero(t, events[0].Fflags&uint32(unix.NOTE_EXIT))
	}, nil
}
