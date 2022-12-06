package fakelaboratory

import (
	"reflect"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/result"
)

type FakeLaboratory struct {
	tuples []*Tuple
}

type Tuple struct {
	file       *gomutatedfile.GoMutatedFile
	diagnostic result.Result[string]
}

func NewTuple(file *gomutatedfile.GoMutatedFile, diagnostic result.Result[string]) *Tuple {
	return &Tuple{file: file, diagnostic: diagnostic}
}

func New(tuples ...*Tuple) *FakeLaboratory {
	return &FakeLaboratory{
		tuples: tuples,
	}
}

func (l *FakeLaboratory) Test(mutatedFile *gomutatedfile.GoMutatedFile) result.Result[string] {
	for _, tuple := range l.tuples {
		if reflect.DeepEqual(mutatedFile, tuple.file) {
			return tuple.diagnostic
		}
	}

	panic("unexpected mutated file")
}
