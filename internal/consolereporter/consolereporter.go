package consolereporter

import (
	"strings"

	"github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type ConsoleReporter struct {
	logger           ooze.Logger
	differ           gomutatedfile.Differ
	calculator       ooze.ScoreCalculator
	minimumThreshold float32
	diagnostics      []*ooze.Diagnostic
}

func New(
	logger ooze.Logger,
	differ gomutatedfile.Differ,
	calculator ooze.ScoreCalculator,
	minimumThreshold float32,
) *ConsoleReporter {
	return &ConsoleReporter{
		logger:           logger,
		differ:           differ,
		calculator:       calculator,
		minimumThreshold: minimumThreshold,
		diagnostics:      []*ooze.Diagnostic{},
	}
}

func (r *ConsoleReporter) AddDiagnostic(diagnostic *ooze.Diagnostic) {
	r.diagnostics = append(r.diagnostics, diagnostic)
}

func (r *ConsoleReporter) Summarize() result.Result[any] {
	total := len(r.diagnostics)

	var killed, survived int

	for _, diagnostic := range r.diagnostics {
		if diagnostic.IsOk() {
			killed++
		} else {
			survived++
			r.logDiff(diagnostic)
		}
	}

	res := result.Ok[any](nil)
	scoreColor := color.BoldGreen
	scoreIcon := "✓"
	score := r.calculator(total, killed)

	if score < r.minimumThreshold {
		res = result.Err[any]("")
		scoreColor = color.BoldRed
		scoreIcon = "⨯"
	}

	r.logger.Logf("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓")
	r.logger.Logf("┃ • "+color.Bold("Total")+": %8d                    ┃", total)
	r.logger.Logf("┃ • "+color.Bold("Killed")+": %7d                    ┃", killed)
	r.logger.Logf("┃ • "+color.Bold("Survived")+": %5d                    ┃", survived)
	r.logger.Logf("┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨")
	r.logger.Logf("┃ " + scoreColor("%s Score: %8.2f (minimum: %.2f)", scoreIcon, score, r.minimumThreshold) + "    ┃")
	r.logger.Logf("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛")

	return res
}

func (r *ConsoleReporter) logDiff(diagnostic *ooze.Diagnostic) {
	r.logger.Logf("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅")
	r.logger.Logf("┃ 🧬 "+color.BoldRed("Mutant survived:")+" %s", diagnostic.Label())
	r.logger.Logf("┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")

	lines := strings.Split(diagnostic.Diff(r.differ), "\n")
	diff := make([]string, 0, len(lines))
	for _, line := range lines {
		diff = append(diff, "┃ "+line)
	}

	r.logger.Logf(strings.Join(diff, "\n"))
	r.logger.Logf("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅")
}
