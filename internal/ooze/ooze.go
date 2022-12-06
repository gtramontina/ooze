package ooze

import (
	"errors"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/viruses"
)

var ErrNoMutationsApplied = errors.New("no mutations applied")

type Repository interface {
	ListGoSourceFiles() []*gosourcefile.GoSourceFile
}

type Laboratory interface {
	Test(mutatedFile *gomutatedfile.GoMutatedFile) result.Result[string]
}

type Ooze struct {
	repository Repository
	laboratory Laboratory
}

func New(repository Repository, laboratory Laboratory) *Ooze {
	return &Ooze{
		repository: repository,
		laboratory: laboratory,
	}
}

func (o *Ooze) Release(viri []viruses.Virus) result.Result[string] {
	if len(viri) == 0 {
		return result.Err[string](ErrNoMutationsApplied)
	}

	sources := o.repository.ListGoSourceFiles()
	if len(sources) == 0 {
		return result.Err[string](ErrNoMutationsApplied)
	}

	incubated := sources[0].Incubate(viri[0])
	if len(incubated) == 0 {
		return result.Err[string](ErrNoMutationsApplied)
	}

	return o.laboratory.Test(incubated[0].Mutate())
}
