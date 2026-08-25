package ooze

import (
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
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

	if report.disposition != managedReportPass {
		t.Fatalf("disposition = %v, want pass at an equal float32 threshold", report.disposition)
	}
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
		if at < 0 {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
		if at < last {
			t.Fatalf("report fragment %q is out of order:\n%s", fragment, report.text)
		}
		last = at
	}
	if strings.Contains(report.text, "Mutant killed: killed.go") {
		t.Fatalf("ordinary killed mutant was rendered:\n%s", report.text)
	}
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
		if !strings.Contains(report.text, fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
	}

	serial := projectManagedReport(ManagedReleaseResult{
		Outcome:   ManagedCompleted,
		Mutations: []ManagedMutationResult{mutation("killed.go", ManagedKilled)},
	}, managedReportConfiguration{minimumThreshold: 1, serial: true})
	if strings.Contains(serial.text, "runaway:") || !strings.Contains(serial.text, "└ timed out:") {
		t.Fatalf("serial summary fabricated runaway evidence:\n%s", serial.text)
	}
}

func TestManagedReportFailsNoMutantsWithoutPublishingScore(t *testing.T) {
	report := projectManagedReport(
		ManagedReleaseResult{Outcome: ManagedNoMutants},
		managedReportConfiguration{minimumThreshold: 1},
	)

	if report.disposition != managedReportError {
		t.Fatalf("disposition = %v, want testing error", report.disposition)
	}
	for _, fragment := range []string{
		"No mutants were discovered. Nothing to score.",
		"WithRepositoryRoot",
		"IgnoreSourceFiles",
		"WithViruses",
		"build constraints",
	} {
		if !strings.Contains(report.text, fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
	}
	if strings.Contains(report.text, "Score:") {
		t.Fatalf("NoMutants published a score:\n%s", report.text)
	}
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
		Outcome: ManagedAborted, Cause: "baseline did not pass", Total: 3,
		Baseline: &baseline,
	}, managedReportConfiguration{minimumThreshold: 0.5})

	if report.disposition != managedReportError {
		t.Fatalf("disposition = %v, want testing error", report.disposition)
	}
	for _, fragment := range []string{
		"Campaign aborted. No mutation score.",
		"Cause: the unmutated baseline failed.",
		"0 of 3 mutants were evaluated.",
		"--- FAIL: TestThing (0.02s)",
		"thing_test.go:41: want 3, got 4",
	} {
		if !strings.Contains(report.text, fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
	}
	if strings.Contains(report.text, "Score:") {
		t.Fatalf("abort published a score:\n%s", report.text)
	}
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
		Outcome: ManagedAborted, Cause: "primary infrastructure uncertainty", Total: 3,
		Mutations:       []ManagedMutationResult{survived, uncertain},
		ArtifactResidue: []string{"/tmp/ooze-residue"},
	}, managedReportConfiguration{minimumThreshold: 0.5})

	for _, fragment := range []string{
		"Mutant survived: first.go → Integer Increment",
		"Infrastructure uncertainty: second.go → Loop Condition",
		"wait: wait failed",
		"output: output partial",
		"Cause: primary infrastructure uncertainty",
		"Evaluated 1 of 3 mutants: 0 detected, 1 survived.",
		"Those results are real, but 2 mutants were not attributed, so no score can be computed.",
		"Artifact residue — remove manually:",
		"/tmp/ooze-residue",
	} {
		if !strings.Contains(report.text, fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
	}
	if strings.Contains(report.text, "Score:") || strings.Contains(report.text, "partial mutant output") {
		t.Fatalf("abort published a score or rendered retained mutant output:\n%s", report.text)
	}
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

	if report.disposition != managedReportPanic || report.panicValue != "ooze: cleanup unconfirmed" {
		t.Fatalf("fatal disposition = %#v, want exactly one cleanup panic", report)
	}
	first := strings.Index(report.text, "prospective attempt campaign-1:2 (generation 7)")
	second := strings.Index(report.text, "owned attempt campaign-1:3 (generation 9, custody transferred)")
	if first < 0 || second <= first {
		t.Fatalf("residual custody is absent or reordered:\n%s", report.text)
	}
	for _, fragment := range []string{
		"Containment fault. Ooze cannot prove every test process has exited.",
		"The process runtime is closed for the remainder of this process.",
		"2 execution-domain obligations remain unresolved:",
		"Attempt diagnostic: campaign-1:3",
		"output prefix: cutoff=14 complete-through-cutoff=true final=false",
		"termination: kill failed",
		"drain census: census unavailable",
	} {
		if !strings.Contains(report.text, fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
	}
	if strings.Contains(report.text, "Score:") || strings.Contains(report.text, "panic:") ||
		strings.Contains(report.text, "private output") {
		t.Fatalf("fatal report embedded a score or second panic presentation:\n%s", report.text)
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

	if report.disposition != managedReportPanic || report.panicValue != "ooze: invariant violation" {
		t.Fatalf("fatal disposition = %#v, want exactly one invariant panic", report)
	}
	for _, fragment := range []string{
		"Internal invariant violated. No campaign in this process is scored.",
		"Operation: campaign advance",
		"Reason: terminal event is stale",
		"Phase: Confirming",
		"Rejected event: attempt terminal/attempt=campaign-1:3",
		"Stable identities: campaign-1, campaign-1:3",
		"Obligations: admission/campaign-1:3, execution-domain/campaign-1:3",
		"Trace tail:",
		"event 4",
		"1 unresolved execution-domain obligation joined this fatal epoch.",
	} {
		if !strings.Contains(report.text, fragment) {
			t.Fatalf("report missing %q:\n%s", fragment, report.text)
		}
	}
	if strings.Contains(report.text, "Containment fault") || strings.Contains(report.text, "Score:") {
		t.Fatalf("invariant did not dominate fatal presentation:\n%s", report.text)
	}
}
