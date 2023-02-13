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
		ooze.WithTestCommand("make test.failfast MAKEFLAGS="),
		ooze.WithMinimumThreshold(0.5),
		ooze.Parallel(),
		ooze.IgnoreSourceFiles("(^release\\.go$|testdata\\/.*)"),
	)
}
