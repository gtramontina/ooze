//go:build mutation

package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/viruses/floatdecrement"
	"github.com/gtramontina/ooze/viruses/floatincrement"
	"github.com/gtramontina/ooze/viruses/integerdecrement"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/gtramontina/ooze/viruses/loopbreak"
)

func TestMutation(t *testing.T) {
	color.Force()

	ooze.Release(
		t,
		ooze.WithRepositoryRoot("."),
		ooze.WithTestCommand("make test.failfast MAKEFLAGS="),
		ooze.WithMinimumThreshold(0.5),
		ooze.Parallel(),
		ooze.IgnoreSourceFiles("^release\\.go$"),
		ooze.WithViruses(
			floatdecrement.New(),
			floatincrement.New(),
			integerdecrement.New(),
			integerincrement.New(),
			loopbreak.New(),
		),
	)
}
