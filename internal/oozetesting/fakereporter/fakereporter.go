package fakereporter

import (
	"github.com/gtramontina/ooze/internal/ooze"
)

type FakeReporter struct {
	diagnostics []*ooze.Diagnostic
	summary     *Summary
}

func New() *FakeReporter {
	return &FakeReporter{
		diagnostics: []*ooze.Diagnostic{},
		summary:     nil,
	}
}

func (r *FakeReporter) AddDiagnostic(diagnostic *ooze.Diagnostic) {
	r.diagnostics = append(r.diagnostics, diagnostic)
}

func (r *FakeReporter) Summarize() {
	survived := 0
	killed := 0

	for _, diagnostic := range r.diagnostics {
		if diagnostic.IsOk() {
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
