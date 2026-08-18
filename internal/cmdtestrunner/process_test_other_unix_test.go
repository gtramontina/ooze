//go:build !windows && !darwin && !linux

package cmdtestrunner_test

import "testing"

const descendantSupervisionSupported = false

func observeProcessExit(*testing.T, int) (processExitObservation, error) {
	return func() {}, nil
}
