package ooze_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	managed "github.com/gtramontina/ooze/internal/ooze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedReportProjectsCompletedCatalogueInStableOrder(t *testing.T) {
	result := managed.ManagedReleaseResult{
		Outcome: managed.ManagedCompleted,
		Mutations: []managed.ManagedMutationResult{
			{
				File: gomutatedfile.New(
					"Integer Increment", "survivor.go", []byte("package fixture\nvar n = 0\n"),
					[]byte("package fixture\nvar n = 1\n"),
				),
				Outcome: managed.ManagedSurvived,
			},
			{
				File: gomutatedfile.New(
					"Integer Increment", "killed.go", []byte("package fixture\nvar n = 0\n"),
					[]byte("package fixture\nvar n = 1\n"),
				),
				Outcome: managed.ManagedKilled,
			},
		},
	}

	report := managed.ProjectManagedReport(result, 0.5, false, false)

	assert.Equal(t, managed.ManagedReportPass, report.Disposition, "disposition = %v, want pass at an equal float32 threshold", report.Disposition)
	wantFragments := []string{
		"Mutant survived: survivor.go → Integer Increment",
		"--- survivor.go (original)",
		"+++ survivor.go (mutated with 'Integer Increment')",
		"• Total:                        2",
		"• Detected:                     1",
		"├ killed:                     1",
		"├ timed out:                  0",
		"└ runaway:                    0",
		"• Survived:                     1",
		"✓ Score:     0.50 (minimum: 0.50)",
	}
	last := -1
	for _, fragment := range wantFragments {
		at := strings.Index(report.Text, fragment)
		assert.False(t, at < 0, "report missing %q:\n%s", fragment, report.Text)
		assert.False(t, at < last, "report fragment %q is out of order:\n%s", fragment, report.Text)
		last = at
	}
	assert.NotContains(t, report.Text, "Mutant killed: killed.go", "ordinary killed mutant was rendered:\n%s", report.Text)
}

func TestManagedReportExplainsTripsConfirmationAndAutomaticFallback(t *testing.T) {
	mutation := func(name string, outcome managed.ManagedMutationOutcome) managed.ManagedMutationResult {
		return managed.ManagedMutationResult{
			File:    gomutatedfile.New("Loop Condition", name, []byte("package fixture\n"), []byte("package fixture\n")),
			Outcome: outcome,
		}
	}
	timedOut := mutation("timeout.go", managed.ManagedTimedOut)
	timedOut.Primary = managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptDeadline, CommandDuration: 21 * time.Second,
		Deadline: 20 * time.Second, Count: managed.ObservedCount{Value: 7, Present: true},
	}
	runaway := mutation("runaway.go", managed.ManagedRunaway)
	runaway.Primary = managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptFuse, CommandDuration: 3 * time.Second,
		Count: managed.ObservedCount{Value: 65, Present: true},
	}
	confirmedKilled := mutation("confirmed.go", managed.ManagedKilled)
	confirmedKilled.Primary = managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptDeadline, CommandDuration: 20 * time.Second,
		Deadline: 20 * time.Second, ConfirmationProvisional: true,
	}
	confirmedKilled.Confirmation = &managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptSettled, CommandDuration: 2 * time.Second,
	}

	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome:                 managed.ManagedCompleted,
		Mutations:               []managed.ManagedMutationResult{timedOut, runaway, confirmedKilled},
		SingleAdmissionFallback: true,
	}, 1, false, false)

	for _, fragment := range []string{
		"Timed out: timeout.go → Loop Condition",
		"observed running peak: 7",
		"Runaway:   runaway.go → Loop Condition",
		"65 live descendants crossed the process fuse",
		"Killed:    confirmed.go → Loop Condition",
		"primary timed out at 20s with peer overlap; exclusive confirmation failed in 2s",
		"Ooze fell back to single-admission automatic after validated capacity pressure.",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}

	serial := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome:   managed.ManagedCompleted,
		Mutations: []managed.ManagedMutationResult{mutation("killed.go", managed.ManagedKilled)},
	}, 1, true, false)
	assert.NotContains(t, serial.Text, "runaway:", "serial summary fabricated runaway evidence:\n%s", serial.Text)
	assert.Contains(t, serial.Text, "└ timed out:", "serial summary fabricated runaway evidence:\n%s", serial.Text)
}

