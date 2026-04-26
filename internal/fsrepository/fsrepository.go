package fsrepository

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
	"golang.org/x/tools/go/packages"
)

type FSRepository struct {
	root string
}

func New(root string) *FSRepository {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}

	stat, err := os.Stat(absRoot)
	if errors.Is(err, fs.ErrNotExist) {
		panic(root + ": no such directory")
	}

	if err != nil {
		panic(err)
	}

	if !stat.IsDir() {
		panic(root + ": not a directory")
	}

	return &FSRepository{
		root: absRoot,
	}
}

func (r *FSRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	cfg := &packages.Config{ //nolint:exhaustruct
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir: r.root,
	}

	pkgs, err := packages.Load(cfg, "./...")
	if err != nil || len(pkgs) == 0 {
		return nil
	}

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil
		}
	}

	var sourceFiles []*gosourcefile.GoSourceFile
	seen := make(map[string]bool)

	for _, pkg := range pkgs {
		sourceFiles = r.collectPackageFiles(pkg, seen, sourceFiles)
	}

	if len(sourceFiles) == 0 {
		return nil
	}

	sort.Slice(sourceFiles, func(i, j int) bool {
		return sourceFiles[i].String() < sourceFiles[j].String()
	})

	return sourceFiles
}

func (r *FSRepository) collectPackageFiles(
	pkg *packages.Package,
	seen map[string]bool,
	sourceFiles []*gosourcefile.GoSourceFile,
) []*gosourcefile.GoSourceFile {
	for i, file := range pkg.Syntax {
		if i >= len(pkg.CompiledGoFiles) {
			continue
		}

		absPath := pkg.CompiledGoFiles[i]
		if seen[absPath] {
			continue
		}

		seen[absPath] = true

		relativePath, err := filepath.Rel(r.root, absPath)
		if err != nil || strings.HasSuffix(relativePath, "_test.go") {
			continue
		}

		rawContent, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		sourceFiles = append(sourceFiles, gosourcefile.New(
			relativePath, rawContent, pkg.Fset, file, pkg.TypesInfo,
		))
	}

	return sourceFiles
}

func (r *FSRepository) LinkAllToTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	rootSize := len(strings.Split(r.root, string(os.PathSeparator)))

	err := filepath.WalkDir(r.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return err
		}

		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed getting absolute path for '%s': %w", path, err)
		}

		linkPath := filepath.Join(temporaryPath, filepath.Join(strings.Split(path, string(os.PathSeparator))[rootSize:]...))
		err = os.MkdirAll(filepath.Dir(linkPath), os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed creating directory tree for '%s': %w", linkPath, err)
		}

		err = os.Symlink(absolutePath, linkPath)
		if err != nil {
			return fmt.Errorf("failed creating link from '%s' to '%s': %w", path, linkPath, err)
		}

		return nil
	})
	if err != nil {
		panic(fmt.Errorf("failed scanning '%s': %w", r.root, err))
	}

	return NewTemporary(temporaryPath)
}
