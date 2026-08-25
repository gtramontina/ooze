package ooze

import (
	"fmt"
	"strings"

	reportcolor "github.com/gtramontina/ooze/internal/color"
	"github.com/gtramontina/ooze/internal/gotextdiff"
)

const managedReportInnerWidth = 38

type managedReportDisposition uint8

const (
	managedReportPass managedReportDisposition = iota + 1
	managedReportError
	managedReportPanic
)

type managedReportConfiguration struct {
	minimumThreshold float32
	serial           bool
	colors           bool
}

type managedReport struct {
	text          string
	disposition   managedReportDisposition
	panicValue    string
	callerMessage string
}

type ManagedReportDisposition uint8

const (
	ManagedReportPass ManagedReportDisposition = iota + 1
	ManagedReportError
	ManagedReportPanic
)

type ManagedReport struct {
	Text          string
	Disposition   ManagedReportDisposition
	PanicValue    string
	CallerMessage string
}

func ProjectManagedReport(result ManagedReleaseResult, minimumThreshold float32, serial, colors bool) ManagedReport {
	projected := projectManagedReport(result, managedReportConfiguration{
		minimumThreshold: minimumThreshold, serial: serial, colors: colors,
	})
	disposition := ManagedReportPass
	switch projected.disposition {
	case managedReportPass:
	case managedReportError:
		disposition = ManagedReportError
	case managedReportPanic:
		disposition = ManagedReportPanic
	default:
		panic("managed report disposition is invalid")
	}

	return ManagedReport{
		Text: projected.text, Disposition: disposition, PanicValue: projected.panicValue,
		CallerMessage: projected.callerMessage,
	}
}

func projectManagedReport(result ManagedReleaseResult, configuration managedReportConfiguration) managedReport {
	if result.Outcome == ManagedNoMutants {
		return managedReport{
			text: strings.Join([]string{
				"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
				"┃ ⨯ No mutants were discovered. Nothing to score.",
				"┃",
				"┃   Check WithRepositoryRoot, the IgnoreSourceFiles patterns, the WithViruses",
				"┃   set, and whether build constraints exclude every source file.",
				"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
			}, "\n"),
			disposition: managedReportError, callerMessage: "no mutants were discovered; no mutation score",
		}
	}
	if result.Outcome == ManagedAborted {
		return projectManagedAbort(result, configuration.colors)
	}
	if result.Outcome == ManagedCleanupUnconfirmed {
		return projectManagedCleanupUnconfirmed(result)
	}
	if result.Outcome == ManagedInvariantViolation {
		return projectManagedInvariantViolation(result)
	}
	if result.Outcome != ManagedCompleted {
		panic("managed report outcome is not implemented")
	}

	var report strings.Builder
	killed, timedOut, runaway, survived := 0, 0, 0, 0
	differ := gotextdiff.New()
	palette := reportcolor.NewPalette(configuration.colors)
	for _, mutation := range result.Mutations {
		writeManagedMutationDetail(&report, mutation, differ, palette)
		switch mutation.Outcome {
		case ManagedSurvived:
			survived++
		case ManagedKilled:
			killed++
		case ManagedTimedOut:
			timedOut++
		case ManagedRunaway:
			runaway++
		default:
			panic("managed mutation outcome is invalid")
		}
	}

	detected := killed + timedOut + runaway
	total := detected + survived
	score := float32(detected) / float32(total)
	writeManagedSummary(&report, killed, timedOut, runaway, survived, score, configuration)
	if result.SingleAdmissionFallback {
		report.WriteString("Ooze fell back to single-admission automatic after validated capacity pressure.\n")
		report.WriteString("Every later campaign in this process runs one automatic attempt at a time.\n")
	}
	disposition := managedReportPass
	if score < configuration.minimumThreshold {
		disposition = managedReportError
	}

	projected := managedReport{text: strings.TrimSuffix(report.String(), "\n"), disposition: disposition}
	if disposition == managedReportError {
		projected.callerMessage = fmt.Sprintf(
			"mutation score %.2f is below minimum %.2f", score, configuration.minimumThreshold,
		)
	}

	return projected
}

