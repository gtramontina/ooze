package verboserepository

import (
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
)

type VerboseRepository struct {
	logger   ooze.Logger
	delegate ooze.Repository
}

func New(logger ooze.Logger, delegate ooze.Repository) *VerboseRepository {
	return &VerboseRepository{
		logger:   logger,
		delegate: delegate,
	}
}

func (r *VerboseRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	r.logger.Logf("listing go source files…")
	files := r.delegate.ListGoSourceFiles()
	r.logger.Logf("found %d source files: %s", len(files), files)

	return files
}

func (r *VerboseRepository) LinkAllToTemporaryRepository(temporaryPath string) ooze.TemporaryRepository {
	r.logger.Logf("linking all files to temporary path '%s'…", temporaryPath)
	repository := r.delegate.LinkAllToTemporaryRepository(temporaryPath)
	r.logger.Logf("linked all files to temporary path '%s'", temporaryPath)

	return NewVerboseTemporaryRepository(r.logger, repository)
}
