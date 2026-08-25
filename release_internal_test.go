package ooze

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gtramontina/ooze/internal/iologger"
	internalooze "github.com/gtramontina/ooze/internal/ooze"
)

const fatalReportHelper = "OOZE_FATAL_REPORT_HELPER"

func TestPublishManagedFatalReportsOnceBeforeOnePanic(t *testing.T) {
	for _, test := range []struct {
		role, diagnostic, panicLine string
	}{
		{"cleanup", "Containment fault.", "panic: ooze: cleanup unconfirmed"},
		{"invariant", "Internal invariant violated.", "panic: ooze: invariant violation"},
	} {
		t.Run(test.role, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestPublishManagedFatalHelper$")
			command.Env = append(os.Environ(), fatalReportHelper+"="+test.role)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("fatal helper passed:\n%s", output)
			}
			text := string(output)
			diagnosticAt := strings.Index(text, test.diagnostic)
			panicAt := strings.Index(text, test.panicLine)
			if diagnosticAt < 0 || panicAt < 0 || diagnosticAt > panicAt {
				t.Fatalf("diagnostic was not emitted before panic:\n%s", text)
			}
			if count := strings.Count(text, test.panicLine); count != 1 {
				t.Fatalf("panic line count = %d, want exactly one:\n%s", count, text)
			}
		})
	}
}

func TestPublishManagedFatalHelper(t *testing.T) {
	role := os.Getenv(fatalReportHelper)
	if role == "" {
		return
	}
	result := internalooze.ManagedReleaseResult{
		Outcome: internalooze.ManagedCleanupUnconfirmed,
		Residual: []internalooze.ManagedResidualCustody{{
			Attempt: "campaign-1:2", Generation: 7, Stage: internalooze.ManagedResidualOwned,
		}},
	}
	if role == "invariant" {
		result = internalooze.ManagedReleaseResult{
			Outcome: internalooze.ManagedInvariantViolation,
			Invariant: &internalooze.ManagedInvariantEvidence{
				Operation: "campaign advance", Reason: "invalid transition", Phase: "Running",
			},
		}
	}
	publishManagedReport(t, iologger.New(os.Stdout), internalooze.ProjectManagedReport(result, 1, false, false))
}
