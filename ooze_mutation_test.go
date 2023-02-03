//go:build mutation

package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/color"
)

func TestMutation(t *testing.T) {
	color.Force()

	ooze.Release(
		t,
		ooze.WithRepositoryRoot("."),
		ooze.WithTestCommand("make test.failfast MAKEFLAGS="),
		ooze.WithMinimumThreshold(0.5),
		ooze.Parallel(),
		ooze.IgnoreSourceFiles("(^release\\.go$|testdata\\/.*)"),
	)
}
