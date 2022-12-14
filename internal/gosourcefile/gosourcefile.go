package gosourcefile

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/viruses"
)

type GoSourceFile struct {
	relativePath string
	rawContent   []byte
}

func New(relativePath string, rawContent []byte) *GoSourceFile {
	return &GoSourceFile{
		relativePath: relativePath,
		rawContent:   rawContent,
	}
}

func (f *GoSourceFile) Incubate(virus viruses.Virus) []*goinfectedfile.GoInfectedFile {
	fileSet := token.NewFileSet()

	fileTree, err := parser.ParseFile(fileSet, f.relativePath, f.rawContent, parser.ParseComments|parser.AllErrors)
	if err != nil {
		panic(fmt.Errorf("failed parsing file '%s': %w", f.relativePath, err))
	}

	var infectedFiles []*goinfectedfile.GoInfectedFile

	ast.Inspect(fileTree, func(node ast.Node) bool {
		for _, infection := range virus.Incubate(node) {
			infectedFiles = append(infectedFiles, goinfectedfile.New(f.relativePath, infection, fileSet, fileTree))
		}

		return true
	})

	return infectedFiles
}

func (f *GoSourceFile) String() string {
	return f.relativePath
}
