package ooze

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
