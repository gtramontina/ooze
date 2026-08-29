package ooze

import (
	"regexp"
	"strings"
	"time"

	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/viruses"
)

type Option func(Options) Options

type Options struct {
	Repository                ooze.Repository
	TestCommand               []string
	TemporaryDir              ooze.ManagedTemporaryDirectory
	MinimumThreshold          float32
	Serial                    bool
	MutationTimeout           time.Duration
	ForceColors               bool
	IgnoreSourceFilesPatterns []*regexp.Regexp
	Viruses                   []viruses.Virus
	Reporter                  Reporter
	Observer                  ProgressObserver
}

// WithRepositoryRoot configures which directory is the repository root. This is
// usually required when your mutation test file lives some other place that is
// not root itself.
func WithRepositoryRoot(repositoryRoot string) func(Options) Options {
	return func(options Options) Options {
		options.Repository = fsrepository.New(repositoryRoot)

		return options
	}
}

// WithTestCommand configures the test command to run, as string. You may
// configure it as you wish, as a `makefile` phony target, for example. Or
// simply run the standard `go test` command with extra flags. The command is
// opaque: an inner timeout that exits before Ooze's deadline is reported as an
// ordinary killed mutant rather than as timed out.
func WithTestCommand(testCommand string) func(Options) Options {
	return func(options Options) Options {
		testCommandParts := strings.Split(testCommand, " ")
		options.TestCommand = append([]string(nil), testCommandParts...)

		return options
	}
}

// WithMinimumThreshold represents the minimum mutation test score to consider
// the execution successful. A float between `0.0` and `1.0`.
func WithMinimumThreshold(minimumThreshold float32) func(Options) Options {
	return func(options Options) Options {
		options.MinimumThreshold = minimumThreshold

		return options
	}
}

// Serial runs each managed attempt process-locally exclusively while preserving
// the detected-capacity cooperative Go execution profile.
func Serial() func(Options) Options {
	return func(options Options) Options {
		options.Serial = true

		return options
	}
}

// WithMutationTimeout configures the absolute deadline for every primary and
// confirmation attempt. The baseline keeps its separate managed bound. Omitting
// this option leaves the deadline baseline-derived.
func WithMutationTimeout(timeout time.Duration) func(Options) Options {
	if timeout < 0 {
		panic("mutation timeout must be positive")
	}

	return func(options Options) Options {
		options.MutationTimeout = timeout

		return options
	}
}

// IgnoreSourceFiles configures regular expressions representing source files
// to be filtered out and not suffer any mutations.
func IgnoreSourceFiles(patterns ...string) func(Options) Options {
	return func(options Options) Options {
		for _, pattern := range patterns {
			options.IgnoreSourceFilesPatterns = append(options.IgnoreSourceFilesPatterns, regexp.MustCompile(pattern))
		}

		return options
	}
}

// WithViruses configure the list of viruses to infect the source files with.
// You can also implement your own viruses (generic or even
// application-specific).
func WithViruses(virus viruses.Virus, rest ...viruses.Virus) func(Options) Options {
	return func(options Options) Options {
		options.Viruses = append([]viruses.Virus{virus}, rest...)

		return options
	}
}

// ForceColors forces the use of colors in the output. This is useful when
// running the mutation tests in a CI environment, for example.
func ForceColors() func(Options) Options {
	return func(options Options) Options {
		options.ForceColors = true

		return options
	}
}

// WithReporter configures the reporter that publishes the terminal campaign result.
func WithReporter(reporter Reporter) func(Options) Options {
	if reporter == nil {
		panic("reporter must not be nil")
	}

	return func(options Options) Options {
		options.Reporter = reporter

		return options
	}
}

// WithObserver configures an observer for campaign domain events.
func WithObserver(observer ProgressObserver) func(Options) Options {
	if observer == nil {
		panic("observer must not be nil")
	}

	return func(options Options) Options {
		options.Observer = observer

		return options
	}
}
