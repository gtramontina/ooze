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

	cfg := types.Config{ //nolint:exhaustruct // default values for missing fields are okay
		Importer: importer.ForCompiler(token.NewFileSet(), "source", nil),
	}

	info := types.Info{ //nolint:exhaustruct // Info.TypeOf needs Defs, Uses, and Types populated, we don't need the rest
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{},
	}

	if _, err := cfg.Check(f.relativePath, fileSet, []*ast.File{fileTree}, &info); err != nil {
		panic(fmt.Errorf("failed type checking for file '%s': %w", f.relativePath, err))
	}

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
