package testingtreporter

import (
	"github.com/gtramontina/ooze/internal/ooze"
)

type TestingT interface {
	Helper()
	Logf(format string, args ...any)
}

type TestingTReporter struct {
	t           TestingT
	diagnostics []*ooze.Diagnostic
	calculator  ooze.ScoreCalculator
}

func New(t TestingT, calculator ooze.ScoreCalculator) *TestingTReporter {
	return &TestingTReporter{
		t:           t,
		diagnostics: []*ooze.Diagnostic{},
		calculator:  calculator,
	}
}

func (r *TestingTReporter) AddDiagnostic(diagnostic *ooze.Diagnostic) {
	r.diagnostics = append(r.diagnostics, diagnostic)
}

func (r *TestingTReporter) Summarize() {
	r.t.Helper()

	total := len(r.diagnostics)

	var killed, survived int

	for _, diagnostic := range r.diagnostics {
		if diagnostic.IsOk() {
			killed++
		} else {
			survived++
		}
	}

	r.t.Logf("********************************************************************************")
	r.t.Logf("• Total: %8d", total)
	r.t.Logf("• Killed: %7d", killed)
	r.t.Logf("• Survived: %5d", survived)
	r.t.Logf("• Score: %8.2f", r.calculator(total, killed))
	r.t.Logf("********************************************************************************")
}
