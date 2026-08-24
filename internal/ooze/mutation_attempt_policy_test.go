package ooze

import (
	"errors"
	"testing"
	"time"
)

func TestMutationAttemptPlanUsesAbsoluteOverrideUnchanged(t *testing.T) {
	t.Parallel()

	const override = 37*time.Second + 19*time.Millisecond
	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: 90 * time.Second,
		Peers:            14,
		Override:         override,
		Profile:          AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	if plan.Deadline() != override {
		t.Fatalf("deadline = %s, want absolute override %s", plan.Deadline(), override)
	}
	if plan.Profile() != AutomaticProfile {
		t.Fatalf("profile = %v, want automatic", plan.Profile())
	}
}

func TestMutationAttemptPlanRejectsUnresolvedFacts(t *testing.T) {
	t.Parallel()

	tests := map[string]MutationAttemptPlanInput{
		"baseline": {Peers: 1, Profile: AutomaticProfile},
		"peers": {
			BaselineDuration: time.Second,
			Profile:          AutomaticProfile,
		},
		"override": {
			BaselineDuration: time.Second,
			Peers:            1,
			Override:         -time.Second,
			Profile:          AutomaticProfile,
		},
		"profile": {BaselineDuration: time.Second, Peers: 1},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewMutationAttemptPlan(input)
			if !errors.Is(err, ErrInvalidMutationAttemptPlan) {
				t.Fatalf("error = %v, want ErrInvalidMutationAttemptPlan", err)
			}
		})
	}
}

func TestMutationAttemptPlanDerivesDeadlineFromPermittedPeers(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: 24 * time.Second,
		Peers:            14,
		Profile:          AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	if plan.Deadline() != 588*time.Second {
		t.Fatalf("deadline = %s, want 24.5 * 24s", plan.Deadline())
	}
}

func TestMutationAttemptPlanAppliesTwentySecondFloor(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Profile:          SerialProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	if plan.Deadline() != 20*time.Second {
		t.Fatalf("deadline = %s, want 20s floor", plan.Deadline())
	}
}

func TestMutationAttemptPlanRejectsUnrepresentableDerivedDeadline(t *testing.T) {
	t.Parallel()

	_, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: time.Duration(1 << 62),
		Peers:            2,
		Profile:          AutomaticProfile,
	})
	if !errors.Is(err, ErrInvalidMutationAttemptPlan) {
		t.Fatalf("error = %v, want ErrInvalidMutationAttemptPlan", err)
	}
}

func TestMutationAttemptPlanBuildsConfirmationFromPrimaryExecutionFacts(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: 4 * time.Second,
		Peers:            2,
		Override:         31 * time.Second,
		Profile:          AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	primary := Spec{
		Attempt: "primary/a", Command: []string{"opaque-test", "--flag"},
		Dir: "/snapshot/primary", Env: []string{"GOMAXPROCS=1", "OPAQUE=value"},
		Profile: AutomaticProfile, Deadline: 31 * time.Second,
	}
	confirmation, err := plan.ConfirmationSpec(primary, "confirmation/a", "/snapshot/confirmation")
	if err != nil {
		t.Fatalf("confirmation spec: %v", err)
	}
	if confirmation.Attempt != "confirmation/a" || confirmation.Dir != "/snapshot/confirmation" ||
		confirmation.Profile != primary.Profile || confirmation.Deadline != primary.Deadline ||
		len(confirmation.Command) != 2 || confirmation.Command[0] != "opaque-test" ||
		len(confirmation.Env) != 2 || confirmation.Env[1] != "OPAQUE=value" {
		t.Fatalf("confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	}

	primary.Command[0] = "changed"
	primary.Env[1] = "changed"
	if confirmation.Command[0] != "opaque-test" || confirmation.Env[1] != "OPAQUE=value" {
		t.Fatalf("confirmation aliases primary slices: %#v", confirmation)
	}
}

func TestMutationAttemptPlanRejectsConfirmationThatChangesOwnedFacts(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Override:         31 * time.Second,
		Profile:          AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	valid := Spec{
		Attempt: "primary/a", Command: []string{"opaque-test"}, Dir: "/snapshot/primary",
		Profile: AutomaticProfile, Deadline: 31 * time.Second,
	}
	tests := map[string]struct {
		primary   Spec
		attempt   string
		workspace string
	}{
		"same attempt":   {primary: valid, attempt: valid.Attempt, workspace: "/snapshot/confirmation"},
		"same workspace": {primary: valid, attempt: "confirmation/a", workspace: valid.Dir},
		"wrong profile": {
			primary: func() Spec { changed := valid; changed.Profile = SerialProfile; return changed }(),
			attempt: "confirmation/a", workspace: "/snapshot/confirmation",
		},
		"wrong deadline": {
			primary: func() Spec { changed := valid; changed.Deadline = time.Minute; return changed }(),
			attempt: "confirmation/a", workspace: "/snapshot/confirmation",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := plan.ConfirmationSpec(test.primary, test.attempt, test.workspace)
			if !errors.Is(err, ErrInvalidMutationAttemptPlan) {
				t.Fatalf("error = %v, want ErrInvalidMutationAttemptPlan", err)
			}
		})
	}
}

func TestPrimaryNonzeroSettlementStaysKilledAcrossOpaqueOutput(t *testing.T) {
	t.Parallel()

	primary := Settled{
		Exit: ExitStatus{Code: 2},
		ExecutionData: ExecutionData{
			Deadline: 11 * time.Minute,
			Output: OutputSnapshot{
				Bytes: "panic: test timed out after 10m0s", Cutoff: 36,
				CompleteThroughCutoff: true, Final: true,
			},
		},
	}
	disposition := ClassifyPrimaryMutation(primary)
	authoritative, ok := disposition.(AttributableMutation)
	if !ok {
		t.Fatalf("disposition = %T, want AttributableMutation", disposition)
	}
	if authoritative.Outcome != MutationKilled || authoritative.Primary != primary ||
		authoritative.Confirmation != nil || authoritative.PressureValidated {
		t.Fatalf("authoritative mutation = %#v, want direct opaque killed result", authoritative)
	}
}

func TestPrimaryDeadlineWithoutRuntimeOverlapProofIsDirect(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, CommandDuration: 31 * time.Second,
			BoundFired: CommandDeadlineFired,
		},
	}
	direct := ClassifyPrimaryMutation(primary)
	authoritative, ok := direct.(AttributableMutation)
	if !ok || authoritative.Outcome != MutationTimedOut || authoritative.Primary != primary {
		t.Fatalf("non-overlapped deadline = %#v, want direct TimedOut", direct)
	}
}