func TestManagedReportFailsNoMutantsWithoutPublishingScore(t *testing.T) {
	report := managed.ProjectManagedReport(
		managed.ManagedReleaseResult{Outcome: managed.ManagedNoMutants},
		1, false, false,
	)

	require.Equal(t, managed.ManagedReportError, report.Disposition, "disposition = %v, want testing error", report.Disposition)
	for _, fragment := range []string{
		"No mutants were discovered. Nothing to score.",
		"WithRepositoryRoot",
		"IgnoreSourceFiles",
		"WithViruses",
		"build constraints",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, "Score:", "NoMutants published a score:\n%s", report.Text)
}

func TestManagedReportPrintsFullFailedBaselineOutputWithoutScore(t *testing.T) {
	baseline := managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptSettled,
		Output: managed.OutputSnapshot{
			Bytes:  "--- FAIL: TestThing (0.02s)\n    thing_test.go:41: want 3, got 4\nFAIL\n",
			Cutoff: 77, CompleteThroughCutoff: true, Final: true,
		},
	}
	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedAborted, Cause: managed.ManagedAbortBaselineFailed, Total: 3,
		Baseline: &baseline,
	}, 0.5, false, false)

	require.Equal(t, managed.ManagedReportError, report.Disposition, "disposition = %v, want testing error", report.Disposition)
	for _, fragment := range []string{
		"Campaign aborted. No mutation score.",
		"Cause: the unmutated baseline failed.",
		"0 of 3 mutants were evaluated.",
		"--- FAIL: TestThing (0.02s)",
		"thing_test.go:41: want 3, got 4",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, "Score:", "abort published a score:\n%s", report.Text)
}

func TestManagedReportDistinguishesBaselineInfrastructureAndSanitizesDiagnostics(t *testing.T) {
	private := "pid=777 path=/private/repository drain-by=42s token=secret"
	baseline := managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptInfrastructure, Deadline: 10 * time.Minute,
		LaunchDuration: time.Second, CommandDuration: 2 * time.Second,
		BoundFired: managed.CommandDeadlineFired,
		Output: managed.OutputSnapshot{
			Bytes: "captured baseline output\n", Cutoff: 25, CompleteThroughCutoff: true, Final: true,
		},
		Failures: managed.FailureDiagnostics{
			Wait: private, RunningCensus: private, Termination: private,
			DrainCensus: private, Output: private, Release: private,
		},
	}
	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedAborted, Cause: managed.ManagedAbortBaselineInfrastructure,
		Total: 3, Baseline: &baseline,
	}, 0, false, false)

	for _, fragment := range []string{
		"Cause: baseline supervision ended with infrastructure uncertainty",
		"deadline: 10m0s; launch: 1s; command: 2s; bound fired: command deadline",
		"output prefix: cutoff=25 complete-through-cutoff=true final=true",
		"wait: failure recorded", "running census: failure recorded",
		"termination: failure recorded", "drain census: failure recorded",
		"output: failure recorded", "release: failure recorded",
		"captured baseline output",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, private, "report leaked a private diagnostic payload:\n%s", report.Text)
}

func TestManagedReportRetainsOrderedPartialDiagnosticsForMidCampaignAbort(t *testing.T) {
	survived := managed.ManagedMutationResult{
		File: gomutatedfile.New(
			"Integer Increment", "first.go", []byte("package fixture\nvar n = 0\n"),
			[]byte("package fixture\nvar n = 1\n"),
		),
		Outcome: managed.ManagedSurvived,
		Primary: managed.ManagedAttemptEvidence{Kind: managed.ManagedAttemptSettled, Passed: true},
	}
	uncertain := managed.ManagedMutationResult{
		File: gomutatedfile.New("Loop Condition", "second.go", []byte("package fixture\n"), []byte("package fixture\n")),
		Primary: managed.ManagedAttemptEvidence{
			Kind: managed.ManagedAttemptInfrastructure, Deadline: 20 * time.Second,
			LaunchDuration: time.Second, CommandDuration: 3 * time.Second,
			Failures: managed.FailureDiagnostics{Wait: "wait failed", Output: "output partial"},
			Output:   managed.OutputSnapshot{Bytes: "partial mutant output", Cutoff: 21, CompleteThroughCutoff: true},
		},
	}
	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedAborted, Cause: managed.ManagedAbortPrimaryInfrastructure, Total: 3,
		Mutations:       []managed.ManagedMutationResult{survived, uncertain},
		ArtifactResidue: []string{"/tmp/ooze-residue"},
	}, 0.5, false, false)

	for _, fragment := range []string{
		"Mutant survived: first.go → Integer Increment",
		"Infrastructure uncertainty: second.go → Loop Condition",
		"wait: failure recorded",
		"output: failure recorded",
		"Cause: primary infrastructure uncertainty",
		"Evaluated 1 of 3 mutants: 0 detected, 1 survived.",
		"Those results are real, but 2 mutants were not attributed, so no score can be computed.",
		"Artifact residue — remove manually:",
		"/tmp/ooze-residue",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, "Score:", "abort published a score or rendered retained mutant output:\n%s", report.Text)
	assert.NotContains(t, report.Text, "partial mutant output", "abort published a score or rendered retained mutant output:\n%s", report.Text)
}

