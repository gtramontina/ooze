package fakerepository

import "github.com/gtramontina/ooze/internal/gosourcefile"

type FakeRepository struct {
	sourceFiles []*gosourcefile.GoSourceFile
}

func New(sourceFiles ...*gosourcefile.GoSourceFile) *FakeRepository {
	return &FakeRepository{
		sourceFiles: sourceFiles,
	}
}

func (r *FakeRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return r.sourceFiles
}
