package ooze

import internalooze "github.com/gtramontina/ooze/internal/ooze"

// Outcome identifies whether a campaign produced a score or why it did not.
type Outcome uint8

const (
	// Completed identifies a campaign that produced a mutation score.
	Completed Outcome = iota + 1
	// NoMutants identifies a campaign with an empty mutant catalogue.
	NoMutants
	// Aborted identifies a campaign stopped by attributable infrastructure evidence.
	Aborted
	// CleanupUnconfirmed identifies a campaign whose execution domains could not be proven drained.
	CleanupUnconfirmed
	// InvariantViolation identifies an invalid internal state transition.
	InvariantViolation
)

// MutationOutcome identifies the attributable outcome of one mutant.
type MutationOutcome uint8

const (
	// Survived identifies a mutant whose test command passed.
	Survived MutationOutcome = iota + 1
	// Killed identifies a mutant whose test command failed.
	Killed
	// TimedOut identifies a mutant whose command deadline fired.
	TimedOut
	// Runaway identifies a mutant whose execution domain crossed the process fuse.
	Runaway
)

// Score is the authoritative score and threshold decision for a completed campaign.
type Score struct {
	Detected int
	Total    int
	Value    float32
	Minimum  float32
	Passed   bool
}

// MutationResult contains the terminal presentation facts for one mutant.
type MutationResult struct {
	Label   string
	Outcome MutationOutcome
}

// Result contains the terminal presentation facts for one campaign.
type Result struct {
	Outcome   Outcome
	Score     *Score
	Mutations []MutationResult
}

func projectResult(managed internalooze.ManagedReleaseResult, minimum float32) Result {
	result := Result{Outcome: projectOutcome(managed.Outcome)}
	result.Mutations = make([]MutationResult, len(managed.Mutations))
	detected := 0
	for index, mutation := range managed.Mutations {
		outcome := projectMutationOutcome(mutation.Outcome)
		result.Mutations[index] = MutationResult{Label: mutation.File.Label(), Outcome: outcome}
		if outcome != Survived && outcome != 0 {
			detected++
		}
	}
	if result.Outcome == Completed {
		total := len(result.Mutations)
		value := float32(detected) / float32(total)
		result.Score = &Score{
			Detected: detected, Total: total, Value: value, Minimum: minimum, Passed: value >= minimum,
		}
	}

	return result
}

func projectOutcome(outcome internalooze.ManagedOutcome) Outcome {
	switch outcome {
	case internalooze.ManagedCompleted:
		return Completed
	case internalooze.ManagedNoMutants:
		return NoMutants
	case internalooze.ManagedAborted:
		return Aborted
	case internalooze.ManagedCleanupUnconfirmed:
		return CleanupUnconfirmed
	case internalooze.ManagedInvariantViolation:
		return InvariantViolation
	default:
		panic("managed campaign outcome is invalid")
	}
}

func projectMutationOutcome(outcome internalooze.ManagedMutationOutcome) MutationOutcome {
	switch outcome {
	case internalooze.ManagedSurvived:
		return Survived
	case internalooze.ManagedKilled:
		return Killed
	case internalooze.ManagedTimedOut:
		return TimedOut
	case internalooze.ManagedRunaway:
		return Runaway
	case 0:
		return 0
	default:
		panic("managed mutation outcome is invalid")
	}
}