func projectManagedInvariantViolation(result ManagedReleaseResult) managedReport {
	if result.Invariant == nil {
		panic("managed invariant presentation is missing")
	}
	invariant := result.Invariant
	var report strings.Builder
	report.WriteString("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓\n")
	report.WriteString("┃ ☠ Internal invariant violated. No campaign in this process is scored.\n")
	fmt.Fprintf(&report, "┃   Operation: %s\n", invariant.Operation)
	fmt.Fprintf(&report, "┃   Reason: %s\n", invariant.Reason)
	if invariant.Phase != "" {
		fmt.Fprintf(&report, "┃   Phase: %s\n", invariant.Phase)
	}
	if invariant.RejectedEvent != "" {
		fmt.Fprintf(&report, "┃   Rejected event: %s\n", invariant.RejectedEvent)
	}
	if len(invariant.StableIdentities) != 0 {
		fmt.Fprintf(&report, "┃   Stable identities: %s\n", strings.Join(invariant.StableIdentities, ", "))
	}
	if len(invariant.Obligations) != 0 {
		fmt.Fprintf(&report, "┃   Obligations: %s\n", strings.Join(invariant.Obligations, ", "))
	}
	if len(invariant.TraceTail) != 0 {
		report.WriteString("┃   Trace tail:\n")
		for _, record := range invariant.TraceTail {
			fmt.Fprintf(&report, "┃     %s\n", record)
		}
	}
	if len(result.Residual) != 0 {
		noun := "obligations"
		if len(result.Residual) == 1 {
			noun = "obligation"
		}
		fmt.Fprintf(
			&report, "┃   %d unresolved execution-domain %s joined this fatal epoch.\n",
			len(result.Residual), noun,
		)
	}
	report.WriteString("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛")

	return managedReport{
		text: report.String(), disposition: managedReportPanic,
		panicValue: "ooze: invariant violation",
	}
}

func projectManagedCleanupUnconfirmed(result ManagedReleaseResult) managedReport {
	var report strings.Builder
	report.WriteString("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓\n")
	report.WriteString("┃ ☠ Containment fault. Ooze cannot prove every test process has exited.\n")
	report.WriteString("┃\n┃   The process runtime is closed for the remainder of this process.\n")
	for _, attempt := range result.FatalAttempts {
		fmt.Fprintf(&report, "┃   Attempt diagnostic: %s\n", attempt.Attempt)
		writeManagedAttemptDiagnostics(&report, attempt.Evidence)
	}
	fmt.Fprintf(&report, "┃   %d execution-domain obligations remain unresolved:\n", len(result.Residual))
	for _, residual := range result.Residual {
		stage := "prospective"
		if residual.Stage == ManagedResidualOwned {
			stage = "owned"
		}
		transferred := ""
		if residual.Transferred {
			transferred = ", custody transferred"
		}
		fmt.Fprintf(
			&report, "┃     %s attempt %s (generation %d%s)\n",
			stage, residual.Attempt, residual.Generation, transferred,
		)
	}
	report.WriteString("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛")

	return managedReport{
		text: report.String(), disposition: managedReportPanic,
		panicValue: "ooze: cleanup unconfirmed",
	}
}

