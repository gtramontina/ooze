package ooze_test

import (
	"errors"
	"testing"
	"time"

	managed "github.com/gtramontina/ooze/internal/ooze"
)

func TestMutationAttemptPlanUsesAbsoluteOverrideUnchanged(t *testing.T) {
	t.Parallel()

	const override = 37*time.Second + 19*time.Millisecond
	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: 90 * time.Second,
		Peers:            14,
		Override:         override,
		Profile:          managed.AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	if plan.Deadline() != override {
		t.Fatalf("deadline = %s, want absolute override %s", plan.Deadline(), override)
	}
	if plan.Profile() != managed.AutomaticProfile {
		t.Fatalf("profile = %v, want automatic", plan.Profile())
	}
}

func TestMutationAttemptPlanRejectsUnresolvedFacts(t *testing.T) {
	t.Parallel()

	tests := map[string]managed.MutationAttemptPlanInput{
		"baseline": {Peers: 1, Profile: managed.AutomaticProfile},
		"peers": {
			BaselineDuration: time.Second,
			Profile:          managed.AutomaticProfile,
		},
		"override": {
			BaselineDuration: time.Second,
			Peers:            1,
			Override:         -time.Second,
			Profile:          managed.AutomaticProfile,
		},
		"profile": {BaselineDuration: time.Second, Peers: 1},
	}
	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := managed.NewMutationAttemptPlan(input)
			if !errors.Is(err, managed.ErrInvalidMutationAttemptPlan) {
				t.Fatalf("error = %v, want ErrInvalidMutationAttemptPlan", err)
			}
		})
	}
}

func TestMutationAttemptPlanDerivesDeadlineFromPermittedPeers(t *testing.T) {
	t.Parallel()

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: 24 * time.Second,
		Peers:            14,
		Profile:          managed.AutomaticProfile,
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

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Profile:          managed.SerialProfile,
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

	_, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: time.Duration(1 << 62),
		Peers:            2,
		Profile:          managed.AutomaticProfile,
	})
	if !errors.Is(err, managed.ErrInvalidMutationAttemptPlan) {
		t.Fatalf("error = %v, want ErrInvalidMutationAttemptPlan", err)
	}
}

func TestMutationAttemptPlanBuildsConfirmationFromPrimaryExecutionFacts(t *testing.T) {
	t.Parallel()

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: 4 * time.Second,
		Peers:            2,
		Override:         31 * time.Second,
		Profile:          managed.AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	primary := managed.Spec{
		Attempt: "primary/a", Command: []string{"opaque-test", "--flag"},
		Dir: "/snapshot/primary", Env: []string{"GOMAXPROCS=1", "OPAQUE=value"},
		Profile: managed.AutomaticProfile, Deadline: 31 * time.Second,
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

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Override:         31 * time.Second,
		Profile:          managed.AutomaticProfile,
	})
	if err != nil {
		t.Fatalf("new mutation attempt plan: %v", err)
	}
	valid := managed.Spec{
		Attempt: "primary/a", Command: []string{"opaque-test"}, Dir: "/snapshot/primary",
		Profile: managed.AutomaticProfile, Deadline: 31 * time.Second,
	}
	tests := map[string]struct {
		primary   managed.Spec
		attempt   string
		workspace string
	}{
		"same attempt":   {primary: valid, attempt: valid.Attempt, workspace: "/snapshot/confirmation"},
		"same workspace": {primary: valid, attempt: "confirmation/a", workspace: valid.Dir},
		"wrong profile": {
			primary: func() managed.Spec { changed := valid; changed.Profile = managed.SerialProfile; return changed }(),
			attempt: "confirmation/a", workspace: "/snapshot/confirmation",
		},
		"wrong deadline": {
			primary: func() managed.Spec { changed := valid; changed.Deadline = time.Minute; return changed }(),
			attempt: "confirmation/a", workspace: "/snapshot/confirmation",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := plan.ConfirmationSpec(test.primary, test.attempt, test.workspace)
			if !errors.Is(err, managed.ErrInvalidMutationAttemptPlan) {
				t.Fatalf("error = %v, want ErrInvalidMutationAttemptPlan", err)
			}
		})
	}
}

