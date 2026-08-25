package ooze

import (
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedReportProjectsCompletedCatalogueInStableOrder(t *testing.T) {
	result := ManagedReleaseResult{
		Outcome: ManagedCompleted,
		Mutations: []ManagedMutationResult{
			{
				File: gomutatedfile.New(
					"Integer Increment", "survivor.go", []byte("package fixture\nvar n = 0\n"),
					[]byte("package fixture\nvar n = 1\n"),
				),
				Outcome: ManagedSurvived,
			},
			{
				File: gomutatedfile.New(
					"Integer Increment", "killed.go", []byte("package fixture\nvar n = 0\n"),
					[]byte("package fixture\nvar n = 1\n"),
				),
				Outcome: ManagedKilled,
			},
		},
	}

	report := projectManagedReport(result, managedReportConfiguration{minimumThreshold: 0.5})

	assert.Equal(t, managedReportPass, report.disposition, "disposition = %v, want pass at an equal float32 threshold", report.disposition)
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
		at := strings.Index(report.text, fragment)
		assert.False(t, at < 0, "report missing %q:\n%s", fragment, report.text)
		assert.False(t, at < last, "report fragment %q is out of order:\n%s", fragment, report.text)
		last = at
	}
	assert.False(t, strings.Contains(report.text, "Mutant killed: killed.go"), "ordinary killed mutant was rendered:\n%s", report.text)
}

func TestManagedReportExplainsTripsConfirmationAndAutomaticFallback(t *testing.T) {
	mutation := func(name string, outcome ManagedMutationOutcome) ManagedMutationResult {
		return ManagedMutationResult{
			File:    gomutatedfile.New("Loop Condition", name, []byte("package fixture\n"), []byte("package fixture\n")),
			Outcome: outcome,
		}
	}
	timedOut := mutation("timeout.go", ManagedTimedOut)
	timedOut.Primary = ManagedAttemptEvidence{
		Kind: ManagedAttemptDeadline, CommandDuration: 21 * time.Second,
		Deadline: 20 * time.Second, Count: ObservedCount{Value: 7, Present: true},
	}
	runaway := mutation("runaway.go", ManagedRunaway)
	runaway.Primary = ManagedAttemptEvidence{
		Kind: ManagedAttemptFuse, CommandDuration: 3 * time.Second,
		Count: ObservedCount{Value: 65, Present: true},
	}
	confirmedKilled := mutation("confirmed.go", ManagedKilled)
	confirmedKilled.Primary = ManagedAttemptEvidence{
		Kind: ManagedAttemptDeadline, CommandDuration: 20 * time.Second,
		Deadline: 20 * time.Second, ConfirmationProvisional: true,
	}
	confirmedKilled.Confirmation = &ManagedAttemptEvidence{
		Kind: ManagedAttemptSettled, CommandDuration: 2 * time.Second,
	}

	report := projectManagedReport(ManagedReleaseResult{
		Outcome:                 ManagedCompleted,
		Mutations:               []ManagedMutationResult{timedOut, runaway, confirmedKilled},
		SingleAdmissionFallback: true,
	}, managedReportConfiguration{minimumThreshold: 1})

	for _, fragment := range []string{
		"Timed out: timeout.go → Loop Condition",
		"observed running peak: 7",
		"Runaway:   runaway.go → Loop Condition",
		"65 live descendants crossed the process fuse",
		"Killed:    confirmed.go → Loop Condition",
		"primary timed out at 20s with peer overlap; exclusive confirmation failed in 2s",
		"Ooze fell back to single-admission automatic after validated capacity pressure.",
	} {
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}

	serial := projectManagedReport(ManagedReleaseResult{
		Outcome:   ManagedCompleted,
		Mutations: []ManagedMutationResult{mutation("killed.go", ManagedKilled)},
	}, managedReportConfiguration{minimumThreshold: 1, serial: true})
	assert.False(t, strings.Contains(serial.text, "runaway:"), "serial summary fabricated runaway evidence:\n%s", serial.text)
	assert.True(t, strings.Contains(serial.text, "└ timed out:"), "serial summary fabricated runaway evidence:\n%s", serial.text)
}

