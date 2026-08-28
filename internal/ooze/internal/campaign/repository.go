package campaign

import "github.com/gtramontina/ooze/internal/gosourcefile"

// Repository supplies the immutable source state for a campaign.
type Repository interface {
	ListGoSourceFiles() []*gosourcefile.GoSourceFile
	MaterializeTemporaryRepository(string) TemporaryRepository
}

// TemporaryRepository is one removable repository snapshot or mutant workspace.
type TemporaryRepository interface {
	Repository
	Root() string
	Overwrite(string, []byte)
	Remove()
}

type ManagedMutationOutcome uint8

const (
	ManagedSurvived ManagedMutationOutcome = iota + 1
	ManagedKilled
	ManagedTimedOut
	ManagedRunaway
)

type ManagedOutcome uint8

const (
	ManagedNoMutants ManagedOutcome = iota + 1
	ManagedCompleted
	ManagedAborted
	ManagedCleanupUnconfirmed
	ManagedInvariantViolation
)

func presentManagedMutation(kind mutantResultKind) ManagedMutationOutcome {
	switch kind {
	case mutantSurvived:
		return ManagedSurvived
	case mutantKilled:
		return ManagedKilled
	case mutantTimedOut:
		return ManagedTimedOut
	case mutantRunaway:
		return ManagedRunaway
	default:
		panic("managed mutant outcome is invalid")
	}
}
