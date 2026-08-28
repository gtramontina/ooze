package campaign_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedCampaignConfigurationContainsOnlyExecutionInputs(t *testing.T) {
	configuration := reflect.TypeOf(campaign.Configuration{})
	_, exposesRecorder := configuration.FieldByName("Recorder")

	assert.False(t, exposesRecorder)
}

func TestManagedCampaignEmptyCatalogueRunsNoCommands(t *testing.T) {
	repository := &managedMemoryRepository{}
	attempts := &managedAttemptFixture{}
	result := executeManagedCampaign(processruntime.New(2), attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 2,
	})

	assert.Equal(t, campaign.ManagedNoMutants, result.Outcome)
	assert.EqualValues(t, 0, attempts.launches, "launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
	assert.EqualValues(t, 1, repository.materializations, "launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
	assert.True(t, repository.snapshot.removed, "launches/materializations/snapshot = %d/%d/%#v", attempts.launches, repository.materializations, repository.snapshot)
}

func TestManagedCampaignLazilyOverlapsAutomaticPrimariesUpToCapacity(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	started := make(chan supervision.Spec, 2)
	release := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitStarted: started, releasePrimaries: release,
		terminals: []supervision.Terminal{
			supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
		},
	}
	runtime := processruntime.New(2)
	completed := make(chan campaign.Result, 1)
	go func() {
		completed <- executeManagedCampaign(runtime, attempts, campaign.Configuration{
			Identity: "campaign-a", Lineage: 11, Repository: repository,
			TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
			Profile: processruntime.AutomaticProfile, Peers: 2,
			Viruses: []viruses.Virus{integerincrement.New()},
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
		assert.Equal(t, campaign.ManagedCompleted, result.Outcome)
		assert.Len(t, result.Mutations, 2)
	case <-time.After(time.Second):
		require.FailNow(t, "overlapped campaign did not complete")
	}
}

func TestManagedCampaignSerialPrimariesAreExclusiveWithDetectedCapacityProfile(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	started := make(chan supervision.Spec, 2)
	release := make(chan struct{})
	attempts := &managedAttemptFixture{
		waitStarted: started, releasePrimaries: release,
		terminals: []supervision.Terminal{
			supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
		},
	}
	runtime := processruntime.New(3)
	completed := make(chan campaign.Result, 1)
	go func() {
		completed <- executeManagedCampaign(runtime, attempts, campaign.Configuration{
			Identity: "campaign-a", Lineage: 11, Repository: repository,
			TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
			Profile: processruntime.SerialProfile, Peers: 3,
			Viruses: []viruses.Virus{integerincrement.New()},
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
		assert.Equal(t, campaign.ManagedCompleted, result.Outcome)
		assert.Len(t, result.Mutations, 2)
	case <-time.After(time.Second):
		require.FailNow(t, "serial campaign did not complete")
	}
}

func TestManagedCampaignRunsBaselineBeforeOneAutomaticPrimary(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []supervision.Terminal{
		supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: 2 * time.Second}},
		supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
	}}
	result := executeManagedCampaign(processruntime.New(2), attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 2,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	require.Equal(t, campaign.ManagedCompleted, result.Outcome)
	require.Len(t, result.Mutations, 1)
	assert.Equal(t, campaign.ManagedKilled, result.Mutations[0].Outcome)
	assert.EqualValues(t, 2, attempts.launches, "launches/specs = %d/%#v", attempts.launches, attempts.specs)
	assert.EqualValues(t, 2, len(attempts.specs), "launches/specs = %d/%#v", attempts.launches, attempts.specs)
	for _, spec := range attempts.specs {
		assert.True(t, slices.Contains(spec.Env, "GOMAXPROCS=1"), "automatic spec environment = %#v", spec.Env)
	}
}

func TestManagedCampaignReportsBaselineAbortCause(t *testing.T) {
	tests := []struct {
		name     string
		terminal supervision.Terminal
		cause    campaign.AbortCause
	}{
		{name: "nonzero exit", terminal: supervision.Settled{Exit: supervision.ExitStatus{Code: 1}}, cause: campaign.AbortBaselineFailed},
		{name: "automatic deadline", terminal: supervision.Tripped{Trip: supervision.AutomaticDeadlineTrip{}}, cause: campaign.AbortBaselineDeadline},
		{name: "process fuse", terminal: supervision.Tripped{Trip: supervision.FuseTrip{Live: 65}}, cause: campaign.AbortBaselineFuse},
		{name: "stopped", terminal: supervision.Stopped{}, cause: campaign.AbortBaselineStopped},
		{name: "infrastructure census", terminal: supervision.Infrastructure{Cause: supervision.CensusFailed, Err: assert.AnError}, cause: campaign.AbortBaselineInfrastructure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
				gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
			}}
			result := executeManagedCampaign(processruntime.New(1), &managedAttemptFixture{
				terminals: []supervision.Terminal{test.terminal},
			}, campaign.Configuration{
				Identity: "campaign-a", Lineage: 11, Repository: repository,
				TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
				Profile: processruntime.AutomaticProfile, Peers: 1,
				Viruses: []viruses.Virus{integerincrement.New()},
			})

			assert.Equal(t, campaign.ManagedAborted, result.Outcome)
			assert.Equal(t, test.cause, result.Cause)
			assert.Equal(t, 1, result.Total)
			require.NotNil(t, result.Baseline)
		})
	}
}