func TestManagedReportFailsNoMutantsWithoutPublishingScore(t *testing.T) {
	report := projectManagedReport(
		ManagedReleaseResult{Outcome: ManagedNoMutants},
		managedReportConfiguration{minimumThreshold: 1},
	)

	require.Equal(t, managedReportError, report.disposition, "disposition = %v, want testing error", report.disposition)
	for _, fragment := range []string{
		"No mutants were discovered. Nothing to score.",
		"WithRepositoryRoot",
		"IgnoreSourceFiles",
		"WithViruses",
		"build constraints",
	} {
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, "Score:"), "NoMutants published a score:\n%s", report.text)
}

func TestManagedReportPrintsFullFailedBaselineOutputWithoutScore(t *testing.T) {
	baseline := ManagedAttemptEvidence{
		Kind: ManagedAttemptSettled,
		Output: OutputSnapshot{
			Bytes:  "--- FAIL: TestThing (0.02s)\n    thing_test.go:41: want 3, got 4\nFAIL\n",
			Cutoff: 77, CompleteThroughCutoff: true, Final: true,
		},
	}
	report := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedAborted, Cause: ManagedAbortBaselineFailed, Total: 3,
		Baseline: &baseline,
	}, managedReportConfiguration{minimumThreshold: 0.5})

	require.Equal(t, managedReportError, report.disposition, "disposition = %v, want testing error", report.disposition)
	for _, fragment := range []string{
		"Campaign aborted. No mutation score.",
		"Cause: the unmutated baseline failed.",
		"0 of 3 mutants were evaluated.",
		"--- FAIL: TestThing (0.02s)",
		"thing_test.go:41: want 3, got 4",
	} {
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, "Score:"), "abort published a score:\n%s", report.text)
}

func TestManagedReportDistinguishesBaselineInfrastructureAndSanitizesDiagnostics(t *testing.T) {
	private := "pid=777 path=/private/repository drain-by=42s token=secret"
	baseline := ManagedAttemptEvidence{
		Kind: ManagedAttemptInfrastructure, Deadline: 10 * time.Minute,
		LaunchDuration: time.Second, CommandDuration: 2 * time.Second,
		BoundFired: CommandDeadlineFired,
		Output: OutputSnapshot{
			Bytes: "captured baseline output\n", Cutoff: 25, CompleteThroughCutoff: true, Final: true,
		},
		Failures: FailureDiagnostics{
			Wait: private, RunningCensus: private, Termination: private,
			DrainCensus: private, Output: private, Release: private,
		},
	}
	report := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedAborted, Cause: ManagedAbortBaselineInfrastructure,
		Total: 3, Baseline: &baseline,
	}, managedReportConfiguration{})

	for _, fragment := range []string{
		"Cause: baseline supervision ended with infrastructure uncertainty",
		"deadline: 10m0s; launch: 1s; command: 2s; bound fired: command deadline",
		"output prefix: cutoff=25 complete-through-cutoff=true final=true",
		"wait: failure recorded", "running census: failure recorded",
		"termination: failure recorded", "drain census: failure recorded",
		"output: failure recorded", "release: failure recorded",
		"captured baseline output",
	} {
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, private), "report leaked a private diagnostic payload:\n%s", report.text)
}

