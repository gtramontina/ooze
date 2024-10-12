package goinfectedfile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
)

type Infection interface {
	fmt.Stringer
	Infect(fn func())
}

type GoInfectedFile struct {
	relativePath     string
	rawSourceContent []byte
	infection        Infection

	fileSet  *token.FileSet
	fileTree *ast.File
}

func New(
	relativePath string,
	rawSourceContent []byte,
	infection Infection,
	fileSet *token.FileSet,
	fileTree *ast.File,
) *GoInfectedFile {
	return &GoInfectedFile{
		relativePath:     relativePath,
		rawSourceContent: rawSourceContent,
		infection:        infection,
		fileSet:          fileSet,
		fileTree:         fileTree,
	}
}

func (f *GoInfectedFile) Mutate() *gomutatedfile.GoMutatedFile {
	mutatedSource := &bytes.Buffer{}

	f.infection.Infect(func() {
		err := format.Node(mutatedSource, f.fileSet, f.fileTree)
		if err != nil {
			panic(fmt.Errorf("failed formating mutated source: %w", err))
		}
	})

	return gomutatedfile.New(f.infection.String(), f.relativePath, f.rawSourceContent, mutatedSource.Bytes())
}
