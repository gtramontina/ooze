package ignoredrepository

import (
	"regexp"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FilteredRepository struct {
	patterns []*regexp.Regexp
	delegate ooze.Repository
}

func New(patterns []*regexp.Regexp, delegate ooze.Repository) *FilteredRepository {
	return &FilteredRepository{
		patterns: patterns,
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

func (r *FilteredRepository) LinkAllToTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	return r.delegate.LinkAllToTemporaryRepository(temporaryPath)
}
