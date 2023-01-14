package ignoredrepository

import (
	"regexp"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type FilteredRepository struct {
	pattern  *regexp.Regexp
	delegate ooze.Repository
}

func New(pattern *regexp.Regexp, delegate ooze.Repository) *FilteredRepository {
	return &FilteredRepository{
		pattern:  pattern,
		delegate: delegate,
	}
}

func (r *FilteredRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	filtered := []*gosourcefile.GoSourceFile{}

	for _, file := range r.delegate.ListGoSourceFiles() {
		if !r.pattern.MatchString(file.String()) {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

func (r *FilteredRepository) LinkAllToTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	return r.delegate.LinkAllToTemporaryRepository(temporaryPath)
}
