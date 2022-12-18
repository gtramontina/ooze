package basicreporter

import (
	"github.com/gtramontina/ooze/internal/result"
)

type BasicReporter struct {
	diagnostics []result.Result[string]
	summary     *Summary
}

func New() *BasicReporter {
	return &BasicReporter{
		diagnostics: []result.Result[string]{},
		summary:     nil,
	}
}

func (r *BasicReporter) AddDiagnostic(diagnostic result.Result[string]) {
	r.diagnostics = append(r.diagnostics, diagnostic)
}

func (r *BasicReporter) Summarize() {
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

	r.summary = &Summary{
		Total:    total,
		Survived: survived,
		Killed:   killed,
		Score:    score,
	}
}

func (r *BasicReporter) GetSummary() *Summary {
	return r.summary
}
