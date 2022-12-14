package verbosetemporarydir

import (
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/ooze"
)

type VerboseTemporaryDir struct {
	logger   ooze.Logger
	delegate laboratory.TemporaryDirectory
}

func New(logger ooze.Logger, delegate laboratory.TemporaryDirectory) *VerboseTemporaryDir {
	return &VerboseTemporaryDir{
		logger:   logger,
		delegate: delegate,
	}
}

func (d *VerboseTemporaryDir) New() string {
	dir := d.delegate.New()
	d.logger.Logf("setting up new temporary directory at '%s'", dir)

	return dir
}
