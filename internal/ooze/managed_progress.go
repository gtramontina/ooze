package ooze

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

func (runner *managedCampaignRunner) publishProgress(
	payload campaignEventPayload,
	previous campaignState,
	next campaignState,
) {
	if runner.observe == nil {
		return
	}
	switch event := payload.(type) {
	case campaignRegisteredEvent:
		runner.observe(ManagedProgress{Kind: ManagedCampaignStarted})
	case catalogueDiscoveredEvent:
		runner.observe(ManagedProgress{Kind: ManagedCatalogueDiscovered, Total: len(event.mutants)})
	case attemptLaunchEvent:
		if event.result.kind != campaignLaunchOwned {
			return
		}
		attemptAt := previous.attemptIndex(event.attempt)
		if attemptAt < 0 {
			panic("managed progress launch attempt is missing")
		}
		attempt := previous.attempts[attemptAt]
		if attempt.kind == campaignAttemptBaseline {
			runner.observe(ManagedProgress{Kind: ManagedBaselineStarted})
			return
		}
		runner.observe(ManagedProgress{
			Kind: ManagedMutationStarted, Label: runner.mutationLabel(attempt.mutant),
			Confirmation: attempt.kind == campaignAttemptConfirmation,
		})
	case attemptTerminalEvent:
		attemptAt := previous.attemptIndex(event.attempt)
		if attemptAt >= 0 && previous.attempts[attemptAt].kind == campaignAttemptBaseline {
			runner.observe(ManagedProgress{Kind: ManagedBaselineFinished, Passed: next.baselineEvidence.passed})
		}
		for _, mutant := range next.mutants {
			previousAt := previous.mutantIndex(mutant.identity)
			if mutant.result == 0 || previousAt < 0 || previous.mutants[previousAt].result != 0 {
				continue
			}
			runner.observe(ManagedProgress{
				Kind: ManagedMutationFinished, Label: runner.mutationLabel(mutant.identity),
				Outcome: presentManagedMutation(mutant.result),
			})
		}
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

func (runner *managedCampaignRunner) mutationLabel(identity mutantIdentity) string {
	mutation := runner.mutations[identity]
	if mutation == nil {
		panic("managed progress mutation is missing")
	}

	return mutation.Label()
}
