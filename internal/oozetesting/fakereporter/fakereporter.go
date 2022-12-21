package fakereporter

import (
	"github.com/gtramontina/ooze/internal/result"
)

type FakeReporter struct {
	diagnostics []<-chan result.Result[string]
	summary     *Summary
}

func New() *FakeReporter {
	return &FakeReporter{
		diagnostics: []<-chan result.Result[string]{},
		summary:     nil,
	}
}

func (r *FakeReporter) AddDiagnostic(diagnostic <-chan result.Result[string]) {
	r.diagnostics = append(r.diagnostics, diagnostic)
}

func (r *FakeReporter) Summarize() {
	survived := 0
	killed := 0

	for _, diagnostic := range r.diagnostics {
		if (<-diagnostic).IsOk() {
			killed++
		} else {
			survived++
		}
	}

	r.summary = &Summary{
		Survived: survived,
		Killed:   killed,
	}
}

func (r *FakeReporter) GetSummary() *Summary {
	return r.summary
}
