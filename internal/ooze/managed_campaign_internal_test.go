package ooze

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedCampaignEmptyCatalogueRunsNoCommands(t *testing.T) {
	repository := &managedMemoryRepository{}
	attempts := &managedAttemptFixture{}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{},
	})

	{
		_, ok := result.outcome.(noMutantsOutcome)
		require.True(t, ok, "outcome = %#v, want NoMutants", result.outcome)
	}
	assert.EqualValues(t, 0, attempts.launches, "launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
	assert.EqualValues(t, 1, repository.materializations, "launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
	assert.True(t, repository.snapshot.removed, "launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
}

func TestManagedCampaignLazilyOverlapsAutomaticPrimariesUpToCapacity(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	started := make(chan Spec, 2)
	release := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitStarted: started, releasePrimaries: release,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	completed := make(chan managedCampaignResult, 1)
	go func() {
		completed <- runner.run(managedCampaignRequest{
			identity: "campaign-a", lineage: 11, command: []string{"test"},
			profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
		})
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			require.FailNow(t, "automatic primaries did not overlap before settlement")
		}
	}
	close(release)
	select {
	case result := <-completed:
		outcome, ok := result.outcome.(completedOutcome)
		require.True(t, ok, "outcome = %#v", result.outcome)
		assert.EqualValues(t, 2, len(outcome.mutants), "outcome = %#v", result.outcome)
	case <-time.After(time.Second):
		require.FailNow(t, "overlapped campaign did not complete")
	}
}

func TestManagedCampaignSerialPrimariesAreExclusiveWithDetectedCapacityProfile(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	started := make(chan Spec, 2)
	release := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitStarted: started, releasePrimaries: release,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(3), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	completed := make(chan managedCampaignResult, 1)
	go func() {
		completed <- runner.run(managedCampaignRequest{
			identity: "campaign-a", lineage: 11, command: []string{"test"},
			profile: SerialProfile, peers: 3, viruses: []viruses.Virus{integerincrement.New()},
		})
	}()

	first := <-started
	assert.True(t, slices.Contains(first.Env, "GOMAXPROCS=3"), "serial environment = %#v", first.Env)
	select {
	case second := <-started:
		close(release)
		require.FailNowf(t, "serial primary overlapped", "second primary: %#v", second)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case result := <-completed:
		{
			outcome, ok := result.outcome.(completedOutcome)
			require.True(t, ok, "outcome = %#v", result.outcome)
			assert.EqualValues(t, 2, len(outcome.mutants), "outcome = %#v", result.outcome)
		}
	case <-time.After(time.Second):
		require.FailNow(t, "serial campaign did not complete")
	}
}

func TestManagedCampaignRunsBaselineBeforeOneAutomaticPrimary(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: 2 * time.Second}},
		Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
	})

	completed, ok := result.outcome.(completedOutcome)
	require.True(t, ok, "outcome = %#v, want one killed mutant", result.outcome)
	require.Len(t, completed.mutants, 1, "outcome = %#v, want one killed mutant", result.outcome)
	assert.Equal(t, mutantKilled, completed.mutants[0].kind, "outcome = %#v, want one killed mutant", result.outcome)
	assert.EqualValues(t, 2, attempts.launches, "launches/specs = %d/%#v", attempts.launches, attempts.specs)
	assert.EqualValues(t, 2, len(attempts.specs), "launches/specs = %d/%#v", attempts.launches, attempts.specs)
	for _, spec := range attempts.specs {
		assert.True(t, slices.Contains(spec.Env, "GOMAXPROCS=1"), "automatic spec environment = %#v", spec.Env)
	}
}

func TestManagedCampaignPropagatesAbsoluteTimeoutAndRetainsTimedOutResult(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		Tripped{Trip: AutomaticDeadlineTrip{}},
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, mutationTimeout: 37 * time.Millisecond,
		viruses: []viruses.Virus{integerincrement.New()},
	})

	completed, ok := result.outcome.(completedOutcome)
	require.True(t, ok, "outcome = %#v, want one timed-out mutant", result.outcome)
	require.Len(t, completed.mutants, 1, "outcome = %#v, want one timed-out mutant", result.outcome)
	assert.Equal(t, mutantTimedOut, completed.mutants[0].kind, "outcome = %#v, want one timed-out mutant", result.outcome)
	require.Len(t, attempts.specs, 2, "attempt specs = %#v, want exact 37ms primary deadline", attempts.specs)
	assert.Equal(t, 37*time.Millisecond, attempts.specs[1].Deadline, "attempt specs = %#v, want exact 37ms primary deadline", attempts.specs)
}

