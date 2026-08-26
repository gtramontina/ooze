package ooze

import internalooze "github.com/gtramontina/ooze/internal/ooze"

type verboseObserver struct {
	logger internalooze.Logger
}

func (observer verboseObserver) Observe(event CampaignEvent) error {
	switch event := event.(type) {
	case CampaignStarted:
		observer.logger.Logf("campaign started")
	case CatalogueDiscovered:
		noun := "mutants"
		if event.Total == 1 {
			noun = "mutant"
		}
		observer.logger.Logf("discovered %d %s", event.Total, noun)
	case BaselineStarted:
		observer.logger.Logf("baseline started")
	case BaselineFinished:
		outcome := "failed"
		if event.Passed {
			outcome = "passed"
		}
		observer.logger.Logf("baseline %s", outcome)
	case MutationStarted:
		kind := "mutation"
		if event.Confirmation {
			kind = "confirmation"
		}
		observer.logger.Logf("%s started: %s", kind, event.Mutation.Label)
	case MutationFinished:
		observer.logger.Logf("mutation %s: %s", mutationOutcomeText(event.Outcome), event.Mutation.Label)
	case CampaignCompleted:
		observer.logger.Logf("campaign completed")
	case CampaignFoundNoMutants:
		observer.logger.Logf("campaign found no mutants")
	case CampaignAborted:
		observer.logger.Logf("campaign aborted")
	case CampaignCleanupUnconfirmed:
		observer.logger.Logf("campaign cleanup unconfirmed")
	case CampaignInvariantViolated:
		observer.logger.Logf("campaign invariant violated")
	}

	return nil
}

func mutationOutcomeText(outcome MutationOutcome) string {
	switch outcome {
	case Survived:
		return "survived"
	case Killed:
		return "killed"
	case TimedOut:
		return "timed out"
	case Runaway:
		return "runaway"
	default:
		panic("mutation outcome is invalid")
	}
}
