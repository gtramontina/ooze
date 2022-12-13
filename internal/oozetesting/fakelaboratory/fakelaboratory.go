package fakelaboratory

import (
	"reflect"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type FakeLaboratory struct {
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
		results: tuples,
	}
}

func (l *FakeLaboratory) Test(repository ooze.Repository, file *goinfectedfile.GoInfectedFile) result.Result[string] {
	for _, res := range l.results {
		sameRepository := repository == res.expectedRepository
		sameMutatedFile := reflect.DeepEqual(file.Mutate(), res.expectedMutatedFile)

		if sameRepository && sameMutatedFile {
			return res.diagnostic
		}
	}

	panic("unexpected mutated file")
}
