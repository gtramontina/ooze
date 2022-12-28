package testingtreporter

import (
	"github.com/gtramontina/ooze/internal/ooze"
)

type TestingT interface {
	Helper()
	Logf(format string, args ...any)
	FailNow()
}

type TestingTReporter struct {
	t                TestingT
	logger           ooze.Logger
	calculator       ooze.ScoreCalculator
	minimumThreshold float32
	diagnostics      []*ooze.Diagnostic
}

func New(t TestingT, logger ooze.Logger, calculator ooze.ScoreCalculator, minimumThreshold float32) *TestingTReporter {
	return &TestingTReporter{
		t:                t,
		logger:           logger,
		calculator:       calculator,
		minimumThreshold: minimumThreshold,
		diagnostics:      []*ooze.Diagnostic{},
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

	score := r.calculator(total, killed)

	r.logger.Logf("********************************************************************************")
	r.logger.Logf("• Total: %8d", total)
	r.logger.Logf("• Killed: %7d", killed)
	r.logger.Logf("• Survived: %5d", survived)
	r.logger.Logf("• Score: %8.2f (minimum threshold: %.2f)", score, r.minimumThreshold)
	r.logger.Logf("********************************************************************************")

	if score < r.minimumThreshold {
		r.t.FailNow()
	}
}