func TestManagedCampaignConfirmsOverlapDeadlineAndTransitionsFutureAdmission(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	launchesReady := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitForLaunches: 3, launchesReady: launchesReady,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Tripped{Trip: AutomaticDeadlineTrip{}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	shell := newProcessRuntimeShell(2)
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, mutationTimeout: time.Minute,
		viruses: []viruses.Virus{integerincrement.New()},
	})

	completed, ok := result.outcome.(completedOutcome)
	require.True(t, ok, "outcome = %#v, want two killed mutants after confirmation", result.outcome)
	require.Len(t, completed.mutants, 2, "outcome = %#v, want two killed mutants after confirmation", result.outcome)
	assert.Equal(t, mutantKilled, completed.mutants[0].kind, "outcome = %#v, want two killed mutants after confirmation", result.outcome)
	assert.Equal(t, mutantKilled, completed.mutants[1].kind, "outcome = %#v, want two killed mutants after confirmation", result.outcome)
	assert.True(t, completed.singleAdmissionFallback, "outcome = %#v, want two killed mutants after confirmation", result.outcome)
	assert.Equal(t, campaignEvidenceSettled, completed.mutants[0].confirmation.kind, "outcome = %#v, want two killed mutants after confirmation", result.outcome)
	assert.EqualValues(t, 4, attempts.launches, "launches/mode = %d/%v, want one confirmation and single admission", attempts.launches, shell.snapshot().mode)
	assert.Equal(t, singleAdmission, shell.snapshot().mode, "launches/mode = %d/%v, want one confirmation and single admission", attempts.launches, shell.snapshot().mode)
}

func TestManagedCampaignResumesWithSingleAdmissionAfterConfirmationPressure(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\nvar third = 0\nvar fourth = 0\n")),
	}}
	deadlineTrip := Tripped{Trip: AutomaticDeadlineTrip{}}
	killed := Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}}
	attempts := &managedAttemptFixture{
		waitForLaunches: 3,
		launchesReady:   make(chan struct{}),
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			deadlineTrip, killed,
			killed,
			killed, killed,
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	completed := make(chan managedCampaignResult, 1)
	go func() {
		completed <- runner.run(managedCampaignRequest{
			identity: "campaign-a", lineage: 11, command: []string{"test"},
			profile: AutomaticProfile, peers: 2, mutationTimeout: time.Minute,
			viruses: []viruses.Virus{integerincrement.New()},
		})
	}()

	select {
	case result := <-completed:
		outcome, ok := result.outcome.(completedOutcome)
		require.True(t, ok, "outcome = %#v", result.outcome)
		assert.Len(t, outcome.mutants, 4)
		assert.True(t, outcome.singleAdmissionFallback)
		assert.EqualValues(t, 6, attempts.launches)
	case <-time.After(time.Second):
		require.FailNow(t, "campaign did not resume with single admission after confirmation pressure")
	}
}

func TestManagedCampaignAbortsResourceExhaustionAndTransitionsFutureAdmission(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{
		notReleasedAt: 2,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	shell := newProcessRuntimeShell(2)
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
	})

	{
		_, ok := result.outcome.(abortedOutcome)
		require.True(t, ok, "outcome/mode/launches = %#v/%v/%d", result.outcome, shell.snapshot().mode, attempts.launches)
		assert.Equal(t, singleAdmission, shell.snapshot().mode, "outcome/mode/launches = %#v/%v/%d", result.outcome, shell.snapshot().mode, attempts.launches)
		assert.EqualValues(t, 2, attempts.launches, "outcome/mode/launches = %#v/%v/%d", result.outcome, shell.snapshot().mode, attempts.launches)
	}
}