func TestPrimaryDeadlineConsumesRuntimeIssuedOverlapProof(t *testing.T) {
	t.Parallel()

	primary := publicTerminal(
		supervisorTerminalEvidence{
			kind: supervisorTerminalAutomaticDeadlineTrip, commandDeadline: 31 * time.Second,
			commandDuration: 31 * time.Second, firedBound: supervisorCommandDeadlineFired,
		},
		func(supervisorOutputRef) string { return "" },
		func(supervisorDiagnosticRef) error { return nil },
		supervisorRuntimeProvisionalDeadline,
	)
	disposition := ClassifyPrimaryMutation(primary)
	provisional, ok := disposition.(MutationNeedsConfirmation)
	if !ok || provisional.Primary() != primary {
		t.Fatalf("runtime-proved deadline = %#v, want MutationNeedsConfirmation", disposition)
	}
}

func TestPrimaryFuseTripIsDirectRunawayDespiteRecordedOverlap(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: FuseTrip{Live: 65},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second,
		},
	}
	disposition := ClassifyPrimaryMutation(primary)
	authoritative, ok := disposition.(AttributableMutation)
	if !ok || authoritative.Outcome != MutationRunaway || authoritative.Primary != primary ||
		authoritative.Confirmation != nil || authoritative.PressureValidated {
		t.Fatalf("fuse disposition = %#v, want direct Runaway", disposition)
	}
}

func TestPrimaryUncertaintyNeverBecomesMutationEvidence(t *testing.T) {
	t.Parallel()

	uncertain := []Terminal{
		Stopped{},
		Infrastructure{Cause: CensusFailed, Err: errors.New("census failed")},
	}
	for _, primary := range uncertain {
		disposition := ClassifyPrimaryMutation(primary)
		aborted, ok := disposition.(MutationAborted)
		if !ok || aborted.Primary != primary || aborted.Confirmation != nil {
			t.Fatalf("uncertain %T disposition = %#v, want MutationAborted", primary, disposition)
		}
	}

	primary := DrainUnconfirmed{Residual: OwnedUndrained}
	disposition := ClassifyPrimaryMutation(primary)
	fatal, ok := disposition.(MutationFatalUncertainty)
	if !ok || fatal.Primary != primary || fatal.Confirmation != nil {
		t.Fatalf("drain-unconfirmed disposition = %#v, want MutationFatalUncertainty", disposition)
	}
}

