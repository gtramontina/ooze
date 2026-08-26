package ooze_test

import (
	"testing"
	"time"

	managed "github.com/gtramontina/ooze/internal/ooze"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	require.Equal(t, override, plan.Deadline(), "deadline = %s, want absolute override %s", plan.Deadline(), override)
	assert.Equal(t, managed.AutomaticProfile, plan.Profile(), "profile = %v, want automatic", plan.Profile())
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
			assert.ErrorIs(t, err, managed.ErrInvalidMutationAttemptPlan, "error = %v, want managed.ErrInvalidMutationAttemptPlan", err)
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
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	assert.Equal(t, 588*time.Second, plan.Deadline(), "deadline = %s, want 24.5 * 24s", plan.Deadline())
}

func TestMutationAttemptPlanAppliesTwentySecondFloor(t *testing.T) {
	t.Parallel()

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Profile:          managed.SerialProfile,
	})
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	assert.Equal(t, 20*time.Second, plan.Deadline(), "deadline = %s, want 20s floor", plan.Deadline())
}

func TestMutationAttemptPlanRejectsUnrepresentableDerivedDeadline(t *testing.T) {
	t.Parallel()

	_, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: time.Duration(1 << 62),
		Peers:            2,
		Profile:          managed.AutomaticProfile,
	})
	assert.ErrorIs(t, err, managed.ErrInvalidMutationAttemptPlan, "error = %v, want managed.ErrInvalidMutationAttemptPlan", err)
}

func TestMutationAttemptPlanBuildsConfirmationFromPrimaryExecutionFacts(t *testing.T) {
	t.Parallel()

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: 4 * time.Second,
		Peers:            2,
		Override:         31 * time.Second,
		Profile:          managed.AutomaticProfile,
	})
	require.NoError(t, err, "new mutation attempt plan: %v", err)
	primary := managed.Spec{
		Attempt: "primary/a", Command: []string{"opaque-test", "--flag"},
		Dir: "/snapshot/primary", Env: []string{"GOMAXPROCS=1", "OPAQUE=value"},
		Profile: managed.AutomaticProfile, Deadline: 31 * time.Second,
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

	plan, err := managed.NewMutationAttemptPlan(managed.MutationAttemptPlanInput{
		BaselineDuration: time.Second,
		Peers:            1,
		Override:         31 * time.Second,
		Profile:          managed.AutomaticProfile,
	})
	require.NoError(t, err, "new mutation attempt plan: %v", err)
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
			assert.ErrorIs(t, err, managed.ErrInvalidMutationAttemptPlan, "error = %v, want managed.ErrInvalidMutationAttemptPlan", err)
		})
	}
}
