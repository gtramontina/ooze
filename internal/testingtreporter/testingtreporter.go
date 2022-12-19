package testingtreporter

import (
	"github.com/gtramontina/ooze/internal/result"
)

type TestingT interface {
	Helper()
	Logf(format string, args ...any)
}

type TestingTReporter struct {
	t           TestingT
	diagnostics []result.Result[string]
}

func New(t TestingT) *TestingTReporter {
	return &TestingTReporter{
		t:           t,
		diagnostics: []result.Result[string]{},
	}
}

func (r *TestingTReporter) AddDiagnostic(diagnostic result.Result[string]) {
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

	var score float32 = -1
	if total > 0 {
		score = float32(killed) / float32(total)
	}

	r.t.Logf("********************************************************************************")
	r.t.Logf("• Total: %8d", total)
	r.t.Logf("• Killed: %7d", killed)
	r.t.Logf("• Survived: %5d", survived)
	r.t.Logf("• Score: %8.2f", score)
	r.t.Logf("********************************************************************************")
}
