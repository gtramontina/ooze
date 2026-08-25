package ooze

import (
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/fstemporarydir"
	"github.com/gtramontina/ooze/internal/gotextdiff"
	"github.com/gtramontina/ooze/internal/ignoredrepository"
	"github.com/gtramontina/ooze/internal/iologger"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/prettydiff"
	"github.com/gtramontina/ooze/internal/verboserepository"
	"github.com/gtramontina/ooze/internal/verbosetemporarydir"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/arithmetic"
	"github.com/gtramontina/ooze/viruses/arithmeticassignment"
	"github.com/gtramontina/ooze/viruses/arithmeticassignmentinvert"
	"github.com/gtramontina/ooze/viruses/bitwise"
	"github.com/gtramontina/ooze/viruses/comparison"
	"github.com/gtramontina/ooze/viruses/comparisoninvert"
	"github.com/gtramontina/ooze/viruses/comparisonreplace"
	"github.com/gtramontina/ooze/viruses/floatdecrement"
	"github.com/gtramontina/ooze/viruses/floatincrement"
	"github.com/gtramontina/ooze/viruses/integerdecrement"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/gtramontina/ooze/viruses/loopbreak"
	"github.com/gtramontina/ooze/viruses/loopcondition"
	"github.com/gtramontina/ooze/viruses/rangebreak"
)

var oozeVerbose *bool //nolint:gochecknoglobals

func init() { //nolint:gochecknoinits
	oozeVerbose = flag.Bool("ooze.v", false, "verbose: print additional output")
}

var defaultOptions = Options{ //nolint:gochecknoglobals
	Repository:                fsrepository.New("."),
	TestCommand:               []string{"go", "test", "-count=1", "./..."},
	TemporaryDir:              fstemporarydir.New("ooze-"),
	MinimumThreshold:          1.0,
	Serial:                    false,
	MutationTimeout:           0,
	IgnoreSourceFilesPatterns: nil,
	Viruses: []viruses.Virus{
		arithmetic.New(),
		arithmeticassignment.New(),
		arithmeticassignmentinvert.New(),
		bitwise.New(),
		comparison.New(),
		comparisoninvert.New(),
		comparisonreplace.New(),
		floatdecrement.New(),
		floatincrement.New(),
		integerdecrement.New(),
		integerincrement.New(),
		loopbreak.New(),
		loopcondition.New(),
		rangebreak.New(),
	},
}

// Release releases the ooze! It infects all source files with viruses that
// mutate the source code DNA and perform tests to determine whether the mutants
// survive.
//
// This is the entry point to configure and run mutation tests. You may want to
// configure it with some options. Here is the available options and their
// defaults:
//
//   - WithRepositoryRoot: `.`
//   - WithTestCommand: `go test -count=1 ./...`
//   - WithMinimumThreshold: `1.0`
//   - Serial: `false` (automatic managed admission is the default)
//   - WithMutationTimeout: baseline-derived
//   - IgnoreSourceFiles: `nil`
//   - WithViruses: all available (see viruses.Virus' implementations)
//
// The results are then presented in the console. If the mutation score is equal
// to or above the configured threshold (WithMinimumThreshold), the execution is
// considered successful. Failed otherwise. Regardless of the execution result,
// any surviving mutant (no tests failed after applying the source code
// mutation) will also be presented in the console for analysis.
func Release(t *testing.T, options ...Option) {
	t.Helper()

	opts := defaultOptions
	for _, option := range options {
		opts = option(opts)
	}

	var logger ooze.Logger = iologger.New(os.Stdout)

	if opts.IgnoreSourceFilesPatterns != nil {
		opts.Repository = ignoredrepository.New(opts.IgnoreSourceFilesPatterns, opts.Repository)
	}

	if verbose() {
		opts.Repository = verboserepository.New(logger, opts.Repository)
		opts.TemporaryDir = verbosetemporarydir.New(logger, opts.TemporaryDir)
	}

	logger.Logf("%s %s", color.Yellow("┃"), color.Green("Releasing Ooze…"))
	profile := ooze.AutomaticProfile
	if opts.Serial {
		profile = ooze.SerialProfile
	}
	managed := ooze.ProcessManagedRelease(ooze.ManagedReleaseConfiguration{
		Lineage: uint64(reflect.ValueOf(t).Pointer()), Repository: opts.Repository,
		TemporaryDir: opts.TemporaryDir, Command: opts.TestCommand,
		Environment: os.Environ(), Profile: profile, MutationTimeout: opts.MutationTimeout,
		Viruses: opts.Viruses,
	})
	if managed.Outcome == ooze.ManagedCleanupUnconfirmed {
		panic(managed.Cause)
	}
	if managed.Outcome != ooze.ManagedCompleted {
		t.Fail()
		return
	}
	if !summarizeManagedCompatibility(logger, managed.Mutations, opts.MinimumThreshold) {
		t.Fail()
	}
}

func summarizeManagedCompatibility(
	logger ooze.Logger,
	mutations []ooze.ManagedMutationResult,
	minimumThreshold float32,
) bool {
	detected := 0
	differ := prettydiff.New(gotextdiff.New())
	for _, mutation := range mutations {
		if mutation.Outcome != ooze.ManagedSurvived {
			detected++

			continue
		}
		logger.Logf("Mutant survived: %s\n%s", mutation.File.Label(), strings.TrimSpace(mutation.File.Diff(differ)))
	}
	score := float32(detected) / float32(len(mutations))
	logger.Logf("Mutation score: %.2f (minimum: %.2f)", score, minimumThreshold)

	return score >= minimumThreshold
}

func verbose() bool {
	return *oozeVerbose || testing.Verbose()
}
