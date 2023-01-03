//go:build mutation

package ooze_test

import (
	"testing"

	"github.com/fatih/color"
	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
)

func TestMutation(t *testing.T) {
	color.NoColor = false

	ooze.Release(
		t,
		ooze.WithRepositoryRoot("."),
		ooze.WithTestCommand("make test"),
		ooze.WithMinimumThreshold(0.5),
		ooze.WithViruses(
			integerincrement.New(),
		),
	)
}