func TestManagedCampaignPropagatesAbsoluteTimeoutAndRetainsTimedOutResult(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{terminals: []supervision.Terminal{
		supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
		supervision.Tripped{Trip: supervision.AutomaticDeadlineTrip{}},
	}}
	result := executeManagedCampaign(processruntime.New(1), attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1, MutationTimeout: 37 * time.Millisecond,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	require.Equal(t, campaign.ManagedCompleted, result.Outcome)
	require.Len(t, result.Mutations, 1)
	assert.Equal(t, campaign.ManagedTimedOut, result.Mutations[0].Outcome)
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
		terminals: []supervision.Terminal{
			supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Tripped{Trip: supervision.AutomaticDeadlineTrip{}},
			supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
		},
	}
	result := executeManagedCampaign(processruntime.New(2), attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 2, MutationTimeout: time.Minute,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	require.Equal(t, campaign.ManagedCompleted, result.Outcome)
	require.Len(t, result.Mutations, 2)
	assert.Equal(t, campaign.ManagedKilled, result.Mutations[0].Outcome)
	assert.Equal(t, campaign.ManagedKilled, result.Mutations[1].Outcome)
	assert.True(t, result.SingleAdmissionFallback)
	require.NotNil(t, result.Mutations[0].Confirmation)
	assert.Equal(t, campaign.AttemptSettled, result.Mutations[0].Confirmation.Kind)
	assert.EqualValues(t, 4, attempts.launches)
}

func TestManagedCampaignResumesWithSingleAdmissionAfterConfirmationPressure(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\nvar third = 0\nvar fourth = 0\n")),
	}}
	deadlineTrip := supervision.Tripped{Trip: supervision.AutomaticDeadlineTrip{}}
	killed := supervision.Settled{Exit: supervision.ExitStatus{Code: 1}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}}
	attempts := &managedAttemptFixture{
		waitForLaunches: 3,
		launchesReady:   make(chan struct{}),
		terminals: []supervision.Terminal{
			supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			deadlineTrip, killed,
			killed,
			killed, killed,
		},
	}
	runtime := processruntime.New(2)
	completed := make(chan campaign.Result, 1)
	go func() {
		completed <- executeManagedCampaign(runtime, attempts, campaign.Configuration{
			Identity: "campaign-a", Lineage: 11, Repository: repository,
			TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
			Profile: processruntime.AutomaticProfile, Peers: 2, MutationTimeout: time.Minute,
			Viruses: []viruses.Virus{integerincrement.New()},
		})
	}()

	select {
	case result := <-completed:
		assert.Equal(t, campaign.ManagedCompleted, result.Outcome)
		assert.Len(t, result.Mutations, 4)
		assert.True(t, result.SingleAdmissionFallback)
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
		terminals: []supervision.Terminal{
			supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
		},
	}
	result := executeManagedCampaign(processruntime.New(2), attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 2,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
	assert.True(t, result.SingleAdmissionFallback)
	assert.EqualValues(t, 2, attempts.launches)
}