func TestOrdinaryConfirmationClassifiesOpaqueExitAndValidatesPressure(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 10 * time.Minute,
			BoundFired: CommandDeadlineFired,
		},
	}
	primary.confirmationProvisional = true
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Settled{
		Exit: ExitStatus{Code: 2},
		ExecutionData: ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 10 * time.Minute,
			Output: OutputSnapshot{
				Bytes: "panic: test timed out after 10m0s", Cutoff: 36,
				CompleteThroughCutoff: true, Final: true,
			},
		},
	}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(AttributableMutation)
	if !ok || authoritative.Outcome != MutationKilled || authoritative.Primary != primary ||
		authoritative.Confirmation != confirmation || !authoritative.PressureValidated {
		t.Fatalf("confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
	}
}

func TestPassingConfirmationSurvivesAndValidatesPressure(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, BoundFired: CommandDeadlineFired,
		},
	}
	primary.confirmationProvisional = true
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Settled{ExecutionData: ExecutionData{Deadline: 31 * time.Second}}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(AttributableMutation)
	if !ok || authoritative.Outcome != MutationSurvived || !authoritative.PressureValidated {
		t.Fatalf("passing confirmation = %#v, want Survived with validated pressure", disposition)
	}
}

func TestRepeatedConfirmationDeadlineIsTimedOutWithoutPressure(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, BoundFired: CommandDeadlineFired,
		},
	}
	primary.confirmationProvisional = true
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, BoundFired: CommandDeadlineFired,
		},
	}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(AttributableMutation)
	if !ok || authoritative.Outcome != MutationTimedOut || authoritative.Primary != primary ||
		authoritative.Confirmation != confirmation || authoritative.PressureValidated {
		t.Fatalf("repeated deadline = %#v, want TimedOut without pressure", disposition)
	}
}

func TestConfirmationFuseTripIsIndependentlyAttributableRunaway(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, BoundFired: CommandDeadlineFired,
		},
	}
	primary.confirmationProvisional = true
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Tripped{
		Trip:          FuseTrip{Live: 65},
		ExecutionData: ExecutionData{Deadline: 31 * time.Second},
	}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(AttributableMutation)
	if !ok || authoritative.Outcome != MutationRunaway || authoritative.Primary != primary ||
		authoritative.Confirmation != confirmation || authoritative.PressureValidated {
		t.Fatalf("confirmation fuse = %#v, want independent Runaway without pressure", disposition)
	}
}

func TestConfirmationWithDifferentResolvedDeadlineAbortsUnscored(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, BoundFired: CommandDeadlineFired,
		},
	}
	primary.confirmationProvisional = true
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Settled{ExecutionData: ExecutionData{Deadline: 30 * time.Second}}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	aborted, ok := disposition.(MutationAborted)
	if !ok || aborted.Primary != primary || aborted.Confirmation != confirmation {
		t.Fatalf("mismatched deadline = %#v, want unscored MutationAborted", disposition)
	}
}

func TestConfirmationWithDifferentExecutionProfileAbortsUnscored(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, profile: AutomaticProfile,
			BoundFired: CommandDeadlineFired, confirmationProvisional: true,
		},
	}
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Settled{ExecutionData: ExecutionData{
		Deadline: 31 * time.Second, profile: SerialProfile,
	}}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	aborted, ok := disposition.(MutationAborted)
	if !ok || aborted.Primary != primary || aborted.Confirmation != confirmation {
		t.Fatalf("mismatched profile = %#v, want unscored MutationAborted", disposition)
	}
}

func TestConfirmationUncertaintyNeverBecomesMutationEvidence(t *testing.T) {
	t.Parallel()

	primary := Tripped{
		Trip: AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{
			Deadline: 31 * time.Second, BoundFired: CommandDeadlineFired,
		},
	}
	primary.confirmationProvisional = true
	provisional := ClassifyPrimaryMutation(primary).(MutationNeedsConfirmation)
	confirmation := Infrastructure{
		Cause: CensusFailed, Err: errors.New("census failed"),
		ExecutionData: ExecutionData{Deadline: 31 * time.Second},
	}
	disposition := ClassifyMutationConfirmation(provisional, confirmation)
	aborted, ok := disposition.(MutationAborted)
	if !ok || aborted.Primary != primary || aborted.Confirmation != confirmation {
		t.Fatalf("infrastructure confirmation = %#v, want MutationAborted", disposition)
	}

	unconfirmed := DrainUnconfirmed{
		Residual:      OwnedUndrained,
		ExecutionData: ExecutionData{Deadline: 31 * time.Second},
	}
	disposition = ClassifyMutationConfirmation(provisional, unconfirmed)
	fatal, ok := disposition.(MutationFatalUncertainty)
	if !ok || fatal.Primary != primary || fatal.Confirmation != unconfirmed {
		t.Fatalf("unconfirmed confirmation = %#v, want MutationFatalUncertainty", disposition)
	}
}
