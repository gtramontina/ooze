package campaign

import "github.com/gtramontina/ooze/internal/gosourcefile"

type Repository interface {
	ListGoSourceFiles() []*gosourcefile.GoSourceFile
	MaterializeTemporaryRepository(string) TemporaryRepository
}

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

type AbortCause uint8

const (
	AbortCampaignRegistration AbortCause = iota + 1
	AbortSnapshotMaterialization
	AbortCatalogueDiscovery
	AbortWorkspaceMaterialization
	AbortAdmissionRejected
	AbortFatalEpoch
	AbortWorkspaceCleanup
	AbortSnapshotCleanup
	AbortAttemptNotReleased
	AbortProspectiveLaunch
	AbortDrainageUnconfirmed
	AbortBaselineFailed
	AbortBaselineDeadline
	AbortBaselineFuse
	AbortBaselineStopped
	AbortBaselineInfrastructure
	AbortPrimaryStopped
	AbortPrimaryInfrastructure
	AbortConfirmationInfrastructure
	AbortProcessEmergency
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