func TestManagedCampaignStopsOwnedPeerAndWaitsForSettlementBeforeAbort(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar first = 0\nvar second = 0\n")),
	}}
	attempts := &managedAttemptFixture{
		stopRelease: make(chan struct{}),
		terminals: []supervision.Terminal{
			supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
			supervision.Infrastructure{Cause: supervision.CensusFailed, Err: errors.New("census failed")},
			supervision.Stopped{},
		},
	}
	result := executeManagedCampaign(processruntime.New(2), attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 2,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
	assert.EqualValues(t, 1, attempts.stops)
}

func TestManagedCampaignSettlesRuntimeEmergencyBeforeCleanupFailure(t *testing.T) {
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	shell := processruntime.New(1)
	attempts := &managedAttemptFixture{shell: shell, terminals: []supervision.Terminal{
		supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
		supervision.DrainUnconfirmed{
			Residual: supervision.OwnedUndrained,
			ExecutionData: supervision.ExecutionData{
				CommandDuration: time.Second,
				Output:          supervision.OutputSnapshot{Bytes: "private", Cutoff: 7, CompleteThroughCutoff: true},
				Failures:        supervision.FailureDiagnostics{DrainCensus: "census failed"},
			},
		},
	}}
	result := executeManagedCampaign(shell, attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	require.Len(t, result.FatalAttempts, 1)
	assert.Equal(t, "campaign-a:2", result.FatalAttempts[0].Attempt)
	assert.EqualValues(t, 7, result.FatalAttempts[0].Evidence.Output.Cutoff)
	assert.Equal(t, "census failed", result.FatalAttempts[0].Evidence.Failures.DrainCensus)
	assert.EqualValues(t, 1, attempts.emergencies)
}

func TestManagedCampaignAuthorizesForcedAbortAfterEmptyEmergencySweep(t *testing.T) {
	shell := processruntime.New(1)
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{shell: shell, emergencyEmpty: true}
	started := make(chan struct{})
	release := make(chan struct{})
	temporaryDirectory := &blockingManagedTemporaryDirectory{
		managedTemporaryDirectory: managedTemporaryDirectory{}, started: started, release: release,
	}
	go func() {
		<-started
		shell.Close("peer fatal without custody")
		close(release)
	}()

	result := executeManagedCampaign(shell, attempts, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: temporaryDirectory, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
}

func TestManagedCampaignNormalizesSnapshotBoundaryPanic(t *testing.T) {
	result := executeManagedCampaign(processruntime.New(1), &managedAttemptFixture{}, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: managedPanickingRepository{},
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
	assert.Equal(t, campaign.AbortSnapshotMaterialization, result.Cause)
}

func TestManagedCampaignCleansWorkspaceAcquiredBeforeMutationPanic(t *testing.T) {
	repository := &managedPartialWorkspaceRepository{}
	result := executeManagedCampaign(processruntime.New(1), &managedAttemptFixture{terminals: []supervision.Terminal{
		supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
	}}, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
	require.NotNil(t, repository.workspace)
	assert.True(t, repository.workspace.removed)
}

func TestManagedCampaignReportsOnlyStructuredResidueWhenFailedWorkspaceCannotBeCleaned(t *testing.T) {
	repository := &managedPartialWorkspaceRepository{failWorkspaceCleanup: true}
	result := executeManagedCampaign(processruntime.New(1), &managedAttemptFixture{terminals: []supervision.Terminal{
		supervision.Settled{Exit: supervision.ExitStatus{}, ExecutionData: supervision.ExecutionData{CommandDuration: time.Second}},
	}}, campaign.Configuration{
		Identity: "campaign-a", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
	assert.Equal(t, campaign.AbortWorkspaceMaterialization, result.Cause)
	require.Len(t, result.ArtifactResidue, 1)
	assert.True(t, strings.HasPrefix(result.ArtifactResidue[0], "temporary-"))
}

func TestManagedCampaignConsumesFatalEpochWhileWaitingForAdmission(t *testing.T) {
	waiting := make(chan struct{}, 1)
	shell := processruntime.NewObserved(1, processruntime.ObserverFunc(func(event processruntime.RecordedCut) {
		if event.Operation() == processruntime.RequestAdmissionOperation &&
			event.Result().Admission().Request().Attempt != "held" {
			waiting <- struct{}{}
		}
	}))
	heldCampaign := shell.RegisterCampaign(99)
	held := shell.RequestAdmission(processruntime.Admission{
		Campaign: heldCampaign.Campaign(), Attempt: "held", Class: processruntime.ExclusiveAdmission,
		Profile: processruntime.AutomaticProfile, Deadline: 10 * time.Minute,
	})
	heldGrant, _ := held.Receive()
	prepared := shell.CommitStart(heldGrant, processruntime.NewStartCell())
	prepared.Observe(prepared.Launch(func(processruntime.Generation) processruntime.Observation { return processruntime.Owned() }))

	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	attempts := &managedAttemptFixture{shell: shell, emergencyGeneration: prepared.Generation()}
	go func() {
		<-waiting
		shell.Observe(prepared.Generation(), processruntime.DrainUnconfirmed())
	}()

	result := executeManagedCampaign(shell, attempts, campaign.Configuration{
		Identity: "campaign-waiting", Lineage: 11, Repository: repository,
		TemporaryDir: &managedTemporaryDirectory{}, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, campaign.ManagedAborted, result.Outcome)
	assert.EqualValues(t, 1, attempts.emergencies)
}

func executeManagedCampaign(runtime *processruntime.Runtime, attempts supervision.System, configuration campaign.Configuration) campaign.Result {
	return campaign.NewExecutorWithSystem(runtime, attempts).Execute(configuration)
}

type managedMemoryRepository struct {
	files            []*gosourcefile.GoSourceFile
	materializations int
	snapshot         *managedMemoryTemporaryRepository
}

type managedPanickingRepository struct{}

func (managedPanickingRepository) ListGoSourceFiles() []*gosourcefile.GoSourceFile { return nil }
func (managedPanickingRepository) MaterializeTemporaryRepository(string) campaign.TemporaryRepository {
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
func (r *managedPartialWorkspaceRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
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
func (r *managedPartialSnapshot) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
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
func (*managedPartialWorkspace) MaterializeTemporaryRepository(string) campaign.TemporaryRepository {
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

func (r *managedMemoryRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
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
func (r *managedMemoryTemporaryRepository) MaterializeTemporaryRepository(path string) campaign.TemporaryRepository {
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
	specs               []supervision.Spec
	terminals           []supervision.Terminal
	shell               *processruntime.Runtime
	byGeneration        map[processruntime.Generation]supervision.Spec
	terminalByGen       map[processruntime.Generation]supervision.Terminal
	starts              map[processruntime.Generation]processruntime.PreparedStart
	waitStarted         chan supervision.Spec
	releasePrimaries    <-chan struct{}
	waitAll             <-chan struct{}
	launchStarted       chan supervision.Spec
	waitForLaunches     int
	launchesReady       chan struct{}
	launchesReadyOnce   sync.Once
	notReleasedAt       int
	stopRelease         chan struct{}
	stops               int
	emergencies         int
	drainGeneration     processruntime.Generation
	emergencyGeneration processruntime.Generation
	emergencyEmpty      bool
}

func managedTerminalData(terminal supervision.Terminal) supervision.ExecutionData {
	switch terminal := terminal.(type) {
	case supervision.Settled:
		return terminal.ExecutionData
	case supervision.Infrastructure:
		return terminal.ExecutionData
	case supervision.Tripped:
		return terminal.ExecutionData
	case supervision.Stopped:
		return terminal.ExecutionData
	case supervision.DrainUnconfirmed:
		return terminal.ExecutionData
	default:
		panic("unsupported fixture terminal")
	}
}

func (f *managedAttemptFixture) ReserveLaunch(*processruntime.StartCell, supervision.Spec) {}

func (f *managedAttemptFixture) DiscardLaunch(*processruntime.StartCell) {}

func (f *managedAttemptFixture) Launch(start processruntime.PreparedStart, spec supervision.Spec) supervision.ObservedLaunch {
	f.mutex.Lock()
	f.launches++
	f.specs = append(f.specs, spec)
	if f.byGeneration == nil {
		f.byGeneration = make(map[processruntime.Generation]supervision.Spec)
		f.terminalByGen = make(map[processruntime.Generation]supervision.Terminal)
		f.starts = make(map[processruntime.Generation]processruntime.PreparedStart)
	}
	f.byGeneration[start.Generation()] = spec
	f.starts[start.Generation()] = start
	if f.launches <= len(f.terminals) {
		f.terminalByGen[start.Generation()] = f.terminals[f.launches-1]
	}
	if f.waitForLaunches != 0 && f.launches >= f.waitForLaunches {
		f.launchesReadyOnce.Do(func() { close(f.launchesReady) })
	}
	f.mutex.Unlock()
	if f.launchStarted != nil {
		f.launchStarted <- spec
	}
	var result supervision.LaunchResult
	if f.notReleasedAt == f.launches {
		observed := start.Launch(func(processruntime.Generation) processruntime.Observation {
			result = supervision.NotReleased{Kind: supervision.LaunchResourceExhausted}

			return processruntime.NotReleased(true)
		})
		receipt := start.Observe(observed)

		return supervision.NewObservedLaunch(result, receipt)
	}
	observed := start.Launch(func(processruntime.Generation) processruntime.Observation {
		result = supervision.Owned{Attempt: &supervision.OwnedAttempt{}}

		return processruntime.Owned()
	})
	receipt := start.Observe(observed)

	return supervision.NewObservedLaunch(result, receipt)
}
func (f *managedAttemptFixture) Wait(generation supervision.Generation, _ *supervision.OwnedAttempt) supervision.ObservedTerminal {
	f.mutex.Lock()
	terminal := f.terminalByGen[generation]
	spec := f.byGeneration[generation]
	start := f.starts[generation]
	f.mutex.Unlock()
	if f.waitAll != nil {
		<-f.waitAll
	}
	if f.waitForLaunches != 0 && spec.Deadline != 10*time.Minute {
		<-f.launchesReady
	}
	if f.waitStarted != nil && spec.Deadline != 10*time.Minute {
		f.waitStarted <- spec
		<-f.releasePrimaries
	}
	data := managedTerminalData(terminal)
	data.Deadline = spec.Deadline
	switch terminal := terminal.(type) {
	case supervision.Settled:
		terminal.ExecutionData = data
		return supervision.NewObservedTerminal(terminal, start.Observe(processruntime.Settled(spec.Profile, spec.Deadline)))
	case supervision.Infrastructure:
		terminal.ExecutionData = data

		return supervision.NewObservedTerminal(terminal, start.Observe(processruntime.Infrastructure(terminal.Err.Error())))
	case supervision.Tripped:
		terminal.ExecutionData = data
		terminal.BoundFired = supervision.CommandDeadlineFired

		return supervision.NewObservedTerminal(terminal, start.Observe(processruntime.Tripped(false, spec.Profile, spec.Deadline)))
	case supervision.Stopped:
		if f.stopRelease != nil {
			<-f.stopRelease
		}
		terminal.ExecutionData = data

		return supervision.NewObservedTerminal(terminal, start.Observe(processruntime.Stopped()))
	case supervision.DrainUnconfirmed:
		terminal.ExecutionData = data
		f.mutex.Lock()
		f.drainGeneration = generation
		f.mutex.Unlock()

		return supervision.NewObservedTerminal(terminal, start.Observe(processruntime.DrainUnconfirmed()))
	default:
		panic("unsupported fixture terminal")
	}
}
func (f *managedAttemptFixture) Stop(*supervision.OwnedAttempt) {
	f.mutex.Lock()
	f.stops++
	close(f.stopRelease)
	f.mutex.Unlock()
}
func (f *managedAttemptFixture) EmergencyDrain(supervision.EmergencyRequest) (supervision.SweepResult, processruntime.EmergencySettlement) {
	f.mutex.Lock()
	f.emergencies++
	generation := f.drainGeneration
	if f.emergencyGeneration != 0 {
		generation = f.emergencyGeneration
	}
	f.mutex.Unlock()
	if f.emergencyEmpty {
		settlement := f.shell.SettleEmergency(nil)

		return supervision.SweepDrained{}, settlement
	}
	settlement := f.shell.SettleEmergency([]processruntime.Resolution{processruntime.TransferCustody(generation)})

	return supervision.SweepDrained{}, settlement
}
