package ooze

import "github.com/gtramontina/ooze/internal/ooze/internal/campaign"

// ManagedProgressKind identifies one campaign-domain observation for the root package.
type ManagedProgressKind uint8

const (
	ManagedCampaignStarted ManagedProgressKind = iota + 1
	ManagedCatalogueDiscovered
	ManagedBaselineStarted
	ManagedBaselineFinished
	ManagedMutationStarted
	ManagedMutationFinished
	ManagedCampaignCompleted
	ManagedCampaignFoundNoMutants
	ManagedCampaignAborted
	ManagedCampaignCleanupUnconfirmed
	ManagedCampaignInvariantViolated
)

// ManagedProgress contains capability-free campaign-domain facts for the root package.
type ManagedProgress struct {
	Kind         ManagedProgressKind
	Total        int
	Label        string
	Passed       bool
	Confirmation bool
	Outcome      ManagedMutationOutcome
}

func presentManagedProgress(progress campaign.ManagedProgress) ManagedProgress {
	return ManagedProgress{
		Kind: ManagedProgressKind(progress.Kind), Total: progress.Total, Label: progress.Label,
		Passed: progress.Passed, Confirmation: progress.Confirmation,
		Outcome: ManagedMutationOutcome(progress.Outcome),
	}
}

func terminalManagedProgress(outcome ManagedOutcome) ManagedProgress {
	var kind ManagedProgressKind
	switch outcome {
	case ManagedCompleted:
		kind = ManagedCampaignCompleted
	case ManagedNoMutants:
		kind = ManagedCampaignFoundNoMutants
	case ManagedAborted:
		kind = ManagedCampaignAborted
	case ManagedCleanupUnconfirmed:
		kind = ManagedCampaignCleanupUnconfirmed
	case ManagedInvariantViolation:
		kind = ManagedCampaignInvariantViolated
	default:
		panic("managed terminal progress outcome is invalid")
	}

	return ManagedProgress{Kind: kind}
}
