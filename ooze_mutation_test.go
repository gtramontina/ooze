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
		ooze.WithTestCommand("gotestsum --format-hide-empty-pkg --max-fails=1 -- -timeout=60s -failfast ./..."),
		ooze.WithMinimumThreshold(0.5),
		ooze.Parallel(),
		ooze.IgnoreSourceFiles("(^release\\.go$|testdata\\/.*)"),
	)
}