func projectManagedAbort(result ManagedReleaseResult, colors bool) managedReport {
	var report strings.Builder
	differ := gotextdiff.New()
	palette := reportcolor.NewPalette(colors)
	evaluated, detected, survived := 0, 0, 0
	for _, mutation := range result.Mutations {
		writeManagedMutationDetail(&report, mutation, differ, palette)
		if mutation.Outcome == 0 {
			continue
		}
		evaluated++
		if mutation.Outcome == ManagedSurvived {
			survived++
		} else {
			detected++
		}
	}
	report.WriteString("┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓\n")
	report.WriteString("┃ ⨯ Campaign aborted. No mutation score.\n")
	report.WriteString("┃\n")
	cause := result.Cause
	if cause == "baseline did not pass" {
		cause = "the unmutated baseline failed."
	}
	fmt.Fprintf(&report, "┃   Cause: %s\n", cause)
	if result.Cause == "baseline did not pass" {
		fmt.Fprintf(&report, "┃   Mutation evidence requires a green suite; 0 of %d mutants were evaluated.\n", result.Total)
	} else {
		fmt.Fprintf(&report, "┃   Evaluated %d of %d mutants: %d detected, %d survived.\n", evaluated, result.Total, detected, survived)
		fmt.Fprintf(
			&report,
			"┃   Those results are real, but %d mutants were not attributed, so no score can be computed.\n",
			result.Total-evaluated,
		)
	}
	if result.Baseline != nil && result.Baseline.Output.Bytes != "" {
		report.WriteString("┠━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┨\n")
		for line := range strings.SplitSeq(strings.TrimSuffix(result.Baseline.Output.Bytes, "\n"), "\n") {
			fmt.Fprintf(&report, "┃ %s\n", line)
		}
	}
	if len(result.ArtifactResidue) != 0 {
		report.WriteString("┃\n┃   Artifact residue — remove manually:\n")
		for _, residue := range result.ArtifactResidue {
			fmt.Fprintf(&report, "┃     %s\n", residue)
		}
	}
	report.WriteString("┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛")

	return managedReport{
		text: report.String(), disposition: managedReportError,
		callerMessage: "campaign aborted: " + result.Cause + "; no mutation score",
	}
}

func writeManagedMutationDetail(
	report *strings.Builder,
	mutation ManagedMutationResult,
	differ *gotextdiff.GoTextDiff,
	palette reportcolor.Palette,
) {
	switch mutation.Outcome {
	case 0:
		fmt.Fprintf(report, "Infrastructure uncertainty: %s\n", mutation.File.Label())
		writeManagedAttemptDiagnostics(report, mutation.Primary)
	case ManagedSurvived:
		fmt.Fprintf(
			report, "Mutant survived: %s\n%s\n", mutation.File.Label(),
			managedColoredDiff(strings.TrimSpace(mutation.File.Diff(differ)), palette),
		)
		if mutation.Confirmation != nil {
			fmt.Fprintf(report, "%s\n", managedConfirmationLine(mutation))
		}
	case ManagedKilled:
		if mutation.Confirmation != nil {
			fmt.Fprintf(report, "Killed:    %s\n%s\n", mutation.File.Label(), managedConfirmationLine(mutation))
		}
	case ManagedTimedOut:
		fmt.Fprintf(report, "Timed out: %s\n", mutation.File.Label())
		if mutation.Primary.Count.Present {
			fmt.Fprintf(report, "  observed running peak: %d\n", mutation.Primary.Count.Value)
		}
		if mutation.Confirmation != nil {
			fmt.Fprintf(report, "%s\n", managedConfirmationLine(mutation))
		}
	case ManagedRunaway:
		fmt.Fprintf(report, "Runaway:   %s\n", mutation.File.Label())
		if evidence := managedOutcomeEvidence(mutation); evidence.Count.Present {
			fmt.Fprintf(report, "  %d live descendants crossed the process fuse\n", evidence.Count.Value)
		}
		if mutation.Confirmation != nil {
			fmt.Fprintf(report, "%s\n", managedConfirmationLine(mutation))
		}
	default:
		panic("managed mutation outcome is invalid")
	}
}

func managedColoredDiff(diff string, palette reportcolor.Palette) string {
	lines := strings.Split(diff, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "-"):
			lines[index] = palette.BoldRed(line)
		case strings.HasPrefix(trimmed, "+"):
			lines[index] = palette.Green(line)
		case strings.HasPrefix(trimmed, "@"):
			lines[index] = palette.Blue(line)
		}
	}

	return strings.Join(lines, "\n")
}

