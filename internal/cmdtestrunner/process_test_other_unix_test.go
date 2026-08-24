//go:build !windows && !darwin && !linux

package cmdtestrunner_test

import (
	"os/exec"
	"testing"
)

const descendantSupervisionSupported = false

const (
	descendantCanEscapeSupervision                 = false
	directRootProcessGroupEscapeDefeatsContainment = false
	sessionEscapeDefeatsContainment                = false
	descendantParentIsObservable                   = false
)

func detachDescendantProcessGroup(*exec.Cmd) {}

func detachDescendantSession(*exec.Cmd) {}

func descendantCanStillExecute(int) (bool, error) { return false, nil }

func descendantIdentity(int) (int, int, error) { return 0, 0, nil }

func terminateDescendant(*testing.T, int) {}

func observeProcessExit(*testing.T, int) (processExitObservation, error) {
	return func() {}, nil
}
