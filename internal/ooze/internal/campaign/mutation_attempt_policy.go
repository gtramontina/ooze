package campaign

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
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

type Spec struct {
	Attempt  string
	Command  []string
	Dir      string
	Env      []string
	Profile  Profile
	Deadline time.Duration
}

func (spec Spec) validate() error {
	return supervision.Spec{
		Attempt: spec.Attempt, Command: spec.Command, Dir: spec.Dir, Env: spec.Env,
		Profile: spec.Profile, Deadline: spec.Deadline,
	}.Validate()
}

func (spec Spec) snapshot() Spec {
	cloned := spec
	cloned.Command = append([]string(nil), spec.Command...)
	cloned.Env = append([]string(nil), spec.Env...)

	return cloned
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

func terminalExecutionData(terminal supervision.Terminal) supervision.ExecutionData {
	switch terminal := terminal.(type) {
	case supervision.Settled:
		return terminal.ExecutionData
	case supervision.Tripped:
		return terminal.ExecutionData
	case supervision.Stopped:
		return terminal.ExecutionData
	case supervision.Infrastructure:
		return terminal.ExecutionData
	case supervision.DrainUnconfirmed:
		return terminal.ExecutionData
	default:
		panic("mutation terminal has no execution evidence")
	}
}
