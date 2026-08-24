package ooze

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalidMutationAttemptPlan identifies unresolved mutation-attempt facts.
var ErrInvalidMutationAttemptPlan = errors.New("invalid mutation attempt plan")

// MutationAttemptPlanInput contains the already-observed facts used to fix one
// campaign's mutation-attempt execution policy.
type MutationAttemptPlanInput struct {
	BaselineDuration time.Duration
	Peers            int
	Override         time.Duration
	Profile          Profile
}

// MutationAttemptPlan fixes the facts shared by every primary and confirmation.
type MutationAttemptPlan struct {
	deadline time.Duration
	profile  Profile
}

// NewMutationAttemptPlan resolves one immutable mutation-attempt plan.
func NewMutationAttemptPlan(input MutationAttemptPlanInput) (MutationAttemptPlan, error) {
	switch {
	case input.BaselineDuration <= 0:
		return MutationAttemptPlan{}, fmt.Errorf("%w: baseline duration must be positive", ErrInvalidMutationAttemptPlan)
	case input.Peers <= 0:
		return MutationAttemptPlan{}, fmt.Errorf("%w: permitted peers must be positive", ErrInvalidMutationAttemptPlan)
	case input.Override < 0:
		return MutationAttemptPlan{}, fmt.Errorf("%w: timeout override cannot be negative", ErrInvalidMutationAttemptPlan)
	case input.Profile != AutomaticProfile && input.Profile != SerialProfile:
		return MutationAttemptPlan{}, fmt.Errorf("%w: execution profile is invalid", ErrInvalidMutationAttemptPlan)
	}
	deadline := input.Override
	if deadline == 0 {
		if uint64(input.Peers) > uint64((math.MaxInt64-7)/3) {
			return MutationAttemptPlan{}, fmt.Errorf("%w: derived deadline exceeds time.Duration", ErrInvalidMutationAttemptPlan)
		}
		halfUnits := 3*int64(input.Peers) + 7
		wholeBaseline := int64(input.BaselineDuration) / 2
		if wholeBaseline > math.MaxInt64/halfUnits {
			return MutationAttemptPlan{}, fmt.Errorf("%w: derived deadline exceeds time.Duration", ErrInvalidMutationAttemptPlan)
		}
		deadline = time.Duration(wholeBaseline * halfUnits)
		if input.BaselineDuration%2 != 0 {
			remainder := time.Duration(halfUnits / 2)
			if deadline > time.Duration(math.MaxInt64)-remainder {
				return MutationAttemptPlan{}, fmt.Errorf("%w: derived deadline exceeds time.Duration", ErrInvalidMutationAttemptPlan)
			}
			deadline += remainder
		}
		if deadline < 20*time.Second {
			deadline = 20 * time.Second
		}
	}

	return MutationAttemptPlan{deadline: deadline, profile: input.Profile}, nil
}

// Deadline returns the full resolved command deadline.
func (plan MutationAttemptPlan) Deadline() time.Duration { return plan.deadline }

// Profile returns the originating primary's cooperative execution profile.
func (plan MutationAttemptPlan) Profile() Profile { return plan.profile }

// ConfirmationSpec derives one confirmation from its primary while replacing
// only the attempt identity and fresh snapshot-derived workspace.
func (plan MutationAttemptPlan) ConfirmationSpec(primary Spec, attempt, workspace string) (Spec, error) {
	if err := primary.validate(); err != nil {
		return Spec{}, fmt.Errorf("%w: primary spec: %v", ErrInvalidMutationAttemptPlan, err)
	}
	switch {
	case primary.Profile != plan.profile:
		return Spec{}, fmt.Errorf("%w: primary profile differs from plan", ErrInvalidMutationAttemptPlan)
	case primary.Deadline != plan.deadline:
		return Spec{}, fmt.Errorf("%w: primary deadline differs from plan", ErrInvalidMutationAttemptPlan)
	case attempt == "" || attempt == primary.Attempt:
		return Spec{}, fmt.Errorf("%w: confirmation identity is not fresh", ErrInvalidMutationAttemptPlan)
	case workspace == "" || workspace == primary.Dir:
		return Spec{}, fmt.Errorf("%w: confirmation workspace is not fresh", ErrInvalidMutationAttemptPlan)
	}
	confirmation := primary.snapshot()
	confirmation.Attempt = attempt
	confirmation.Dir = workspace

	return confirmation, nil
}

// MutationOutcome is an attributable scored result for one mutant.
type MutationOutcome uint8

const (
	MutationSurvived MutationOutcome = iota + 1
	MutationKilled
	MutationTimedOut
	MutationRunaway
)

// MutationDisposition is the policy result for one drained observation.
type MutationDisposition interface{ mutationDisposition() }

// AttributableMutation retains the observation or observations supporting one outcome.
type AttributableMutation struct {
	Outcome           MutationOutcome
	Primary           Terminal
	Confirmation      Terminal
	PressureValidated bool
}

func (AttributableMutation) mutationDisposition() {}

// MutationNeedsConfirmation retains one overlap-ambiguous primary deadline.
type MutationNeedsConfirmation struct{ Primary Tripped }

func (MutationNeedsConfirmation) mutationDisposition() {}

// MutationAborted retains infrastructure uncertainty that cannot be scored.
type MutationAborted struct {
	Primary      Terminal
	Confirmation Terminal
}

func (MutationAborted) mutationDisposition() {}

// MutationFatalUncertainty retains drainage uncertainty for runtime-fatal handling.
type MutationFatalUncertainty struct {
	Primary      Terminal
	Confirmation Terminal
}

func (MutationFatalUncertainty) mutationDisposition() {}

// ClassifyPrimaryMutation maps one drained primary observation into mutation policy.
func ClassifyPrimaryMutation(primary Terminal, overlapAmbiguous bool) MutationDisposition {
	switch terminal := primary.(type) {
	case Settled:
		outcome := MutationKilled
		if terminal.Exit.Passed() {
			outcome = MutationSurvived
		}

		return AttributableMutation{Outcome: outcome, Primary: primary}
	case Tripped:
		switch terminal.Trip.(type) {
		case AutomaticDeadlineTrip, SerialDeadlineTrip:
			if overlapAmbiguous {
				return MutationNeedsConfirmation{Primary: terminal}
			}

			return AttributableMutation{Outcome: MutationTimedOut, Primary: primary}
		case FuseTrip:
			return AttributableMutation{Outcome: MutationRunaway, Primary: primary}
		default:
			panic("primary mutation trip classification is not implemented")
		}
	case Stopped, Infrastructure:
		return MutationAborted{Primary: primary}
	case DrainUnconfirmed:
		return MutationFatalUncertainty{Primary: primary}
	default:
		panic("primary mutation classification is not implemented for this terminal")
	}
}
