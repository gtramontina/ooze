package ooze

import "github.com/gtramontina/ooze/internal/viruses"

type Option func(*Options)

type Options struct {
	RepositoryRoot   string
	TestCommand      string
	MinimumThreshold float32
	Parallel         bool
	Viruses          []viruses.Virus
}

func WithRepositoryRoot(repositoryRoot string) func(*Options) {
	return func(options *Options) {
		options.RepositoryRoot = repositoryRoot
	}
}

func WithTestCommand(testCommand string) func(*Options) {
	return func(options *Options) {
		options.TestCommand = testCommand
	}
}

func WithMinimumThreshold(minimumThreshold float32) func(*Options) {
	return func(options *Options) {
		options.MinimumThreshold = minimumThreshold
	}
}

func Parallel() func(*Options) {
	return func(options *Options) {
		options.Parallel = true
	}
}

func WithViruses(virus viruses.Virus, rest ...viruses.Virus) func(*Options) {
	return func(options *Options) {
		options.Viruses = append([]viruses.Virus{virus}, rest...)
	}
}
