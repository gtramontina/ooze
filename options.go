package ooze

import (
	"regexp"
	"strings"

	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/viruses"
)

type Option func(Options) Options

type Options struct {
	Repository                ooze.Repository
	TestRunner                laboratory.TestRunner
	TemporaryDir              laboratory.TemporaryDirectory
	MinimumThreshold          float32
	Parallel                  bool
	IgnoreSourceFilesPatterns []*regexp.Regexp
	Viruses                   []viruses.Virus
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
// simply run the standard `go test` command with extra flags, such as `timeout`
// and `tags`.
func WithTestCommand(testCommand string) func(Options) Options {
	return func(options Options) Options {
		testCommandParts := strings.Split(testCommand, " ")
		options.TestRunner = cmdtestrunner.New(testCommandParts[0], testCommandParts[1:]...)

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

// Parallel indicates whether to run the tests on the mutants in parallel. Given
// Ooze is executed via Go's testing framework, the level of parallelism can be
// configured when running the mutation tests. For example, with
// WithTestCommand(`go test -v -tags=mutation -parallel 3`).
func Parallel() func(Options) Options {
	return func(options Options) Options {
		options.Parallel = true

		return options
	}
}

// IgnoreSourceFiles configures a egular expressions representing source files
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
		color.Force()

		return options
	}
}
