package ooze_test

import (
	"runtime"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses/integerincrement"
)

func performanceOptions(repository, command string) []ooze.Option {
	return []ooze.Option{
		ooze.WithRepositoryRoot(repository),
		ooze.WithTestCommand(command),
		ooze.WithMinimumThreshold(0),
		ooze.WithViruses(integerincrement.New()),
	}
}

func performanceExpectedBaselines() int { return 0 }

func performanceExpectedPeak() int { return 1 }

func performanceExpectedGOMAXPROCS() int { return runtime.GOMAXPROCS(0) }
