package faketestrunner

import (
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/result"
)

type FakeTestRunner struct {
	results []*Result
}

func New(results ...*Result) *FakeTestRunner {
	return &FakeTestRunner{
		results: results,
	}
}

type Result struct {
	directoryPath string
	result        result.Result[string]
}

func NewResult(directoryPath string, result result.Result[string]) *Result {
	return &Result{directoryPath: directoryPath, result: result}
}

func (t *FakeTestRunner) Test(repository laboratory.TemporaryRepository) result.Result[string] {
	for _, runnerResult := range t.results {
		if runnerResult.directoryPath == repository.Root() {
			return runnerResult.result
		}
	}

	panic("faketestrunner: missing result configuration for given repository '" + repository.Root() + "'")
}
