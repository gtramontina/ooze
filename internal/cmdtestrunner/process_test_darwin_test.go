//go:build darwin

package cmdtestrunner_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const descendantSupervisionSupported = true

// darwinZombieState is the P_stat value of a process that has exited but has
// not been reaped. Such a process still occupies the process table.
const darwinZombieState = 5

// descendantIdentity reports the parent and session of a live process, read from
// the kernel rather than from anything the process itself claimed.
func descendantIdentity(processID int) (int, int, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", processID)
	if err != nil {
		return 0, 0, fmt.Errorf("inspect process %d: %w", processID, err)
	}

	session, err := unix.Getsid(processID)
	if err != nil {
		return 0, 0, fmt.Errorf("read session of process %d: %w", processID, err)
	}

	for _, process := range processes {
		if int(process.Proc.P_pid) == processID {
			return int(process.Eproc.Ppid), session, nil
		}
	}

	return 0, session, nil
}

// descendantCanStillExecute reports whether the process remains able to run.
// A killed but unreaped process is still in the process table, so a bare
// kill(pid, 0) probe reports it as present and cannot tell it apart from a live
// one. The fixture therefore asks the question the containment contract asks --
// whether any process can still execute -- rather than whether a PID exists.
func descendantCanStillExecute(processID int) (bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", processID)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return false, nil
		}

		return false, fmt.Errorf("inspect process %d: %w", processID, err)
	}

	for _, process := range processes {
		if int(process.Proc.P_pid) == processID && process.Proc.P_stat != darwinZombieState {
			return true, nil
		}
	}

	return false, nil
}

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
