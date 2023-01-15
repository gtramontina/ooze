package fakereporter

import (
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
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

func (r *FakeReporter) Summarize() result.Result[any] {
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

	if survived > 0 {
		return result.Err[any]("")
	}

	return result.Ok[any](nil)
}

func (r *FakeReporter) GetSummary() *Summary {
	return r.summary
}