func TestPrimaryNonzeroSettlementStaysKilledAcrossOpaqueOutput(t *testing.T) {
	t.Parallel()

	primary := managed.Settled{
		Exit: managed.ExitStatus{Code: 2},
		ExecutionData: managed.ExecutionData{
			Deadline: 11 * time.Minute,
			Output: managed.OutputSnapshot{
				Bytes: "panic: test timed out after 10m0s", Cutoff: 36,
				CompleteThroughCutoff: true, Final: true,
			},
		},
	}
	disposition := managed.ClassifyPrimaryMutation(primary, false)
	authoritative, ok := disposition.(managed.AttributableMutation)
	if !ok {
		t.Fatalf("disposition = %T, want AttributableMutation", disposition)
	}
	if authoritative.Outcome != managed.MutationKilled || authoritative.Primary != primary ||
		authoritative.Confirmation != nil || authoritative.PressureValidated {
		t.Fatalf("authoritative mutation = %#v, want direct opaque killed result", authoritative)
	}
}

func TestPrimaryDeadlineRequiresConfirmationOnlyWithRecordedOverlap(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, CommandDuration: 31 * time.Second,
			BoundFired: managed.CommandDeadlineFired,
		},
	}
	direct := managed.ClassifyPrimaryMutation(primary, false)
	authoritative, ok := direct.(managed.AttributableMutation)
	if !ok || authoritative.Outcome != managed.MutationTimedOut || authoritative.Primary != primary {
		t.Fatalf("non-overlapped deadline = %#v, want direct TimedOut", direct)
	}

	ambiguous := managed.ClassifyPrimaryMutation(primary, true)
	provisional, ok := ambiguous.(managed.MutationNeedsConfirmation)
	if !ok || provisional.Primary() != primary {
		t.Fatalf("overlapped deadline = %#v, want MutationNeedsConfirmation", ambiguous)
	}
}

func TestPrimaryFuseTripIsDirectRunawayDespiteRecordedOverlap(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.FuseTrip{Live: 65},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second,
		},
	}
	disposition := managed.ClassifyPrimaryMutation(primary, true)
	authoritative, ok := disposition.(managed.AttributableMutation)
	if !ok || authoritative.Outcome != managed.MutationRunaway || authoritative.Primary != primary ||
		authoritative.Confirmation != nil || authoritative.PressureValidated {
		t.Fatalf("fuse disposition = %#v, want direct Runaway", disposition)
	}
}

func TestPrimaryUncertaintyNeverBecomesMutationEvidence(t *testing.T) {
	t.Parallel()

	uncertain := []managed.Terminal{
		managed.Stopped{},
		managed.Infrastructure{Cause: managed.CensusFailed, Err: errors.New("census failed")},
	}
	for _, primary := range uncertain {
		disposition := managed.ClassifyPrimaryMutation(primary, false)
		aborted, ok := disposition.(managed.MutationAborted)
		if !ok || aborted.Primary != primary || aborted.Confirmation != nil {
			t.Fatalf("uncertain %T disposition = %#v, want MutationAborted", primary, disposition)
		}
	}

	primary := managed.DrainUnconfirmed{Residual: managed.OwnedUndrained}
	disposition := managed.ClassifyPrimaryMutation(primary, false)
	fatal, ok := disposition.(managed.MutationFatalUncertainty)
	if !ok || fatal.Primary != primary || fatal.Confirmation != nil {
		t.Fatalf("drain-unconfirmed disposition = %#v, want MutationFatalUncertainty", disposition)
	}
}

func TestOrdinaryConfirmationClassifiesOpaqueExitAndValidatesPressure(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 10 * time.Minute,
			BoundFired: managed.CommandDeadlineFired,
		},
	}
	provisional := managed.ClassifyPrimaryMutation(primary, true).(managed.MutationNeedsConfirmation)
	confirmation := managed.Settled{
		Exit: managed.ExitStatus{Code: 2},
		ExecutionData: managed.ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 10 * time.Minute,
			Output: managed.OutputSnapshot{
				Bytes: "panic: test timed out after 10m0s", Cutoff: 36,
				CompleteThroughCutoff: true, Final: true,
			},
		},
	}
	disposition := managed.ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(managed.AttributableMutation)
	if !ok || authoritative.Outcome != managed.MutationKilled || authoritative.Primary != primary ||
		authoritative.Confirmation != confirmation || !authoritative.PressureValidated {
		t.Fatalf("confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
	}
}

