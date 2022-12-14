package fakelaboratory

import (
	"reflect"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
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
	diagnostic          result.Result[string]
}

func NewResult(
	expectedRepository ooze.Repository,
	expectedMutatedFile *gomutatedfile.GoMutatedFile,
	diagnostic result.Result[string],
) *Result {
	return &Result{
		expectedRepository:  expectedRepository,
		expectedMutatedFile: expectedMutatedFile,
		diagnostic:          diagnostic,
	}
}

func New(tuples ...*Result) *FakeLaboratory {
	return &FakeLaboratory{
		always:  nil,
		results: tuples,
	}
}

func NewAlways(diagnostic result.Result[string]) *FakeLaboratory {
	return &FakeLaboratory{
		always:  diagnostic,
		results: []*Result{},
	}
}

func (l *FakeLaboratory) Test(repository ooze.Repository, file *goinfectedfile.GoInfectedFile) result.Result[string] {
	if l.always != nil {
		return l.always
	}

	for _, res := range l.results {
		sameRepository := repository == res.expectedRepository
		sameMutatedFile := reflect.DeepEqual(file.Mutate(), res.expectedMutatedFile)

		if sameRepository && sameMutatedFile {
			return res.diagnostic
		}
	}

	panic("unexpected mutated file")
}
