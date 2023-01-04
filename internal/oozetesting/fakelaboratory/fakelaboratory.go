package fakelaboratory

import (
	"reflect"

	"github.com/gtramontina/ooze/internal/future"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type FakeLaboratory struct {
	always  result.Result[string]
	results []*Result
}

type Result struct {
	expectedRepository  ooze.Repository
	expectedMutatedFile *gomutatedfile.GoMutatedFile
	output              result.Result[string]
}

func NewResult(
	expectedRepository ooze.Repository,
	expectedMutatedFile *gomutatedfile.GoMutatedFile,
	output result.Result[string],
) *Result {
	return &Result{
		expectedRepository:  expectedRepository,
		expectedMutatedFile: expectedMutatedFile,
		output:              output,
	}
}

func New(tuples ...*Result) *FakeLaboratory {
	return &FakeLaboratory{
		always:  nil,
		results: tuples,
	}
}

func NewAlways(output result.Result[string]) *FakeLaboratory {
	return &FakeLaboratory{
		always:  output,
		results: []*Result{},
	}
}

func (l *FakeLaboratory) Test(
	repository ooze.Repository,
	file *gomutatedfile.GoMutatedFile,
) future.Future[result.Result[string]] {
	if l.always != nil {
		return future.Resolved(l.always)
	}

	for _, res := range l.results {
		sameRepository := repository == res.expectedRepository
		sameMutatedFile := reflect.DeepEqual(file, res.expectedMutatedFile)

		if sameRepository && sameMutatedFile {
			return future.Resolved(res.output)
		}
	}

	panic("unexpected mutated file")
}
