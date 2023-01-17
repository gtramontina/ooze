package consolereporter

import (
	"strings"

	"github.com/fatih/color"
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

	bold := color.New(color.Bold).SprintFunc()
	res := result.Ok[any](nil)
	scoreColor := color.New(color.Bold, color.FgGreen).SprintfFunc()
	scoreIcon := "✓"
	score := r.calculator(total, killed)

	if score < r.minimumThreshold {
		res = result.Err[any]("")
		scoreColor = color.New(color.Bold, color.FgRed).SprintfFunc()
		scoreIcon = "⨯"
	}

	r.logger.Logf("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓")
	r.logger.Logf("┃ • "+bold("Total")+": %8d                    ┃", total)
	r.logger.Logf("┃ • "+bold("Killed")+": %7d                    ┃", killed)
	r.logger.Logf("┃ • "+bold("Survived")+": %5d                    ┃", survived)
	r.logger.Logf("┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨")
	r.logger.Logf("┃ " + scoreColor("%s Score: %8.2f (minimum: %.2f)", scoreIcon, score, r.minimumThreshold) + "    ┃")
	r.logger.Logf("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛")

	return res
}

func (r *ConsoleReporter) logDiff(diagnostic *ooze.Diagnostic) {
	r.logger.Logf("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅")
	r.logger.Logf("┃ 🧟 "+color.New(color.Bold, color.FgRed).Sprint("Mutant survived:")+" %s", diagnostic.Label())
	r.logger.Logf("┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")

	diff := []string{}
	for _, line := range strings.Split(diagnostic.Diff(r.differ), "\n") {
		diff = append(diff, "┃ "+line)
	}

	r.logger.Logf(strings.Join(diff, "\n"))
	r.logger.Logf("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅")
}