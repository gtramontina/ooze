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

func presentManagedProgress(event campaign.Event) (ManagedProgress, bool) {
	switch event.Kind() {
	case campaign.CampaignRegisteredEvent:
		return ManagedProgress{Kind: ManagedCampaignStarted}, true
	case campaign.CatalogueDiscoveredEvent:
		total, ok := event.CatalogueSize()
		return ManagedProgress{Kind: ManagedCatalogueDiscovered, Total: total}, ok
	case campaign.AttemptLaunchedEvent:
		if !event.LaunchOwned() {
			return ManagedProgress{}, false
		}
		role, ok := event.AttemptRole()
		if !ok {
			return ManagedProgress{}, false
		}
		if role == campaign.BaselineAttempt {
			return ManagedProgress{Kind: ManagedBaselineStarted}, true
		}
		label, ok := event.MutationLabel()
		return ManagedProgress{
			Kind: ManagedMutationStarted, Label: label,
			Confirmation: role == campaign.ConfirmationAttempt,
		}, ok
	case campaign.AttemptTerminalEvent:
		if passed, ok := event.AttemptPassed(); ok {
			return ManagedProgress{Kind: ManagedBaselineFinished, Passed: passed}, true
		}
		outcome, ok := event.MutationOutcome()
		if !ok {
			return ManagedProgress{}, false
		}
		label, labelled := event.MutationLabel()
		return ManagedProgress{
			Kind: ManagedMutationFinished, Label: label,
			Outcome: ManagedMutationOutcome(outcome),
		}, labelled
	case campaign.TerminalCommittedEvent:
		outcome, ok := event.TerminalOutcome()
		if !ok {
			return ManagedProgress{}, false
		}
		switch outcome {
		case campaign.CompletedOutcome:
			return ManagedProgress{Kind: ManagedCampaignCompleted}, true
		case campaign.NoMutantsOutcome:
			return ManagedProgress{Kind: ManagedCampaignFoundNoMutants}, true
		case campaign.AbortedOutcome:
			return ManagedProgress{Kind: ManagedCampaignAborted}, true
		}
	}
	return ManagedProgress{}, false
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