func TestManagedCampaignStopsOwnedPeerAndWaitsForSettlementBeforeAbort(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	attempts := &managedAttemptFixture{
		stopRelease: make(chan struct{}),
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Infrastructure{Cause: CensusFailed, Err: errors.New("census failed")},
			Stopped{},
		},
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(2), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 2, viruses: []viruses.Virus{integerincrement.New()},
	})

	{
		_, ok := result.outcome.(abortedOutcome)
		require.True(t, ok, "outcome/stops = %#v/%d, want aborted after one peer stop", result.outcome, attempts.stops)
		assert.EqualValues(t, 1, attempts.stops, "outcome/stops = %#v/%d, want aborted after one peer stop", result.outcome, attempts.stops)
	}
}

func TestManagedCampaignSettlesRuntimeEmergencyBeforeCleanupFailure(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		DrainUnconfirmed{
			Residual: OwnedUndrained,
			ExecutionData: ExecutionData{
				CommandDuration: time.Second,
				Output:          OutputSnapshot{Bytes: "private", Cutoff: 7, CompleteThroughCutoff: true},
				Failures:        FailureDiagnostics{DrainCensus: "census failed"},
			},
		},
	}}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	fault, ok := result.failure.(cleanupUnconfirmedFault)
	require.True(t, ok, "failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
	require.Len(t, fault.attempts, 1, "failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
	assert.EqualValues(t, "campaign-a:2", fault.attempts[0].attempt, "failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
	assert.EqualValues(t, 7, fault.attempts[0].evidence.output.Cutoff, "failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
	assert.EqualValues(t, "census failed", fault.attempts[0].evidence.failures.DrainCensus, "failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
	assert.EqualValues(t, 1, attempts.emergencies, "failure/emergencies = %#v/%d, want cleanup failure after one emergency", result.failure, attempts.emergencies)
}

func TestManagedCampaignAuthorizesForcedAbortAfterEmptyEmergencySweep(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{shell: shell, emergencyEmpty: true}
	started := make(chan struct{})
	release := make(chan struct{})
	temporaryDirectory := &blockingManagedTemporaryDirectory{
		managedTemporaryDirectory: managedTemporaryDirectory{}, started: started, release: release,
	}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository, temporaryDirectory: temporaryDirectory, attempts: attempts,
	})
	go func() {
		<-started
		shell.closeRuntime("peer fatal without custody")
		close(release)
	}()

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	{
		_, ok := result.outcome.(abortedOutcome)
		require.True(t, ok, "outcome/failure = %#v/%#v, want forced Aborted", result.outcome, result.failure)
		assert.Nil(t, result.failure, "outcome/failure = %#v/%#v, want forced Aborted", result.outcome, result.failure)
	}
}

func TestManagedCampaignNormalizesSnapshotBoundaryPanic(t *testing.T) {
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: managedPanickingRepository{},
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: &managedAttemptFixture{},
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	{
		outcome, ok := result.outcome.(abortedOutcome)
		require.True(t, ok, "boundary outcome = %#v, want typed snapshot abort", result.outcome)
		assert.EqualValues(t, "repository snapshot could not be materialized", outcome.cause, "boundary outcome = %#v, want typed snapshot abort", result.outcome)
		assert.NotContains(t, outcome.cause, "/private/repository", "boundary outcome = %#v, want typed snapshot abort", result.outcome)
	}
}

func TestManagedCampaignCleansWorkspaceAcquiredBeforeMutationPanic(t *testing.T) {
	repository := &managedPartialWorkspaceRepository{}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: &managedAttemptFixture{
			terminals: []Terminal{Settled{
				Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second},
			}},
		},
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	{
		_, ok := result.outcome.(abortedOutcome)
		require.True(t, ok, "outcome/workspace = %#v/%#v, want aborted with acquired workspace removed", result.outcome, repository.workspace)
		require.NotNil(t, repository.workspace, "outcome/workspace = %#v/%#v, want aborted with acquired workspace removed", result.outcome, repository.workspace)
		assert.True(t, repository.workspace.removed, "outcome/workspace = %#v/%#v, want aborted with acquired workspace removed", result.outcome, repository.workspace)
	}
}

func TestManagedCampaignReportsOnlyStructuredResidueWhenFailedWorkspaceCannotBeCleaned(t *testing.T) {
	repository := &managedPartialWorkspaceRepository{failWorkspaceCleanup: true}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: newProcessRuntimeShell(1), repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: &managedAttemptFixture{
			terminals: []Terminal{Settled{
				Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second},
			}},
		},
	})

	result := runner.run(managedCampaignRequest{
		identity: "campaign-a", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	outcome, ok := result.outcome.(abortedOutcome)
	require.True(t, ok, "outcome = %#v, want stable cause plus structured residue", result.outcome)
	assert.NotContains(t, outcome.cause, "/private/workspace", "outcome = %#v, want stable cause plus structured residue", result.outcome)
	require.Len(t, outcome.artifactResidue, 1, "outcome = %#v, want stable cause plus structured residue", result.outcome)
	assert.True(t, strings.HasPrefix(outcome.artifactResidue[0], "temporary-"), "outcome = %#v, want stable cause plus structured residue", result.outcome)
}

func TestManagedCampaignConsumesFatalEpochWhileWaitingForAdmission(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	heldCampaign := shell.registerCampaign(campaignProvenance{lineage: 99})
	held := shell.requestAdmission(admissionRequest{
		campaign: heldCampaign.token, attempt: "held", class: exclusiveAdmission,
		profile: AutomaticProfile, deadline: baselineBootstrapDeadline,
	})
	heldGrant := <-held.delivery
	prepared := shell.startCommitted(heldGrant, startInstallation{grant: heldGrant, cell: &pendingStartCell{}})
	prepared.start.launch(func(attemptGeneration) attemptObservation { return launchOwned{} })
	shell.observeAttempt(prepared.result.generation, launchOwned{})

	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{shell: shell, emergencyGeneration: prepared.result.generation}
	runner := newManagedCampaignRunner(managedCampaignConstruction{
		runtime: shell, repository: repository,
		temporaryDirectory: &managedTemporaryDirectory{}, attempts: attempts,
	})
	go func() {
		deadline := time.Now().Add(time.Second)
		for len(shell.snapshot().admissions) < 2 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		shell.observeAttempt(prepared.result.generation, drainUnconfirmed{})
	}()

	result := runner.run(managedCampaignRequest{
		identity: "campaign-waiting", lineage: 11, command: []string{"test"},
		profile: AutomaticProfile, peers: 1, viruses: []viruses.Virus{integerincrement.New()},
	})

	{
		_, ok := result.outcome.(abortedOutcome)
		require.True(t, ok, "outcome/failure/emergencies = %#v/%#v/%d, want non-owner forced abort", result.outcome, result.failure, attempts.emergencies)
		assert.Nil(t, result.failure, "outcome/failure/emergencies = %#v/%#v/%d, want non-owner forced abort", result.outcome, result.failure, attempts.emergencies)
		assert.EqualValues(t, 1, attempts.emergencies, "outcome/failure/emergencies = %#v/%#v/%d, want non-owner forced abort", result.outcome, result.failure, attempts.emergencies)
	}
}

type managedMemoryRepository struct {
	files            []*gosourcefile.GoSourceFile
	materializations int
	snapshot         *managedMemoryTemporaryRepository
}

type managedPanickingRepository struct{}

func (managedPanickingRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }
func (managedPanickingRepository) MaterializeTemporaryRepository(string) TemporaryRepository {
	panic("snapshot exploded at /private/repository")
}

type managedPartialWorkspaceRepository struct {
	snapshot             *managedPartialSnapshot
	workspace            *managedPartialWorkspace
	failWorkspaceCleanup bool
	workspaceCount       int
}

func (r *managedPartialWorkspaceRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}
}
func (r *managedPartialWorkspaceRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	r.snapshot = &managedPartialSnapshot{owner: r, root: path}
	return r.snapshot
}

