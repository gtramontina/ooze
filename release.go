package ooze

import (
	"flag"
	"os"
	"testing"

	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/consolereporter"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/fstemporarydir"
	"github.com/gtramontina/ooze/internal/gotextdiff"
	"github.com/gtramontina/ooze/internal/ignoredrepository"
	"github.com/gtramontina/ooze/internal/iologger"
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/prettydiff"
	"github.com/gtramontina/ooze/internal/scorecalculator"
	"github.com/gtramontina/ooze/internal/testingtlaboratory"
	"github.com/gtramontina/ooze/internal/verboselaboratory"
	"github.com/gtramontina/ooze/internal/verbosereporter"
	"github.com/gtramontina/ooze/internal/verboserepository"
	"github.com/gtramontina/ooze/internal/verbosetemporarydir"
	"github.com/gtramontina/ooze/internal/verbosetestrunner"
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
	TestRunner:                cmdtestrunner.New("go", "test", "-count=1", "./..."),
	TemporaryDir:              fstemporarydir.New("ooze-"),
	MinimumThreshold:          1.0,
	Parallel:                  false,
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
//   - Parallel: `false`
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

	var reporter ooze.Reporter = consolereporter.New(
		logger,
		prettydiff.New(gotextdiff.New()),
		scorecalculator.New(),
		opts.MinimumThreshold,
	)

	if opts.IgnoreSourceFilesPatterns != nil {
		opts.Repository = ignoredrepository.New(opts.IgnoreSourceFilesPatterns, opts.Repository)
	}

	if verbose() {
		opts.Repository = verboserepository.New(logger, opts.Repository)
		opts.TemporaryDir = verbosetemporarydir.New(logger, opts.TemporaryDir)
		opts.TestRunner = verbosetestrunner.New(logger, opts.TestRunner)
		reporter = verbosereporter.New(logger, reporter)
	}

	var lab ooze.Laboratory = laboratory.New(opts.TestRunner, opts.TemporaryDir)
	if verbose() {
		lab = verboselaboratory.New(logger, lab)
	}

	t.Cleanup(func() {
		t.Helper()
		res := reporter.Summarize()
		if !res.IsOk() {
			t.Fail()
		}
	})

	lab = testingtlaboratory.New(t, lab, opts.Parallel)

	logger.Logf("%s %s", color.Yellow("┃"), color.Green("Releasing Ooze…"))
	ooze.New(opts.Repository, lab, reporter).Release(
		opts.Viruses...,
	)
}

func verbose() bool {
	return *oozeVerbose || testing.Verbose()
}
