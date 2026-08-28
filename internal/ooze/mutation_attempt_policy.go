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
type Spec = campaign.Spec

func NewMutationAttemptPlan(input MutationAttemptPlanInput) (MutationAttemptPlan, error) {
	return campaign.NewMutationAttemptPlan(input)
}
