package verbosereporter

import (
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type VerboseReporter struct {
	logger   ooze.Logger
	delegate ooze.Reporter
}

func New(logger ooze.Logger, delegate ooze.Reporter) *VerboseReporter {
	return &VerboseReporter{
		logger:   logger,
		delegate: delegate,
	}
}

func (r *VerboseReporter) AddDiagnostic(diagnostic result.Result[string]) {
	r.logger.Logf("registering diagnostic…")
	r.delegate.AddDiagnostic(diagnostic)
}

func (r *VerboseReporter) Summarize() {
	r.logger.Logf("summarizing report…")
	r.delegate.Summarize()
}
