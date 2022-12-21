package verboselaboratory

import (
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type VerboseLaboratory struct {
	logger   ooze.Logger
	delegate ooze.Laboratory
}

func New(logger ooze.Logger, delegate ooze.Laboratory) *VerboseLaboratory {
	return &VerboseLaboratory{
		logger:   logger,
		delegate: delegate,
	}
}

func (l *VerboseLaboratory) Test(
	repository ooze.Repository,
	file *gomutatedfile.GoMutatedFile,
) <-chan result.Result[string] {
	l.logger.Logf("running laboratory tests for '%s'", file)
	output := <-l.delegate.Test(repository, file)
	l.logger.Logf("laboratory result for '%s': %+v", file, output)

	outputChannel := make(chan result.Result[string], 1)
	outputChannel <- output

	return outputChannel
}
