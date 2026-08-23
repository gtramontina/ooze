//go:build darwin || linux

package cmdtestrunner_test

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// descendantCanEscapeSupervision reports whether this operating system gives a
// descendant a way to drop the containment handle its supervisor relies on.
// Where it does not, a fixture that tries to escape proves nothing and must say
// so rather than reporting a pass.
const descendantCanEscapeSupervision = true

// detachDescendantProcessGroup makes the descendant its own process-group
// leader, so it leaves the process group the supervisor censuses.
func detachDescendantProcessGroup(command *exec.Cmd) {
	//nolint:exhaustruct // Other process attributes retain OS defaults.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// detachDescendantSession makes the descendant a session leader, which also
// gives it a new process group. Paired with the relay's second fork, this
// leaves the eventual writer in neither the supervised group nor any ancestry
// leading back to the supervised root.
func detachDescendantSession(command *exec.Cmd) {
	//nolint:exhaustruct // Other process attributes retain OS defaults.
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// terminateDescendant kills the process and waits, against the fixture's own
// budget, until it can no longer execute. Fixtures own their teardown rather
// than relying on the supervisor they exercise: an escapee is by definition one
// the supervisor does not reach, so a fixture that left cleanup to the
// supervisor would leak exactly when it caught a defect.
func terminateDescendant(t *testing.T, processID int) {
	t.Helper()

	if processID <= 0 {
		return
	}

	err := unix.Kill(processID, unix.SIGKILL)
	if err != nil && !errors.Is(err, unix.ESRCH) {
		t.Errorf("kill descendant %d: %v", processID, err)

		return
	}

	deadline := time.Now().Add(fixtureCleanupTimeout)
	for {
		canExecute, inspectErr := descendantCanStillExecute(processID)
		if inspectErr != nil {
			t.Errorf("inspect descendant %d: %v", processID, inspectErr)

			return
		}
		if !canExecute {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("descendant %d could still execute %s after SIGKILL", processID, fixtureCleanupTimeout)

			return
		}
		time.Sleep(time.Millisecond)
	}
}
