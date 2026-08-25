package ignoredrepository

import (
	"regexp"
	"slices"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FilteredRepository struct {
	patterns []*regexp.Regexp
	delegate ooze.Repository
}

type filteredTemporaryRepository struct {
	patterns []*regexp.Regexp
	delegate ooze.TemporaryRepository
}

func New(patterns []*regexp.Regexp, delegate ooze.Repository) *FilteredRepository {
	return &FilteredRepository{
		patterns: slices.Clone(patterns),
		delegate: delegate,
	}
}

func (r *FilteredRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	filtered := []*gosourcefile.GoSourceFile{}

FILE_LOOP:
	for _, file := range r.delegate.ListGoSourceFiles() {
		for _, pattern := range r.patterns {
			if pattern.MatchString(file.String()) {
				continue FILE_LOOP
			}
		}
		filtered = append(filtered, file)
	}

	return filtered
}

func (r *FilteredRepository) MaterializeTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	return &filteredTemporaryRepository{
		patterns: slices.Clone(r.patterns),
		delegate: r.delegate.MaterializeTemporaryRepository(temporaryPath),
	}
}

func (r *filteredTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return New(r.patterns, r.delegate).ListGoSourceFiles()
}

func (r *filteredTemporaryRepository) MaterializeTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	return New(r.patterns, r.delegate).MaterializeTemporaryRepository(temporaryPath)
}

func (r *filteredTemporaryRepository) Root() string { return r.delegate.Root() }

func (r *filteredTemporaryRepository) Overwrite(filePath string, data []byte) {
	r.delegate.Overwrite(filePath, data)
}

func (r *filteredTemporaryRepository) Remove() { r.delegate.Remove() }
