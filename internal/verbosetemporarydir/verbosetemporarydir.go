package verbosetemporarydir

import (
	"github.com/gtramontina/ooze/internal/ooze"
)

type VerboseTemporaryDir struct {
	logger   ooze.Logger
	delegate ooze.ManagedTemporaryDirectory
}

func New(logger ooze.Logger, delegate ooze.ManagedTemporaryDirectory) *VerboseTemporaryDir {
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