func writeManagedAttemptDiagnostics(report *strings.Builder, evidence ManagedAttemptEvidence) {
	fmt.Fprintf(
		report, "  deadline: %s; launch: %s; command: %s; bound fired: %d\n",
		evidence.Deadline, evidence.LaunchDuration, evidence.CommandDuration, evidence.BoundFired,
	)
	fmt.Fprintf(
		report, "  output prefix: cutoff=%d complete-through-cutoff=%t final=%t\n",
		evidence.Output.Cutoff, evidence.Output.CompleteThroughCutoff, evidence.Output.Final,
	)
	for _, diagnostic := range []struct{ name, value string }{
		{"wait", evidence.Failures.Wait},
		{"running census", evidence.Failures.RunningCensus},
		{"termination", evidence.Failures.Termination},
		{"drain census", evidence.Failures.DrainCensus},
		{"output", evidence.Failures.Output},
		{"release", evidence.Failures.Release},
	} {
		if diagnostic.value != "" {
			fmt.Fprintf(report, "  %s: %s\n", diagnostic.name, diagnostic.value)
		}
	}
}

func managedOutcomeEvidence(mutation ManagedMutationResult) ManagedAttemptEvidence {
	if mutation.Confirmation != nil {
		return *mutation.Confirmation
	}

	return mutation.Primary
}

func managedConfirmationLine(mutation ManagedMutationResult) string {
	if mutation.Confirmation == nil {
		return ""
	}
	confirmation := *mutation.Confirmation
	result := "failed"
	switch confirmation.Kind {
	case ManagedAttemptSettled:
		if confirmation.Passed {
			result = "passed"
		}
	case ManagedAttemptDeadline:
		result = "timed out"
	case ManagedAttemptFuse:
		result = "tripped the process fuse"
	}

	return fmt.Sprintf(
		"  primary timed out at %s with peer overlap; exclusive confirmation %s in %s",
		mutation.Primary.CommandDuration, result, confirmation.CommandDuration,
	)
}

func writeManagedSummary(
	report *strings.Builder,
	killed, timedOut, runaway, survived int,
	score float32,
	configuration managedReportConfiguration,
) {
	detected := killed + timedOut + runaway
	total := detected + survived
	fmt.Fprintf(report, "┏%s┓\n", strings.Repeat("━", managedReportInnerWidth))
	writeManagedSummaryRow(report, "• Total", total)
	writeManagedSummaryRow(report, "• Detected", detected)
	writeManagedSummaryRow(report, "  ├ killed", killed)
	if configuration.serial {
		writeManagedSummaryRow(report, "  └ timed out", timedOut)
	} else {
		writeManagedSummaryRow(report, "  ├ timed out", timedOut)
		writeManagedSummaryRow(report, "  └ runaway", runaway)
	}
	writeManagedSummaryRow(report, "• Survived", survived)
	fmt.Fprintf(report, "┠%s┨\n", strings.Repeat("┄", managedReportInnerWidth))
	icon := "✓"
	if score < configuration.minimumThreshold {
		icon = "⨯"
	}
	line := fmt.Sprintf(" %s Score: %8.2f (minimum: %.2f)", icon, score, configuration.minimumThreshold)
	fmt.Fprintf(report, "┃%s%s┃\n", line, strings.Repeat(" ", managedReportInnerWidth-len([]rune(line))))
	fmt.Fprintf(report, "┗%s┛\n", strings.Repeat("━", managedReportInnerWidth))
}

func writeManagedSummaryRow(report *strings.Builder, label string, value int) {
	left := " " + label + ":"
	right := fmt.Sprintf("%d", value)
	padding := managedReportInnerWidth - len([]rune(left)) - len(right) - 4
	fmt.Fprintf(report, "┃%s%s%s    ┃\n", left, strings.Repeat(" ", padding), right)
}
