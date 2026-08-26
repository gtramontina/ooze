package ooze_test

import (
	"errors"
	"testing"
	"time"

	managed "github.com/gtramontina/ooze/internal/ooze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	disposition := managed.ClassifyPrimaryMutation(primary)
	authoritative, ok := disposition.(managed.AttributableMutation)
	require.True(t, ok, "disposition = %T, want AttributableMutation", disposition)
	assert.Equal(t, managed.MutationKilled, authoritative.Outcome)
	assert.Equal(t, primary, authoritative.Primary)
	assert.Nil(t, authoritative.Confirmation)
	assert.False(t, authoritative.PressureValidated)
}

func TestPrimaryDeadlineWithoutRuntimeOverlapProofIsDirect(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip: managed.AutomaticDeadlineTrip{},
		ExecutionData: managed.ExecutionData{
			Deadline: 31 * time.Second, CommandDuration: 31 * time.Second,
			BoundFired: managed.CommandDeadlineFired,
		},
	}
	disposition := managed.ClassifyPrimaryMutation(primary)
	authoritative, ok := disposition.(managed.AttributableMutation)
	require.True(t, ok, "non-overlapped deadline = %#v, want direct TimedOut", disposition)
	assert.Equal(t, managed.MutationTimedOut, authoritative.Outcome)
	assert.Equal(t, primary, authoritative.Primary)
}

func TestPrimaryFuseTripIsDirectRunawayDespiteRecordedOverlap(t *testing.T) {
	t.Parallel()

	primary := managed.Tripped{
		Trip:          managed.FuseTrip{Live: 65},
		ExecutionData: managed.ExecutionData{Deadline: 31 * time.Second},
	}
	disposition := managed.ClassifyPrimaryMutation(primary)
	authoritative, ok := disposition.(managed.AttributableMutation)
	require.True(t, ok, "fuse disposition = %#v, want direct Runaway", disposition)
	assert.Equal(t, managed.MutationRunaway, authoritative.Outcome)
	assert.Equal(t, primary, authoritative.Primary)
	assert.Nil(t, authoritative.Confirmation)
	assert.False(t, authoritative.PressureValidated)
}

func TestPrimaryUncertaintyNeverBecomesMutationEvidence(t *testing.T) {
	t.Parallel()

	uncertain := []struct {
		name    string
		primary managed.Terminal
	}{
		{"stopped_aborts_mutation", managed.Stopped{}},
		{"infrastructure_aborts_mutation", managed.Infrastructure{
			Cause: managed.CensusFailed, Err: errors.New("census failed"),
		}},
	}
	for _, test := range uncertain {
		t.Run(test.name, func(t *testing.T) {
			disposition := managed.ClassifyPrimaryMutation(test.primary)
			aborted, ok := disposition.(managed.MutationAborted)
			require.True(t, ok, "uncertain %T disposition = %#v, want MutationAborted", test.primary, disposition)
			assert.Equal(t, test.primary, aborted.Primary)
			assert.Nil(t, aborted.Confirmation)
		})
	}

	t.Run("drain_unconfirmed_is_fatal_uncertainty", func(t *testing.T) {
		primary := managed.DrainUnconfirmed{Residual: managed.OwnedUndrained}
		disposition := managed.ClassifyPrimaryMutation(primary)
		fatal, ok := disposition.(managed.MutationFatalUncertainty)
		require.True(t, ok, "drain-unconfirmed disposition = %#v, want MutationFatalUncertainty", disposition)
		assert.Equal(t, primary, fatal.Primary)
		assert.Nil(t, fatal.Confirmation)
	})
}
