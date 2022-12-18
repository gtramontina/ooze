package goinfectedfile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/viruses"
)

type GoInfectedFile struct {
	relativePath string
	infection    *viruses.Infection

	fileSet  *token.FileSet
	fileTree *ast.File
}

func New(
	relativePath string,
	infection *viruses.Infection,
	fileSet *token.FileSet,
	fileTree *ast.File,
) *GoInfectedFile {
	return &GoInfectedFile{
		relativePath: relativePath,
		infection:    infection,
		fileSet:      fileSet,
		fileTree:     fileTree,
	}
}

func (f *GoInfectedFile) Mutate() *gomutatedfile.GoMutatedFile {
	mutatedSource := &bytes.Buffer{}

	f.infection.Mutate(func() {
		err := format.Node(mutatedSource, f.fileSet, f.fileTree)
		if err != nil {
			panic(fmt.Errorf("failed formating mutated source: %w", err))
		}
	})

	return gomutatedfile.New(f.relativePath, mutatedSource.Bytes())
}

func (f *GoInfectedFile) String() string {
	return f.relativePath
}

func (f *GoInfectedFile) Label() string {
	return f.relativePath + "~>" + f.infection.String()
}
