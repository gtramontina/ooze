package ooze

import (
	"regexp"

	"github.com/gtramontina/ooze/internal/viruses"
)

type Option func(Options) Options

type Options struct {
	RepositoryRoot           string
	TestCommand              string
	MinimumThreshold         float32
	Parallel                 bool
	IgnoreSourceFilesPattern *regexp.Regexp
	Viruses                  []viruses.Virus
}

func WithRepositoryRoot(repositoryRoot string) func(Options) Options {
	return func(options Options) Options {
		options.RepositoryRoot = repositoryRoot

		return options
	}
}

func WithTestCommand(testCommand string) func(Options) Options {
	return func(options Options) Options {
		options.TestCommand = testCommand

		return options
	}
}

func WithMinimumThreshold(minimumThreshold float32) func(Options) Options {
	return func(options Options) Options {
		options.MinimumThreshold = minimumThreshold

		return options
	}
}

func Parallel() func(Options) Options {
	return func(options Options) Options {
		options.Parallel = true

		return options
	}
}

func IgnoreSourceFiles(pattern string) func(Options) Options {
	return func(options Options) Options {
		options.IgnoreSourceFilesPattern = regexp.MustCompile(pattern)

		return options
	}
}

func WithViruses(virus viruses.Virus, rest ...viruses.Virus) func(Options) Options {
	return func(options Options) Options {
		options.Viruses = append([]viruses.Virus{virus}, rest...)

		return options
	}
}
