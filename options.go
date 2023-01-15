package ooze

import (
	"regexp"
	"strings"

	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/viruses"
)

type Option func(Options) Options

type Options struct {
	Repository               ooze.Repository
	TestRunner               laboratory.TestRunner
	TemporaryDir             laboratory.TemporaryDirectory
	MinimumThreshold         float32
	Parallel                 bool
	IgnoreSourceFilesPattern *regexp.Regexp
	Viruses                  []viruses.Virus
}

func WithRepositoryRoot(repositoryRoot string) func(Options) Options {
	return func(options Options) Options {
		options.Repository = fsrepository.New(repositoryRoot)

		return options
	}
}

func WithTestCommand(testCommand string) func(Options) Options {
	return func(options Options) Options {
		testCommandParts := strings.Split(testCommand, " ")
		options.TestRunner = cmdtestrunner.New(testCommandParts[0], testCommandParts[1:]...)

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
