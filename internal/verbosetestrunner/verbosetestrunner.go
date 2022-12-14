package verbosetestrunner

import (
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type VerboseTestRunner struct {
	logger   ooze.Logger
	delegate laboratory.TestRunner
}

func New(logger ooze.Logger, delegate laboratory.TestRunner) *VerboseTestRunner {
	return &VerboseTestRunner{
		logger:   logger,
		delegate: delegate,
	}
}

func (r *VerboseTestRunner) Test(repository ooze.TemporaryRepository) result.Result[string] {
	r.logger.Logf("running tests on '%s'…", repository.Root())
	diagnostic := r.delegate.Test(repository)

	if diagnostic.IsOk() {
		r.logger.Logf("tests for '%s' failed; mutant killed", repository.Root())
	} else {
		r.logger.Logf("tests for '%s' passed; mutant survived", repository.Root())
	}

	return diagnostic
}
