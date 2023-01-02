//go:build mutation

package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
)

func TestMutation(t *testing.T) {
	ooze.Release(
		t,
		ooze.WithRepositoryRoot("."),
		ooze.WithTestCommand("go test -timeout=60s -count=1 ./..."),
		ooze.WithMinimumThreshold(0.5),
		ooze.WithViruses(
			integerincrement.New(),
		),
	)
}