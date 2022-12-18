package ooze

import (
	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/viruses"
)

type Logger interface {
	Logf(message string, args ...any)
}

type Repository interface {
	ListGoSourceFiles() []*gosourcefile.GoSourceFile
	LinkAllToTemporaryRepository(temporaryPath string) TemporaryRepository
}

type TemporaryRepository interface {
	Root() string
	Overwrite(filePath string, data []byte)
	Remove()
}

type Laboratory interface {
	Test(repository Repository, file *gomutatedfile.GoMutatedFile) result.Result[string]
}

type Reporter interface {
	AddDiagnostic(diagnostic result.Result[string])
	Summarize()
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

func (o *Ooze) Release(viri ...viruses.Virus) result.Result[string] {
	sources := o.repository.ListGoSourceFiles()

	var incubated []*goinfectedfile.GoInfectedFile

	for _, source := range sources {
		for _, virus := range viri {
			incubated = append(incubated, source.Incubate(virus)...)
		}
	}

	if len(incubated) == 0 {
		return result.Err[string]("no mutations applied")
	}

	diagnostic := result.Ok("")
	for _, infectedFile := range incubated {
		diagnostic = diagnostic.And(o.laboratory.Test(o.repository, infectedFile.Mutate()))
	}

	return diagnostic
}
