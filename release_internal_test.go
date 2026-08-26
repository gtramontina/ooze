package ooze

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/gtramontina/ooze/internal/iologger"
	internalooze "github.com/gtramontina/ooze/internal/ooze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			require.Error(t, err, "fatal helper passed:\n%s", output)
			text := string(output)
			diagnosticAt := strings.Index(text, test.diagnostic)
			panicAt := strings.Index(text, test.panicLine)
			assert.False(t, diagnosticAt < 0, "diagnostic was not emitted before panic:\n%s", text)
			assert.False(t, panicAt < 0, "diagnostic was not emitted before panic:\n%s", text)
			assert.False(t, diagnosticAt > panicAt, "diagnostic was not emitted before panic:\n%s", text)
			{
				count := strings.Count(text, test.panicLine)
				assert.EqualValues(t, 1, count, "panic line count = %d, want exactly one:\n%s", count, text)
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

func TestProjectResultPreservesFatalCampaignEvidence(t *testing.T) {
	t.Run("cleanup remains unconfirmed", func(t *testing.T) {
		result := projectResult(internalooze.ManagedReleaseResult{
			Outcome: internalooze.ManagedCleanupUnconfirmed,
			Residual: []internalooze.ManagedResidualCustody{{
				Attempt: "campaign-1:2", Generation: 7, Stage: internalooze.ManagedResidualOwned, Transferred: true,
			}},
			FatalAttempts: []internalooze.ManagedFatalAttemptEvidence{{
				Attempt: "campaign-1:2",
				Evidence: internalooze.ManagedAttemptEvidence{
					Kind:     internalooze.ManagedAttemptDrainUnconfirmed,
					Failures: internalooze.FailureDiagnostics{Termination: "kill failed"},
				},
			}},
		}, internalooze.ProjectManagedReport(internalooze.ManagedReleaseResult{
			Outcome: internalooze.ManagedCleanupUnconfirmed,
			Residual: []internalooze.ManagedResidualCustody{{
				Attempt: "campaign-1:2", Generation: 7, Stage: internalooze.ManagedResidualOwned, Transferred: true,
			}},
			FatalAttempts: []internalooze.ManagedFatalAttemptEvidence{{
				Attempt: "campaign-1:2",
				Evidence: internalooze.ManagedAttemptEvidence{
					Kind:     internalooze.ManagedAttemptDrainUnconfirmed,
					Failures: internalooze.FailureDiagnostics{Termination: "kill failed"},
				},
			}},
		}, 1, false, false))

		assert.Equal(t, CleanupUnconfirmed, result.Outcome)
		assert.Equal(t, []ResidualCustody{{
			Attempt: "campaign-1:2", Generation: 7, Stage: ResidualOwned, Transferred: true,
		}}, result.Residual)
		require.Len(t, result.FatalAttempts, 1)
		assert.Equal(t, "campaign-1:2", result.FatalAttempts[0].Attempt)
		assert.Equal(t, "kill failed", result.FatalAttempts[0].Evidence.Failures.Termination)
	})

	t.Run("invariant violation retains diagnostic context", func(t *testing.T) {
		result := projectResult(internalooze.ManagedReleaseResult{
			Outcome: internalooze.ManagedInvariantViolation,
			Invariant: &internalooze.ManagedInvariantEvidence{
				Operation: "campaign advance", Reason: "invalid transition", Phase: "Running",
				RejectedEvent: "attempt terminal", StableIdentities: []string{"campaign-1", "attempt-2"},
				Obligations: []string{"execution-domain"}, TraceTail: []string{"event-7"},
			},
		}, internalooze.ProjectManagedReport(internalooze.ManagedReleaseResult{
			Outcome: internalooze.ManagedInvariantViolation,
			Invariant: &internalooze.ManagedInvariantEvidence{
				Operation: "campaign advance", Reason: "invalid transition", Phase: "Running",
				RejectedEvent: "attempt terminal", StableIdentities: []string{"campaign-1", "attempt-2"},
				Obligations: []string{"execution-domain"}, TraceTail: []string{"event-7"},
			},
		}, 1, false, false))

		assert.Equal(t, InvariantViolation, result.Outcome)
		require.NotNil(t, result.Invariant)
		assert.Equal(t, "campaign advance", result.Invariant.Operation)
		assert.Equal(t, []string{"campaign-1", "attempt-2"}, result.Invariant.StableIdentities)
		assert.Equal(t, []string{"execution-domain"}, result.Invariant.Obligations)
		assert.Equal(t, []string{"event-7"}, result.Invariant.TraceTail)
	})
}

func TestProjectResultRejectsUnknownAttemptBound(t *testing.T) {
	assert.PanicsWithValue(t, "managed attempt bound is invalid", func() {
		projectAttempt(internalooze.ManagedAttemptEvidence{
			Kind: internalooze.ManagedAttemptSettled, BoundFired: internalooze.BoundFired(255),
		})
	})
}
