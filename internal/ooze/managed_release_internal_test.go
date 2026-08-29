package ooze

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedReleaseNormalizesSupervisionInvariantAndSettlesFatalRuntime(t *testing.T) {
	runtime := processruntime.New(1)
	executor := campaign.NewExecutorWithSystem(runtime, supervisionViolationSystem{runtime: runtime})
	process := managedProcess{capacity: 1, runtime: runtime, executor: executor}

	result := process.release(ManagedReleaseConfiguration{
		Lineage: 1, Repository: invariantRepository{}, TemporaryDir: &invariantTemporaryDirectory{},
		Command: []string{"test"}, Profile: AutomaticProfile,
		Viruses: []viruses.Virus{integerincrement.New()},
		Observe: func(ManagedProgress) { panic("observer failed") },
	})

	require.Equal(t, ManagedInvariantViolation, result.Outcome)
	require.NotNil(t, result.Invariant)
	assert.Equal(t, "reduce supervisor", result.Invariant.Operation)
	assert.Equal(t, "prospective registration is incomplete or duplicated", result.Invariant.Reason)
	assert.NotZero(t, runtime.FatalEpoch())
	assert.False(t, runtime.EmergencySettlementRequired())
}

func TestManagedTerminalProgressObserverPanicDoesNotReplaceResult(t *testing.T) {
	for _, test := range []struct {
		name    string
		outcome ManagedOutcome
	}{
		{name: "cleanup unconfirmed", outcome: ManagedCleanupUnconfirmed},
		{name: "invariant violation", outcome: ManagedInvariantViolation},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed := false
			result := func() (result ManagedReleaseResult) {
				result.Outcome = test.outcome
				defer func() {
					observeManagedProgress(func(ManagedProgress) {
						observed = true
						panic("observer failed")
					}, terminalManagedProgress(test.outcome))
				}()

				return result
			}()

			assert.Equal(t, test.outcome, result.Outcome)
			assert.True(t, observed)
		})
	}
}

type supervisionViolationSystem struct{ runtime *processruntime.Runtime }

func (system supervisionViolationSystem) ReserveLaunch(*processruntime.StartCell, supervision.Spec) {
	_, _ = supervision.NewMachine().Apply(
		supervision.CorrelatedMalformedFact(supervision.ProspectiveRegisteredFact, 0),
	)
}

func (supervisionViolationSystem) DiscardLaunch(*processruntime.StartCell) {
	panic("unexpected discarded launch")
}

func (supervisionViolationSystem) Launch(processruntime.PreparedStart, supervision.Spec) supervision.ObservedLaunch {
	panic("unexpected launch")
}

func (supervisionViolationSystem) Wait(supervision.Generation, *supervision.OwnedAttempt) supervision.ObservedTerminal {
	panic("unexpected wait")
}

func (supervisionViolationSystem) Stop(*supervision.OwnedAttempt) { panic("unexpected stop") }

func (system supervisionViolationSystem) EmergencyDrain(supervision.EmergencyRequest) (
	supervision.SweepResult,
	processruntime.EmergencySettlement,
) {
	return supervision.SweepDrained{}, system.runtime.SettleEmergency(nil)
}

type invariantRepository struct{}

func (invariantRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}
}

func (invariantRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	return invariantTemporaryRepository{path: path}
}

type invariantTemporaryRepository struct{ path string }

func (repository invariantTemporaryRepository) Root() string { return repository.path }
func (invariantTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return invariantRepository{}.ListGoSourceFiles()
}
func (invariantTemporaryRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	return invariantTemporaryRepository{path: path}
}
func (invariantTemporaryRepository) Overwrite(string, []byte) {}
func (invariantTemporaryRepository) Remove()                  {}

type invariantTemporaryDirectory struct{ next int }

func (directory *invariantTemporaryDirectory) New() string {
	directory.next++
	return "workspace"
}

func TestManagedCleanupFailureRetainsOrderedResidualEvidence(t *testing.T) {
	result := presentManagedRelease(campaign.Result{
		Outcome: campaign.ManagedCleanupUnconfirmed,
		Residual: []campaign.ResidualCustody{
			{Attempt: "campaign-1:2", Generation: 7, Transferred: true},
			{Attempt: "campaign-1:3", Generation: 9, Prospective: true},
		},
	})

	assert.Equal(t, ManagedCleanupUnconfirmed, result.Outcome)
	assert.Equal(t, []ManagedResidualCustody{
		{Attempt: "campaign-1:2", Generation: 7, Stage: ManagedResidualOwned, Transferred: true},
		{Attempt: "campaign-1:3", Generation: 9, Stage: ManagedResidualProspective},
	}, result.Residual)
}

func TestManagedAbortCauseMappingIsExhaustive(t *testing.T) {
	tests := []struct {
		name string
		from campaign.AbortCause
		to   ManagedAbortCause
	}{
		{"campaign registration", campaign.AbortCampaignRegistration, ManagedAbortCampaignRegistration},
		{"snapshot materialization", campaign.AbortSnapshotMaterialization, ManagedAbortSnapshotMaterialization},
		{"catalogue discovery", campaign.AbortCatalogueDiscovery, ManagedAbortCatalogueDiscovery},
		{"workspace materialization", campaign.AbortWorkspaceMaterialization, ManagedAbortWorkspaceMaterialization},
		{"admission rejection", campaign.AbortAdmissionRejected, ManagedAbortAdmissionRejected},
		{"fatal epoch", campaign.AbortFatalEpoch, ManagedAbortFatalEpoch},
		{"workspace cleanup", campaign.AbortWorkspaceCleanup, ManagedAbortWorkspaceCleanup},
		{"snapshot cleanup", campaign.AbortSnapshotCleanup, ManagedAbortSnapshotCleanup},
		{"attempt not released", campaign.AbortAttemptNotReleased, ManagedAbortAttemptNotReleased},
		{"prospective launch", campaign.AbortProspectiveLaunch, ManagedAbortProspectiveLaunch},
		{"drainage", campaign.AbortDrainageUnconfirmed, ManagedAbortDrainageUnconfirmed},
		{"baseline failure", campaign.AbortBaselineFailed, ManagedAbortBaselineFailed},
		{"baseline deadline", campaign.AbortBaselineDeadline, ManagedAbortBaselineDeadline},
		{"baseline fuse", campaign.AbortBaselineFuse, ManagedAbortBaselineFuse},
		{"baseline stopped", campaign.AbortBaselineStopped, ManagedAbortBaselineStopped},
		{"baseline infrastructure", campaign.AbortBaselineInfrastructure, ManagedAbortBaselineInfrastructure},
		{"primary stopped", campaign.AbortPrimaryStopped, ManagedAbortPrimaryStopped},
		{"primary infrastructure", campaign.AbortPrimaryInfrastructure, ManagedAbortPrimaryInfrastructure},
		{"confirmation infrastructure", campaign.AbortConfirmationInfrastructure, ManagedAbortConfirmationInfrastructure},
		{"process emergency", campaign.AbortProcessEmergency, ManagedAbortProcessEmergency},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.to, presentManagedAbortCause(test.from))
		})
	}

	assert.Panics(t, func() { presentManagedAbortCause(0) })
}