func TestManagedReportRetainsOrderedPartialDiagnosticsForMidCampaignAbort(t *testing.T) {
	survived := ManagedMutationResult{
		File: gomutatedfile.New(
			"Integer Increment", "first.go", []byte("package fixture\nvar n = 0\n"),
			[]byte("package fixture\nvar n = 1\n"),
		),
		Outcome: ManagedSurvived,
		Primary: ManagedAttemptEvidence{Kind: ManagedAttemptSettled, Passed: true},
	}
	uncertain := ManagedMutationResult{
		File: gomutatedfile.New("Loop Condition", "second.go", []byte("package fixture\n"), []byte("package fixture\n")),
		Primary: ManagedAttemptEvidence{
			Kind: ManagedAttemptInfrastructure, Deadline: 20 * time.Second,
			LaunchDuration: time.Second, CommandDuration: 3 * time.Second,
			Failures: FailureDiagnostics{Wait: "wait failed", Output: "output partial"},
			Output:   OutputSnapshot{Bytes: "partial mutant output", Cutoff: 21, CompleteThroughCutoff: true},
		},
	}
	report := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedAborted, Cause: ManagedAbortPrimaryInfrastructure, Total: 3,
		Mutations:       []ManagedMutationResult{survived, uncertain},
		ArtifactResidue: []string{"/tmp/ooze-residue"},
	}, managedReportConfiguration{minimumThreshold: 0.5})

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
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, "Score:"), "abort published a score or rendered retained mutant output:\n%s", report.text)
	assert.False(t, strings.Contains(report.text, "partial mutant output"), "abort published a score or rendered retained mutant output:\n%s", report.text)
}

func TestManagedReportRetainsConfirmationFailureWhenCampaignAborts(t *testing.T) {
	confirmation := ManagedAttemptEvidence{
		Kind: ManagedAttemptInfrastructure, Deadline: 20 * time.Second,
		CommandDuration: 4 * time.Second, BoundFired: NoBoundFired,
		Failures: FailureDiagnostics{Wait: "confirmation wait failed", Output: "confirmation output partial"},
	}
	report := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedAborted, Cause: ManagedAbortConfirmationInfrastructure, Total: 1,
		Mutations: []ManagedMutationResult{{
			File: gomutatedfile.New("Loop Condition", "confirmed.go", []byte("package fixture\n"), []byte("package fixture\n")),
			Primary: ManagedAttemptEvidence{
				Kind: ManagedAttemptDeadline, Deadline: 20 * time.Second,
				CommandDuration: 20 * time.Second, ConfirmationProvisional: true,
			},
			Confirmation: &confirmation,
		}},
	}, managedReportConfiguration{})

	for _, fragment := range []string{
		"Confirmation infrastructure uncertainty: confirmed.go → Loop Condition",
		"primary timed out at 20s with peer overlap",
		"confirmation wait: failure recorded",
		"confirmation output: failure recorded",
		"bound fired: none",
	} {
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, "exclusive confirmation failed"), "confirmation infrastructure was mislabeled as a command failure:\n%s", report.text)
}

func TestManagedReportUsesAuthoritativeConfirmationPeakAndReportsAbortFallback(t *testing.T) {
	confirmation := ManagedAttemptEvidence{
		Kind: ManagedAttemptDeadline, CommandDuration: 20 * time.Second,
		Count: ObservedCount{Value: 11, Present: true},
	}
	confirmed := ManagedMutationResult{
		File:    gomutatedfile.New("Loop Condition", "timeout.go", []byte("package fixture\n"), []byte("package fixture\n")),
		Outcome: ManagedTimedOut,
		Primary: ManagedAttemptEvidence{
			Kind: ManagedAttemptDeadline, CommandDuration: 20 * time.Second,
			Count: ObservedCount{Value: 7, Present: true}, ConfirmationProvisional: true,
		},
		Confirmation: &confirmation,
	}
	completed := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedCompleted, Mutations: []ManagedMutationResult{confirmed},
	}, managedReportConfiguration{})
	assert.True(t, strings.Contains(completed.text, "observed running peak: 11"), "report did not use authoritative confirmation peak:\n%s", completed.text)
	assert.False(t, strings.Contains(completed.text, "observed running peak: 7"), "report did not use authoritative confirmation peak:\n%s", completed.text)

	aborted := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedAborted, Cause: ManagedAbortPrimaryInfrastructure, Total: 1,
		SingleAdmissionFallback: true,
	}, managedReportConfiguration{})
	assert.True(t, strings.Contains(aborted.text, "Ooze fell back to single-admission automatic"), "aborted report hid the process fallback:\n%s", aborted.text)
}