type managedPartialSnapshot struct {
	owner *managedPartialWorkspaceRepository
	root  string
}

func (r *managedPartialSnapshot) Root() string { return r.root }
func (r *managedPartialSnapshot) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return r.owner.ListGoSourceFiles()
}
func (r *managedPartialSnapshot) MaterializeTemporaryRepository(path string) TemporaryRepository {
	r.owner.workspaceCount++
	r.owner.workspace = &managedPartialWorkspace{
		root: path, failRemove: r.owner.failWorkspaceCleanup && r.owner.workspaceCount > 1,
	}
	return r.owner.workspace
}
func (*managedPartialSnapshot) Overwrite(string, []byte) {}
func (*managedPartialSnapshot) Remove()                  {}

type managedPartialWorkspace struct {
	root       string
	removed    bool
	failRemove bool
}

func (r *managedPartialWorkspace) Root() string                                  { return r.root }
func (*managedPartialWorkspace) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }
func (*managedPartialWorkspace) MaterializeTemporaryRepository(string) TemporaryRepository {
	panic("nested workspace is invalid")
}
func (*managedPartialWorkspace) Overwrite(string, []byte) { panic("mutation write exploded") }
func (r *managedPartialWorkspace) Remove() {
	if r.failRemove {
		panic("cleanup exploded at /private/workspace")
	}
	r.removed = true
}

