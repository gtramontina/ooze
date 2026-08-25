package ooze

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	require.Equal(t, override, plan.Deadline(), "deadline = %s, want absolute override %s", plan.Deadline(), override)
	assert.Equal(t, AutomaticProfile, plan.Profile(), "profile = %v, want automatic", plan.Profile())
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
			assert.ErrorIs(t, err, ErrInvalidMutationAttemptPlan, "error = %v, want ErrInvalidMutationAttemptPlan", err)
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
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	assert.Equal(t, 588*time.Second, plan.Deadline(), "deadline = %s, want 24.5 * 24s", plan.Deadline())
}

func TestMutationAttemptPlanAppliesTwentySecondFloor(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Profile:          SerialProfile,
	})
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	assert.Equal(t, 20*time.Second, plan.Deadline(), "deadline = %s, want 20s floor", plan.Deadline())
}

func TestMutationAttemptPlanRejectsUnrepresentableDerivedDeadline(t *testing.T) {
	t.Parallel()

	_, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: time.Duration(1 << 62),
		Peers:            2,
		Profile:          AutomaticProfile,
	})
	assert.ErrorIs(t, err, ErrInvalidMutationAttemptPlan, "error = %v, want ErrInvalidMutationAttemptPlan", err)
}

func TestMutationAttemptPlanBuildsConfirmationFromPrimaryExecutionFacts(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: 4 * time.Second,
		Peers:            2,
		Override:         31 * time.Second,
		Profile:          AutomaticProfile,
	})
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	primary := Spec{
		Attempt: "primary/a", Command: []string{"opaque-test", "--flag"},
		Dir: "/snapshot/primary", Env: []string{"GOMAXPROCS=1", "OPAQUE=value"},
		Profile: AutomaticProfile, Deadline: 31 * time.Second,
	}
	confirmation, err := plan.ConfirmationSpec(primary, "confirmation/a", "/snapshot/confirmation")
	require.NoError(t, err, "confirmation spec: %v", err)
	assert.EqualValues(t, "confirmation/a", confirmation.Attempt, "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	assert.EqualValues(t, "/snapshot/confirmation", confirmation.Dir, "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	assert.Equal(t, primary.Profile, confirmation.Profile, "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	assert.Equal(t, primary.Deadline, confirmation.Deadline, "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	require.Len(t, confirmation.Command, 2, "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	assert.EqualValues(t, "opaque-test", confirmation.Command[0], "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	require.Len(t, confirmation.Env, 2, "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)
	assert.EqualValues(t, "OPAQUE=value", confirmation.Env[1], "confirmation spec = %#v, want fresh identity/workspace and primary execution facts", confirmation)

	primary.Command[0] = "changed"
	primary.Env[1] = "changed"
	assert.EqualValues(t, "opaque-test", confirmation.Command[0], "confirmation aliases primary slices: %#v", confirmation)
	assert.EqualValues(t, "OPAQUE=value", confirmation.Env[1], "confirmation aliases primary slices: %#v", confirmation)
}

func TestMutationAttemptPlanRejectsConfirmationThatChangesOwnedFacts(t *testing.T) {
	t.Parallel()

	plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Override:         31 * time.Second,
		Profile:          AutomaticProfile,
	})
	require.NoError(t, err, "new mutation attempt plan: %v", err)
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
			assert.ErrorIs(t, err, ErrInvalidMutationAttemptPlan, "error = %v, want ErrInvalidMutationAttemptPlan", err)
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
	require.True(t, ok, "disposition = %T, want AttributableMutation", disposition)
	assert.Equal(t, MutationKilled, authoritative.Outcome, "authoritative mutation = %#v, want direct opaque killed result", authoritative)
	assert.Equal(t, primary, authoritative.Primary, "authoritative mutation = %#v, want direct opaque killed result", authoritative)
	assert.Nil(t, authoritative.Confirmation, "authoritative mutation = %#v, want direct opaque killed result", authoritative)
	assert.False(t, authoritative.PressureValidated, "authoritative mutation = %#v, want direct opaque killed result", authoritative)
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
	require.True(t, ok, "non-overlapped deadline = %#v, want direct TimedOut", direct)
	assert.Equal(t, MutationTimedOut, authoritative.Outcome, "non-overlapped deadline = %#v, want direct TimedOut", direct)
	assert.Equal(t, primary, authoritative.Primary, "non-overlapped deadline = %#v, want direct TimedOut", direct)
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
	require.True(t, ok, "runtime-proved deadline = %#v, want MutationNeedsConfirmation", disposition)
	assert.Equal(t, primary, provisional.Primary(), "runtime-proved deadline = %#v, want MutationNeedsConfirmation", disposition)
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
	require.True(t, ok, "fuse disposition = %#v, want direct Runaway", disposition)
	assert.Equal(t, MutationRunaway, authoritative.Outcome, "fuse disposition = %#v, want direct Runaway", disposition)
	assert.Equal(t, primary, authoritative.Primary, "fuse disposition = %#v, want direct Runaway", disposition)
	assert.Nil(t, authoritative.Confirmation, "fuse disposition = %#v, want direct Runaway", disposition)
	assert.False(t, authoritative.PressureValidated, "fuse disposition = %#v, want direct Runaway", disposition)
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
		require.True(t, ok, "uncertain %T disposition = %#v, want MutationAborted", primary, disposition)
		assert.Equal(t, primary, aborted.Primary, "uncertain %T disposition = %#v, want MutationAborted", primary, disposition)
		assert.Nil(t, aborted.Confirmation, "uncertain %T disposition = %#v, want MutationAborted", primary, disposition)
	}

	primary := DrainUnconfirmed{Residual: OwnedUndrained}
	disposition := ClassifyPrimaryMutation(primary)
	fatal, ok := disposition.(MutationFatalUncertainty)
	require.True(t, ok, "drain-unconfirmed disposition = %#v, want MutationFatalUncertainty", disposition)
	assert.Equal(t, primary, fatal.Primary, "drain-unconfirmed disposition = %#v, want MutationFatalUncertainty", disposition)
	assert.Nil(t, fatal.Confirmation, "drain-unconfirmed disposition = %#v, want MutationFatalUncertainty", disposition)
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
	require.True(t, ok, "confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
	assert.Equal(t, MutationKilled, authoritative.Outcome, "confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
	assert.Equal(t, primary, authoritative.Primary, "confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
	assert.Equal(t, confirmation, authoritative.Confirmation, "confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
	assert.True(t, authoritative.PressureValidated, "confirmation disposition = %#v, want opaque Killed with validated pressure", disposition)
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
	require.True(t, ok, "passing confirmation = %#v, want Survived with validated pressure", disposition)
	assert.Equal(t, MutationSurvived, authoritative.Outcome, "passing confirmation = %#v, want Survived with validated pressure", disposition)
	assert.True(t, authoritative.PressureValidated, "passing confirmation = %#v, want Survived with validated pressure", disposition)
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
	require.True(t, ok, "repeated deadline = %#v, want TimedOut without pressure", disposition)
	assert.Equal(t, MutationTimedOut, authoritative.Outcome, "repeated deadline = %#v, want TimedOut without pressure", disposition)
	assert.Equal(t, primary, authoritative.Primary, "repeated deadline = %#v, want TimedOut without pressure", disposition)
	assert.Equal(t, confirmation, authoritative.Confirmation, "repeated deadline = %#v, want TimedOut without pressure", disposition)
	assert.False(t, authoritative.PressureValidated, "repeated deadline = %#v, want TimedOut without pressure", disposition)
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
	require.True(t, ok, "confirmation fuse = %#v, want independent Runaway without pressure", disposition)
	assert.Equal(t, MutationRunaway, authoritative.Outcome, "confirmation fuse = %#v, want independent Runaway without pressure", disposition)
	assert.Equal(t, primary, authoritative.Primary, "confirmation fuse = %#v, want independent Runaway without pressure", disposition)
	assert.Equal(t, confirmation, authoritative.Confirmation, "confirmation fuse = %#v, want independent Runaway without pressure", disposition)
	assert.False(t, authoritative.PressureValidated, "confirmation fuse = %#v, want independent Runaway without pressure", disposition)
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
	require.True(t, ok, "mismatched deadline = %#v, want unscored MutationAborted", disposition)
	assert.Equal(t, primary, aborted.Primary, "mismatched deadline = %#v, want unscored MutationAborted", disposition)
	assert.Equal(t, confirmation, aborted.Confirmation, "mismatched deadline = %#v, want unscored MutationAborted", disposition)
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
	require.True(t, ok, "mismatched profile = %#v, want unscored MutationAborted", disposition)
	assert.Equal(t, primary, aborted.Primary, "mismatched profile = %#v, want unscored MutationAborted", disposition)
	assert.Equal(t, confirmation, aborted.Confirmation, "mismatched profile = %#v, want unscored MutationAborted", disposition)
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
	require.True(t, ok, "infrastructure confirmation = %#v, want MutationAborted", disposition)
	assert.Equal(t, primary, aborted.Primary, "infrastructure confirmation = %#v, want MutationAborted", disposition)
	assert.Equal(t, confirmation, aborted.Confirmation, "infrastructure confirmation = %#v, want MutationAborted", disposition)

	unconfirmed := DrainUnconfirmed{
		Residual:      OwnedUndrained,
		ExecutionData: ExecutionData{Deadline: 31 * time.Second},
	}
	disposition = ClassifyMutationConfirmation(provisional, unconfirmed)
	fatal, ok := disposition.(MutationFatalUncertainty)
	require.True(t, ok, "unconfirmed confirmation = %#v, want MutationFatalUncertainty", disposition)
	assert.Equal(t, primary, fatal.Primary, "unconfirmed confirmation = %#v, want MutationFatalUncertainty", disposition)
	assert.Equal(t, unconfirmed, fatal.Confirmation, "unconfirmed confirmation = %#v, want MutationFatalUncertainty", disposition)
}
