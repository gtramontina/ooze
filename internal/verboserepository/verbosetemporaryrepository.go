package verboserepository

import "github.com/gtramontina/ooze/internal/ooze"

type VerboseTemporaryRepository struct {
	logger   ooze.Logger
	delegate ooze.TemporaryRepository
}

func NewVerboseTemporaryRepository(logger ooze.Logger, delegate ooze.TemporaryRepository) *VerboseTemporaryRepository {
	return &VerboseTemporaryRepository{
		logger:   logger,
		delegate: delegate,
	}
}

func (t *VerboseTemporaryRepository) Root() string {
	return t.delegate.Root()
}

func (t *VerboseTemporaryRepository) Overwrite(filePath string, data []byte) {
	t.logger.Logf("overwriting '%s'…", filePath)
	t.delegate.Overwrite(filePath, data)
}

func (t *VerboseTemporaryRepository) Remove() {
	t.logger.Logf("removing '%s'…", t.Root())
	t.delegate.Remove()
}