func (r *managedMemoryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return append([]*gosourcefile.GoSourceFile(nil), r.files...)
}

func (r *managedMemoryRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	r.materializations++
	r.snapshot = &managedMemoryTemporaryRepository{root: path, files: r.ListGoSourceFiles()}

	return r.snapshot
}

type managedMemoryTemporaryRepository struct {
	root    string
	files   []*gosourcefile.GoSourceFile
	removed bool
}

func (r *managedMemoryTemporaryRepository) Root() string { return r.root }
func (r *managedMemoryTemporaryRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile {
	return append([]*gosourcefile.GoSourceFile(nil), r.files...)
}
func (r *managedMemoryTemporaryRepository) MaterializeTemporaryRepository(path string) TemporaryRepository {
	return &managedMemoryTemporaryRepository{root: path, files: r.ListGoSourceFiles()}
}
func (r *managedMemoryTemporaryRepository) Overwrite(string, []byte) {}
func (r *managedMemoryTemporaryRepository) Remove()                  { r.removed = true }

type managedTemporaryDirectory struct{ next int }

func (d *managedTemporaryDirectory) New() string {
	d.next++

	return "temporary-" + time.Unix(int64(d.next), 0).Format("150405")
}

type blockingManagedTemporaryDirectory struct {
	managedTemporaryDirectory
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *blockingManagedTemporaryDirectory) New() string {
	d.once.Do(func() {
		close(d.started)
		<-d.release
	})

	return d.managedTemporaryDirectory.New()
}

type managedAttemptFixture struct {
	mutex               sync.Mutex
	launches            int
	specs               []Spec
	terminals           []Terminal
	shell               *processRuntimeShell
	byGeneration        map[attemptGeneration]Spec
	terminalByGen       map[attemptGeneration]Terminal
	waitStarted         chan Spec
	releasePrimaries    <-chan struct{}
	waitAll             <-chan struct{}
	launchStarted       chan Spec
	waitForLaunches     int
	launchesReady       chan struct{}
	launchesReadyOnce   sync.Once
	notReleasedAt       int
	stopRelease         chan struct{}
	stops               int
	emergencies         int
	drainGeneration     attemptGeneration
	emergencyGeneration attemptGeneration
	emergencyEmpty      bool
}

func (f *managedAttemptFixture) reserveLaunch(*pendingStartCell, Spec) {}

func (f *managedAttemptFixture) discardLaunch(*pendingStartCell) {}

func (f *managedAttemptFixture) launch(start installedStart, spec Spec) managedObservedLaunch {
	f.mutex.Lock()
	f.launches++
	f.specs = append(f.specs, spec)
	if f.shell == nil {
		f.shell = start.shell
	} else if f.shell != start.shell {
		panic("managed attempt fixture changed process runtime")
	}
	if f.byGeneration == nil {
		f.byGeneration = make(map[attemptGeneration]Spec)
		f.terminalByGen = make(map[attemptGeneration]Terminal)
	}
	f.byGeneration[start.generation] = spec
	if f.launches <= len(f.terminals) {
		f.terminalByGen[start.generation] = f.terminals[f.launches-1]
	}
	if f.waitForLaunches != 0 && f.launches >= f.waitForLaunches {
		f.launchesReadyOnce.Do(func() { close(f.launchesReady) })
	}
	f.mutex.Unlock()
	if f.launchStarted != nil {
		f.launchStarted <- spec
	}
	var result LaunchResult
	if f.notReleasedAt == f.launches {
		observed := start.launch(func(attemptGeneration) attemptObservation {
			result = NotReleased{Kind: LaunchResourceExhausted}

			return launchNotReleased{reason: launchResourceExhausted}
		})
		receipt := start.shell.observeAttempt(start.generation, observed)

		return managedObservedLaunch{result: result, receipt: receipt}
	}
	observed := start.launch(func(attemptGeneration) attemptObservation {
		result = Owned{Attempt: newOwnedAttempt(func(StopRequest) {}, func() Terminal { return nil })}

		return launchOwned{}
	})
	receipt := start.shell.observeAttempt(start.generation, observed)

	return managedObservedLaunch{result: result, receipt: receipt}
}
func (f *managedAttemptFixture) wait(generation attemptGeneration, _ *OwnedAttempt) managedObservedTerminal {
	f.mutex.Lock()
	terminal := f.terminalByGen[generation]
	spec := f.byGeneration[generation]
	f.mutex.Unlock()
	if f.waitAll != nil {
		<-f.waitAll
	}
	if f.waitForLaunches != 0 && spec.Deadline != baselineBootstrapDeadline {
		<-f.launchesReady
	}
	if f.waitStarted != nil && spec.Deadline != baselineBootstrapDeadline {
		f.waitStarted <- spec
		<-f.releasePrimaries
	}
	data := terminalExecutionData(terminal)
	data.Deadline = spec.Deadline
	data.profile = spec.Profile
	switch terminal := terminal.(type) {
	case Settled:
		terminal.ExecutionData = data
		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, attemptSettled{profile: spec.Profile, deadline: spec.Deadline}),
		}
	case Infrastructure:
		terminal.ExecutionData = data

		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, attemptInfrastructure{cause: terminal.Err.Error()}),
		}
	case Tripped:
		terminal.ExecutionData = data
		terminal.BoundFired = CommandDeadlineFired

		return managedObservedTerminal{
			terminal: terminal,
			receipt: f.shell.observeAttempt(generation, attemptTripped{
				kind: deadlineTrip, profile: spec.Profile, deadline: spec.Deadline,
			}),
		}
	case Stopped:
		if f.stopRelease != nil {
			<-f.stopRelease
		}
		terminal.ExecutionData = data

		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, attemptStopped{}),
		}
	case DrainUnconfirmed:
		terminal.ExecutionData = data
		f.mutex.Lock()
		f.drainGeneration = generation
		f.mutex.Unlock()

		return managedObservedTerminal{
			terminal: terminal,
			receipt:  f.shell.observeAttempt(generation, drainUnconfirmed{}),
		}
	default:
		panic("unsupported fixture terminal")
	}
}
func (f *managedAttemptFixture) stop(*OwnedAttempt) {
	f.mutex.Lock()
	f.stops++
	close(f.stopRelease)
	f.mutex.Unlock()
}
func (f *managedAttemptFixture) emergency(epoch fatalEpochID) managedObservedEmergency {
	f.mutex.Lock()
	f.emergencies++
	generation := f.drainGeneration
	if f.emergencyGeneration != 0 {
		generation = f.emergencyGeneration
	}
	f.mutex.Unlock()
	if f.emergencyEmpty {
		settlement := f.shell.settleEmergency(emergencySweep{})

		return managedObservedEmergency{epoch: epoch, settlement: settlement}
	}
	settlement := f.shell.settleEmergency(emergencySweep{resolutions: []emergencyResolution{{
		generation: generation, disposition: emergencyCustodyTransferred,
	}}})

	return managedObservedEmergency{epoch: epoch, settlement: settlement}
}
