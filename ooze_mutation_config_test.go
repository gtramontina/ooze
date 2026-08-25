package ooze_test

import "github.com/gtramontina/ooze"

func selfMutationOptions() []ooze.Option {
	return []ooze.Option{
		ooze.ForceColors(),
		ooze.WithRepositoryRoot("."),
		ooze.WithTestCommand("gotestsum --format-hide-empty-pkg --max-fails=1 -- -failfast -skip=^TestDarwinNativeSupervisorTripsAutomaticDescendantFuse$ ./..."),
		ooze.WithMinimumThreshold(0.5),
		ooze.IgnoreSourceFiles("(^release\\.go$|^docs/prototypes/|testdata\\/.*)"),
	}
}
