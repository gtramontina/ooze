package gosourcefile

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/viruses"
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

	cfg := types.Config{
		Importer: importer.Default(),
	}

	info := types.Info{
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{},
	}

	_, _ = cfg.Check(f.relativePath, fileSet, []*ast.File{fileTree}, &info)

	var infectedFiles []*goinfectedfile.GoInfectedFile

	ast.Inspect(fileTree, func(node ast.Node) bool {
		for _, infection := range virus.Incubate(node, &info) {
			infectedFiles = append(infectedFiles, goinfectedfile.New(f.relativePath, f.rawContent, infection, fileSet, fileTree))
		}

		return true
	})

	return infectedFiles
}

func (f *GoSourceFile) String() string {
	return f.relativePath
}