func TestPassingConfirmationSurvivesAndValidatesPressure(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, BoundFired: managed.CommandDeadlineFired,
		},
	}
	provisional := managed.ClassifyPrimaryMutation(primary, true).(managed.MutationNeedsConfirmation)
	confirmation := managed.Settled{ExecutionData: managed.ExecutionData{Deadline: 31 * time.Second}}
	disposition := managed.ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(managed.AttributableMutation)
	if !ok || authoritative.Outcome != managed.MutationSurvived || !authoritative.PressureValidated {
		t.Fatalf("passing confirmation = %#v, want Survived with validated pressure", disposition)
	}
}

func TestRepeatedConfirmationDeadlineIsTimedOutWithoutPressure(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, BoundFired: managed.CommandDeadlineFired,
		},
	}
	provisional := managed.ClassifyPrimaryMutation(primary, true).(managed.MutationNeedsConfirmation)
	confirmation := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, BoundFired: managed.CommandDeadlineFired,
		},
	}
	disposition := managed.ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(managed.AttributableMutation)
	if !ok || authoritative.Outcome != managed.MutationTimedOut || authoritative.Primary != primary ||
		authoritative.Confirmation != confirmation || authoritative.PressureValidated {
		t.Fatalf("repeated deadline = %#v, want TimedOut without pressure", disposition)
	}
}

func TestConfirmationFuseTripIsIndependentlyAttributableRunaway(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, BoundFired: managed.CommandDeadlineFired,
		},
	}
	provisional := managed.ClassifyPrimaryMutation(primary, true).(managed.MutationNeedsConfirmation)
	confirmation := managed.Tripped{
		Trip:          managed.FuseTrip{Live: 65},
		ExecutionData: managed.ExecutionData{Deadline: 31 * time.Second},
	}
	disposition := managed.ClassifyMutationConfirmation(provisional, confirmation)
	authoritative, ok := disposition.(managed.AttributableMutation)
	if !ok || authoritative.Outcome != managed.MutationRunaway || authoritative.Primary != primary ||
		authoritative.Confirmation != confirmation || authoritative.PressureValidated {
		t.Fatalf("confirmation fuse = %#v, want independent Runaway without pressure", disposition)
	}
}

func TestConfirmationWithDifferentResolvedDeadlineAbortsUnscored(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, BoundFired: managed.CommandDeadlineFired,
		},
	}
	provisional := managed.ClassifyPrimaryMutation(primary, true).(managed.MutationNeedsConfirmation)
	confirmation := managed.Settled{ExecutionData: managed.ExecutionData{Deadline: 30 * time.Second}}
	disposition := managed.ClassifyMutationConfirmation(provisional, confirmation)
	aborted, ok := disposition.(managed.MutationAborted)
	if !ok || aborted.Primary != primary || aborted.Confirmation != confirmation {
		t.Fatalf("mismatched deadline = %#v, want unscored MutationAborted", disposition)
	}
}

func TestConfirmationUncertaintyNeverBecomesMutationEvidence(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, BoundFired: managed.CommandDeadlineFired,
		},
	}
	provisional := managed.ClassifyPrimaryMutation(primary, true).(managed.MutationNeedsConfirmation)
	confirmation := managed.Infrastructure{
		Cause: managed.CensusFailed, Err: errors.New("census failed"),
		ExecutionData: managed.ExecutionData{Deadline: 31 * time.Second},
	}
	disposition := managed.ClassifyMutationConfirmation(provisional, confirmation)
	aborted, ok := disposition.(managed.MutationAborted)
	if !ok || aborted.Primary != primary || aborted.Confirmation != confirmation {
		t.Fatalf("infrastructure confirmation = %#v, want MutationAborted", disposition)
	}

	unconfirmed := managed.DrainUnconfirmed{
		Residual:      managed.OwnedUndrained,
		ExecutionData: managed.ExecutionData{Deadline: 31 * time.Second},
	}
	disposition = managed.ClassifyMutationConfirmation(provisional, unconfirmed)
	fatal, ok := disposition.(managed.MutationFatalUncertainty)
	if !ok || fatal.Primary != primary || fatal.Confirmation != unconfirmed {
		t.Fatalf("unconfirmed confirmation = %#v, want MutationFatalUncertainty", disposition)
	}
}
