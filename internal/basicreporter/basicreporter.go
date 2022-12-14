package basicreporter

import (
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type BasicReporter struct {
	diagnostics []result.Result[string]
}

func New() *BasicReporter {
	return &BasicReporter{
		diagnostics: []result.Result[string]{},
	}
}

func (r *BasicReporter) AddDiagnostic(diagnostic result.Result[string]) {
	r.diagnostics = append(r.diagnostics, diagnostic)
}

func (r *BasicReporter) Summarize() *ooze.ReportSummary {
	total := len(r.diagnostics)
	survived := 0
	killed := 0

	for _, diagnostic := range r.diagnostics {
		if diagnostic.IsOk() {
			killed++
		} else {
			survived++
		}
	}

	var score float32 = -1
	if total > 0 {
		score = float32(killed) / float32(total)
	}

	return &ooze.ReportSummary{
		Total:    total,
		Survived: survived,
		Killed:   killed,
		Score:    score,
	}
}
