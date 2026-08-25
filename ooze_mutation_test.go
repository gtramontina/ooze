//go:build mutation

package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze"
)

func TestMutation(t *testing.T) {
	ooze.Release(
		t,
		ooze.ForceColors(),
		ooze.WithRepositoryRoot("."),
		ooze.WithTestCommand("gotestsum --format-hide-empty-pkg --max-fails=1 -- -failfast -skip=^TestDarwinNativeSupervisorTripsAutomaticDescendantFuse$ ./..."),
		ooze.WithMinimumThreshold(0.5),
		ooze.IgnoreSourceFiles("(^release\\.go$|^docs/prototypes/|testdata\\/.*)"),
	)
}