func TestManagedReportRetainsConfirmationFailureWhenCampaignAborts(t *testing.T) {
	confirmation := managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptInfrastructure, Deadline: 20 * time.Second,
		CommandDuration: 4 * time.Second, BoundFired: managed.NoBoundFired,
		Failures: managed.FailureDiagnostics{Wait: "confirmation wait failed", Output: "confirmation output partial"},
	}
	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedAborted, Cause: managed.ManagedAbortConfirmationInfrastructure, Total: 1,
		Mutations: []managed.ManagedMutationResult{{
			File: gomutatedfile.New("Loop Condition", "confirmed.go", []byte("package fixture\n"), []byte("package fixture\n")),
			Primary: managed.ManagedAttemptEvidence{
				Kind: managed.ManagedAttemptDeadline, Deadline: 20 * time.Second,
				CommandDuration: 20 * time.Second, ConfirmationProvisional: true,
			},
			Confirmation: &confirmation,
		}},
	}, 0, false, false)

	for _, fragment := range []string{
		"Confirmation infrastructure uncertainty: confirmed.go → Loop Condition",
		"primary timed out at 20s with peer overlap",
		"confirmation wait: failure recorded",
		"confirmation output: failure recorded",
		"bound fired: none",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, "exclusive confirmation failed", "confirmation infrastructure was mislabeled as a command failure:\n%s", report.Text)
}

func TestManagedReportUsesAuthoritativeConfirmationPeakAndReportsAbortFallback(t *testing.T) {
	confirmation := managed.ManagedAttemptEvidence{
		Kind: managed.ManagedAttemptDeadline, CommandDuration: 20 * time.Second,
		Count: managed.ObservedCount{Value: 11, Present: true},
	}
	confirmed := managed.ManagedMutationResult{
		File:    gomutatedfile.New("Loop Condition", "timeout.go", []byte("package fixture\n"), []byte("package fixture\n")),
		Outcome: managed.ManagedTimedOut,
		Primary: managed.ManagedAttemptEvidence{
			Kind: managed.ManagedAttemptDeadline, CommandDuration: 20 * time.Second,
			Count: managed.ObservedCount{Value: 7, Present: true}, ConfirmationProvisional: true,
		},
		Confirmation: &confirmation,
	}
	completed := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedCompleted, Mutations: []managed.ManagedMutationResult{confirmed},
	}, 0, false, false)
	assert.Contains(t, completed.Text, "observed running peak: 11", "report did not use authoritative confirmation peak:\n%s", completed.Text)
	assert.NotContains(t, completed.Text, "observed running peak: 7", "report did not use authoritative confirmation peak:\n%s", completed.Text)

	aborted := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedAborted, Cause: managed.ManagedAbortPrimaryInfrastructure, Total: 1,
		SingleAdmissionFallback: true,
	}, 0, false, false)
	assert.Contains(t, aborted.Text, "Ooze fell back to single-admission automatic", "aborted report hid the process fallback:\n%s", aborted.Text)
}

