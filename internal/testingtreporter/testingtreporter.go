package testingtreporter

import (
	"github.com/fatih/color"
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

	bold := color.New(color.Bold).SprintFunc()
	scoreExit := func() {}
	scoreColor := color.New(color.Bold, color.FgGreen).SprintFunc()
	score := r.calculator(total, killed)

	if score < r.minimumThreshold {
		scoreExit = r.t.FailNow
		scoreColor = color.New(color.Bold, color.FgRed).SprintFunc()
	}

	r.logger.Logf("********************************************************************************")
	r.logger.Logf("• "+bold("Total")+": %8d", total)
	r.logger.Logf("• "+bold("Killed")+": %7d", killed)
	r.logger.Logf("• "+bold("Survived")+": %5d", survived)
	r.logger.Logf("• "+scoreColor("Score: %8.2f (minimum threshold: %.2f)"), score, r.minimumThreshold)
	r.logger.Logf("********************************************************************************")

	scoreExit()
}
