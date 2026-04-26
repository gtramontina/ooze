package gosourcefile

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/viruses"
)

func Parse(relativePath string, rawContent []byte) *GoSourceFile {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, relativePath, rawContent, parser.ParseComments|parser.AllErrors)
	if err != nil {
		panic(err)
	}

	info := &types.Info{ //nolint:exhaustruct
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
		Defs:  make(map[*ast.Ident]types.Object),
	}

	conf := types.Config{Importer: importer.Default()} //nolint:exhaustruct
	_, _ = conf.Check("source", fset, []*ast.File{file}, info)

	return New(relativePath, rawContent, fset, file, info)
}

type GoSourceFile struct {
	relativePath string
	rawContent   []byte
	fileSet      *token.FileSet
	fileTree     *ast.File
	typeInfo     *types.Info
}

func New(
	relativePath string,
	rawContent []byte,
	fileSet *token.FileSet,
	fileTree *ast.File,
	typeInfo *types.Info,
) *GoSourceFile {
	return &GoSourceFile{
		relativePath: relativePath,
		rawContent:   rawContent,
		fileSet:      fileSet,
		fileTree:     fileTree,
		typeInfo:     typeInfo,
	}
}

func (f *GoSourceFile) Incubate(virus viruses.Virus) []*goinfectedfile.GoInfectedFile {
	var infectedFiles []*goinfectedfile.GoInfectedFile

	ast.Inspect(f.fileTree, func(node ast.Node) bool {
		for _, infection := range virus.Incubate(node, f.typeInfo) {
			infectedFiles = append(infectedFiles,
				goinfectedfile.New(f.relativePath, f.rawContent, infection, f.fileSet, f.fileTree))
		}

		return true
	})

	return infectedFiles
}

func (f *GoSourceFile) String() string {
	return f.relativePath
}

func (f *GoSourceFile) Equal(other *GoSourceFile) bool {
	if f == nil && other == nil {
		return true
	}

	if f == nil || other == nil {
		return false
	}

	return f.relativePath == other.relativePath &&
		string(f.rawContent) == string(other.rawContent)
}

func EqualSlice(left, right []*GoSourceFile) bool {
	if len(left) != len(right) {
		return false
	}

	for i := range left {
		if !left[i].Equal(right[i]) {
			return false
		}
	}

	return true
}
