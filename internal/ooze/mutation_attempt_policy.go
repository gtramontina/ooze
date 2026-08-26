package ooze

import (
	"errors"
	"fmt"
	"math"
	"time"
)

var ErrInvalidMutationAttemptPlan = errors.New("invalid mutation attempt plan")

type MutationAttemptPlanInput struct {
	BaselineDuration time.Duration
	Peers            int
	Override         time.Duration
	Profile          Profile
}

type MutationAttemptPlan struct {
	deadline time.Duration
	profile  Profile
}

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

func (plan MutationAttemptPlan) Deadline() time.Duration { return plan.deadline }

func (plan MutationAttemptPlan) Profile() Profile { return plan.profile }

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

type MutationOutcome uint8

const (
	MutationSurvived MutationOutcome = iota + 1
	MutationKilled
	MutationTimedOut
	MutationRunaway
)

type MutationDisposition interface{ mutationDisposition() }

type AttributableMutation struct {
	Outcome           MutationOutcome
	Primary           Terminal
	Confirmation      Terminal
	PressureValidated bool
}

func (AttributableMutation) mutationDisposition() {}

type MutationNeedsConfirmation struct{ primary Tripped }

func (MutationNeedsConfirmation) mutationDisposition() {}

func (provisional MutationNeedsConfirmation) Primary() Tripped { return provisional.primary }

type MutationAborted struct {
	Primary      Terminal
	Confirmation Terminal
}

func (MutationAborted) mutationDisposition() {}

type MutationFatalUncertainty struct {
	Primary      Terminal
	Confirmation Terminal
}

func (MutationFatalUncertainty) mutationDisposition() {}

func ClassifyPrimaryMutation(primary Terminal) MutationDisposition {
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
			if terminal.confirmationProvisional {
				return MutationNeedsConfirmation{primary: terminal}
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

func ClassifyMutationConfirmation(
	provisional MutationNeedsConfirmation,
	confirmation Terminal,
) MutationDisposition {
	confirmationData := terminalExecutionData(confirmation)
	if confirmationData.Deadline != provisional.primary.Deadline ||
		confirmationData.profile != provisional.primary.profile {
		return MutationAborted{Primary: provisional.primary, Confirmation: confirmation}
	}
	switch terminal := confirmation.(type) {
	case Settled:
		outcome := MutationKilled
		if terminal.Exit.Passed() {
			outcome = MutationSurvived
		}

		return AttributableMutation{
			Outcome: outcome, Primary: provisional.primary,
			Confirmation: confirmation, PressureValidated: true,
		}
	case Tripped:
		switch terminal.Trip.(type) {
		case AutomaticDeadlineTrip, SerialDeadlineTrip:
			return AttributableMutation{
				Outcome: MutationTimedOut, Primary: provisional.primary,
				Confirmation: confirmation,
			}
		case FuseTrip:
			return AttributableMutation{
				Outcome: MutationRunaway, Primary: provisional.primary,
				Confirmation: confirmation,
			}
		default:
			panic("mutation confirmation trip classification is not implemented")
		}
	case Stopped, Infrastructure:
		return MutationAborted{Primary: provisional.primary, Confirmation: confirmation}
	case DrainUnconfirmed:
		return MutationFatalUncertainty{Primary: provisional.primary, Confirmation: confirmation}
	default:
		panic("mutation confirmation classification is not implemented for this terminal")
	}
}

func terminalExecutionData(terminal Terminal) ExecutionData {
	switch terminal := terminal.(type) {
	case Settled:
		return terminal.ExecutionData
	case Tripped:
		return terminal.ExecutionData
	case Stopped:
		return terminal.ExecutionData
	case Infrastructure:
		return terminal.ExecutionData
	case DrainUnconfirmed:
		return terminal.ExecutionData
	default:
		panic("mutation terminal has no execution evidence")
	}
}