func TestManagedReportConsolidatesCleanupResidualsBeforeOnePanic(t *testing.T) {
	report := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedCleanupUnconfirmed,
		FatalAttempts: []ManagedFatalAttemptEvidence{{
			Attempt: "campaign-1:3",
			Evidence: ManagedAttemptEvidence{
				Kind:     ManagedAttemptDrainUnconfirmed,
				Output:   OutputSnapshot{Bytes: "private output", Cutoff: 14, CompleteThroughCutoff: true},
				Failures: FailureDiagnostics{DrainCensus: "census unavailable", Termination: "kill failed"},
			},
		}},
		Residual: []ManagedResidualCustody{
			{Attempt: "campaign-1:2", Generation: 7, Stage: ManagedResidualProspective},
			{Attempt: "campaign-1:3", Generation: 9, Stage: ManagedResidualOwned, Transferred: true},
		},
	}, managedReportConfiguration{})

	assert.Equal(t, managedReportPanic, report.disposition, "fatal disposition = %#v, want exactly one cleanup panic", report)
	assert.EqualValues(t, "ooze: cleanup unconfirmed", report.panicValue, "fatal disposition = %#v, want exactly one cleanup panic", report)
	first := strings.Index(report.text, "prospective attempt campaign-1:2")
	second := strings.Index(report.text, "owned attempt campaign-1:3 (custody transferred)")
	assert.False(t, first < 0, "residual custody is absent or reordered:\n%s", report.text)
	assert.False(t, second <= first, "residual custody is absent or reordered:\n%s", report.text)
	for _, fragment := range []string{
		"Containment fault. Ooze cannot prove every test process has exited.",
		"The process runtime is closed for the remainder of this process.",
		"2 execution-domain obligations remain unresolved:",
		"Attempt diagnostic: campaign-1:3",
		"output prefix: cutoff=14 complete-through-cutoff=true final=false",
		"termination: failure recorded",
		"drain census: failure recorded",
	} {
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, "Score:"), "fatal report embedded a score or second panic presentation:\n%s", report.text)
	assert.False(t, strings.Contains(report.text, "panic:"), "fatal report embedded a score or second panic presentation:\n%s", report.text)
	assert.False(t, strings.Contains(report.text, "private output"), "fatal report embedded a score or second panic presentation:\n%s", report.text)
	assert.False(t, strings.Contains(report.text, "generation"), "fatal report embedded a score or second panic presentation:\n%s", report.text)
	for _, line := range strings.Split(report.text, "\n") {
		assert.False(t, !strings.HasPrefix(line, "┃") && !strings.HasPrefix(line, "┏") &&
			!strings.HasPrefix(line, "┗"), "fatal report line escaped its box: %q\n%s", line, report.text)
	}
}

func TestManagedReportLetsInvariantViolationDominateBeforeOnePanic(t *testing.T) {
	report := projectManagedReport(ManagedReleaseResult{
		Outcome: ManagedInvariantViolation,
		Invariant: &ManagedInvariantEvidence{
			Operation: "campaign advance", Reason: "terminal event is stale", Phase: "Confirming",
			RejectedEvent:    "attempt terminal/attempt=campaign-1:3",
			StableIdentities: []string{"campaign-1", "campaign-1:3"},
			Obligations:      []string{"admission/campaign-1:3", "execution-domain/campaign-1:3"},
			TraceTail:        []string{"event 4", "event 5"},
		},
		Residual: []ManagedResidualCustody{{
			Attempt: "campaign-1:3", Generation: 9, Stage: ManagedResidualOwned,
		}},
	}, managedReportConfiguration{})

	assert.Equal(t, managedReportPanic, report.disposition, "fatal disposition = %#v, want exactly one invariant panic", report)
	assert.EqualValues(t, "ooze: invariant violation", report.panicValue, "fatal disposition = %#v, want exactly one invariant panic", report)
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
		assert.True(t, strings.Contains(report.text, fragment), "report missing %q:\n%s", fragment, report.text)
	}
	assert.False(t, strings.Contains(report.text, "Containment fault"), "invariant did not dominate fatal presentation:\n%s", report.text)
	assert.False(t, strings.Contains(report.text, "Score:"), "invariant did not dominate fatal presentation:\n%s", report.text)
}