func TestManagedReportConsolidatesCleanupResidualsBeforeOnePanic(t *testing.T) {
	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedCleanupUnconfirmed,
		FatalAttempts: []managed.ManagedFatalAttemptEvidence{{
			Attempt: "campaign-1:3",
			Evidence: managed.ManagedAttemptEvidence{
				Kind:     managed.ManagedAttemptDrainUnconfirmed,
				Output:   managed.OutputSnapshot{Bytes: "private output", Cutoff: 14, CompleteThroughCutoff: true},
				Failures: managed.FailureDiagnostics{DrainCensus: "census unavailable", Termination: "kill failed"},
			},
		}},
		Residual: []managed.ManagedResidualCustody{
			{Attempt: "campaign-1:2", Generation: 7, Stage: managed.ManagedResidualProspective},
			{Attempt: "campaign-1:3", Generation: 9, Stage: managed.ManagedResidualOwned, Transferred: true},
		},
	}, 0, false, false)

	assert.Equal(t, managed.ManagedReportPanic, report.Disposition, "fatal disposition = %#v, want exactly one cleanup panic", report)
	assert.EqualValues(t, "ooze: cleanup unconfirmed", report.PanicValue, "fatal disposition = %#v, want exactly one cleanup panic", report)
	first := strings.Index(report.Text, "prospective attempt campaign-1:2")
	second := strings.Index(report.Text, "owned attempt campaign-1:3 (custody transferred)")
	assert.False(t, first < 0, "residual custody is absent or reordered:\n%s", report.Text)
	assert.False(t, second <= first, "residual custody is absent or reordered:\n%s", report.Text)
	for _, fragment := range []string{
		"Containment fault. Ooze cannot prove every test process has exited.",
		"The process runtime is closed for the remainder of this process.",
		"2 execution-domain obligations remain unresolved:",
		"Attempt diagnostic: campaign-1:3",
		"output prefix: cutoff=14 complete-through-cutoff=true final=false",
		"termination: failure recorded",
		"drain census: failure recorded",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, "Score:", "fatal report embedded a score or second panic presentation:\n%s", report.Text)
	assert.NotContains(t, report.Text, "panic:", "fatal report embedded a score or second panic presentation:\n%s", report.Text)
	assert.NotContains(t, report.Text, "private output", "fatal report embedded a score or second panic presentation:\n%s", report.Text)
	assert.NotContains(t, report.Text, "generation", "fatal report embedded a score or second panic presentation:\n%s", report.Text)
	for _, line := range strings.Split(report.Text, "\n") {
		assert.False(t, !strings.HasPrefix(line, "┃") && !strings.HasPrefix(line, "┏") &&
			!strings.HasPrefix(line, "┗"), "fatal report line escaped its box: %q\n%s", line, report.Text)
	}
}

func TestManagedReportLetsInvariantViolationDominateBeforeOnePanic(t *testing.T) {
	report := managed.ProjectManagedReport(managed.ManagedReleaseResult{
		Outcome: managed.ManagedInvariantViolation,
		Invariant: &managed.ManagedInvariantEvidence{
			Operation: "campaign advance", Reason: "terminal event is stale", Phase: "Confirming",
			RejectedEvent:    "attempt terminal/attempt=campaign-1:3",
			StableIdentities: []string{"campaign-1", "campaign-1:3"},
			Obligations:      []string{"admission/campaign-1:3", "execution-domain/campaign-1:3"},
			TraceTail:        []string{"event 4", "event 5"},
		},
		Residual: []managed.ManagedResidualCustody{{
			Attempt: "campaign-1:3", Generation: 9, Stage: managed.ManagedResidualOwned,
		}},
	}, 0, false, false)

	assert.Equal(t, managed.ManagedReportPanic, report.Disposition, "fatal disposition = %#v, want exactly one invariant panic", report)
	assert.EqualValues(t, "ooze: invariant violation", report.PanicValue, "fatal disposition = %#v, want exactly one invariant panic", report)
	for _, fragment := range []string{
		"Internal invariant violated. This campaign has no score.",
		"Operation: campaign advance",
		"Reason: terminal event is stale",
		"Phase: Confirming",
		"Rejected event: attempt terminal/attempt=campaign-1:3",
		"Stable identities: campaign-1, campaign-1:3",
		"Obligations: admission/campaign-1:3, execution-domain/campaign-1:3",
		"Trace tail:",
		"event 4",
		"1 unresolved execution-domain obligation joined this fatal epoch.",
		"owned attempt campaign-1:3",
	} {
		assert.Contains(t, report.Text, fragment, "report missing %q:\n%s", fragment, report.Text)
	}
	assert.NotContains(t, report.Text, "Containment fault", "invariant did not dominate fatal presentation:\n%s", report.Text)
	assert.NotContains(t, report.Text, "Score:", "invariant did not dominate fatal presentation:\n%s", report.Text)
}
