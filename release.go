package ooze

import (
	"flag"
	"os"
	"reflect"
	"testing"

	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/fstemporarydir"
	"github.com/gtramontina/ooze/internal/ignoredrepository"
	"github.com/gtramontina/ooze/internal/iologger"
	"github.com/gtramontina/ooze/internal/ooze"
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
	ForceColors:               false,
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
//   - ForceColors: `false`
//
// Automatic execution manages aggregate process-local admission. Serial chooses
// process-local exclusive attempts while preserving the detected-capacity Go
// execution profile. WithMutationTimeout is the sole absolute mutation-deadline
// override.
//
// Release writes one report to stdout before returning. Only a completed
// campaign publishes a score; it succeeds when the float32 detected/total score
// is greater than or equal to WithMinimumThreshold. NoMutants and infrastructure
// aborts fail without a score. Cleanup uncertainty and invariant violations emit
// one consolidated diagnostic and panic once. Go may discard stdout for passing
// package-pattern runs without -v, so use go test -v when retaining the report is
// required.
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

	colorsEnabled := opts.ForceColors || color.EnabledByDefault()
	palette := color.NewPalette(colorsEnabled)
	logger.Logf("%s %s", palette.Yellow("┃"), palette.Green("Releasing Ooze…"))
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
	report := ooze.ProjectManagedReport(managed, opts.MinimumThreshold, opts.Serial, colorsEnabled)
	publishManagedReport(t, logger, report)
}

func publishManagedReport(t *testing.T, logger ooze.Logger, report ooze.ManagedReport) {
	t.Helper()
	logger.Logf("%s", report.Text)
	switch report.Disposition {
	case ooze.ManagedReportPass:
		return
	case ooze.ManagedReportError:
		t.Errorf("ooze: %s", report.CallerMessage)
	case ooze.ManagedReportPanic:
		panic(report.PanicValue)
	default:
		panic("ooze: report disposition is invalid")
	}
}

func verbose() bool {
	return *oozeVerbose || testing.Verbose()
}
