package ooze

import (
	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

type Profile = processruntime.Profile

const (
	AutomaticProfile = processruntime.AutomaticProfile
	SerialProfile    = processruntime.SerialProfile
)

var ErrInvalidMutationAttemptPlan = campaign.ErrInvalidMutationAttemptPlan

type MutationAttemptPlanInput = campaign.MutationAttemptPlanInput
type MutationAttemptPlan = campaign.MutationAttemptPlan

// Spec fixes one mutation attempt's command and supervision policy.
type Spec = campaign.Spec

func NewMutationAttemptPlan(input MutationAttemptPlanInput) (MutationAttemptPlan, error) {
	return campaign.NewMutationAttemptPlan(input)
}
