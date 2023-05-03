package gosourcefile

import (
	"go/ast"
	"go/types"
	"path"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/viruses"
	"golang.org/x/tools/go/packages"
)

type GoSourceFile struct {
	root         string
	relativePath string
	rawContent   []byte
	pkg          *packages.Package
	typeInfo     *types.Info
}

func New(root string, relativePath string, rawContent []byte) *GoSourceFile {
	return &GoSourceFile{ //nolint:exhaustruct // rest will be filled later
		root:         root,
		relativePath: relativePath,
		rawContent:   rawContent,
	}
}

func (f *GoSourceFile) Incubate(virus viruses.Virus) []*goinfectedfile.GoInfectedFile {
	f.loadTypeInfo()

	fullPath := path.Join(f.root, f.relativePath)

	idx := -1

	for i, path := range f.pkg.CompiledGoFiles {
		if path == fullPath {
			idx = i

			break
		}
	}

	if idx < 0 {
		return nil
	}

	fileTree := f.pkg.Syntax[idx]

	var infectedFiles []*goinfectedfile.GoInfectedFile

	ast.Inspect(fileTree, func(node ast.Node) bool {
		for _, infection := range virus.Incubate(node, f.typeInfo) {
			file := goinfectedfile.New(f.relativePath, f.rawContent, infection, f.pkg.Fset, fileTree)
			infectedFiles = append(infectedFiles, file)
		}

		return true
	})

	return infectedFiles
}

func (f *GoSourceFile) loadTypeInfo() {
	if f.typeInfo != nil {
		return
	}

	pkgCfg := &packages.Config{ //nolint:exhaustruct // rest of the defaults are fine
		Dir:  path.Join(f.root, path.Dir(f.relativePath)),
		Mode: packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedCompiledGoFiles,
	}

	pkgs, err := packages.Load(pkgCfg, ".")
	if err != nil {
		panic(err)
	}

	f.pkg = pkgs[0]

	f.typeInfo = pkgs[0].TypesInfo
}

func (f *GoSourceFile) String() string {
	return f.relativePath
}
