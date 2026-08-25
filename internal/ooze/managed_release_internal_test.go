package ooze

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/gtramontina/ooze/viruses/loopbreak"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManagedProcessRejectsRecursiveReleaseWhileCallerOwnsWait(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	release := make(chan struct{})
	launched := make(chan Spec, 1)
	attempts := &managedAttemptFixture{
		waitAll: release, launchStarted: launched,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	process := &managedProcess{capacity: 1, runtime: shell, attempts: attempts}
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	configuration := ManagedReleaseConfiguration{
		Lineage: 11, Repository: repository, TemporaryDir: &managedTemporaryDirectory{},
		Command: []string{"test"}, Profile: AutomaticProfile,
		Viruses: []viruses.Virus{integerincrement.New()},
	}
	first := make(chan ManagedReleaseResult, 1)
	go func() { first <- process.release(configuration) }()

	select {
	case <-launched:
	case <-time.After(time.Second):
		require.FailNow(t, "first release did not launch its baseline")
	}
	recursive := process.release(configuration)
	assert.Equal(t, ManagedAborted, recursive.Outcome, "recursive outcome/launches = %v/%d, want aborted before a second launch", recursive.Outcome, attempts.launches)
	assert.EqualValues(t, 1, attempts.launches, "recursive outcome/launches = %v/%d, want aborted before a second launch", recursive.Outcome, attempts.launches)
	select {
	case result := <-first:
		require.FailNow(t, "first release returned before owned wait settled: %#v", result)
	default:
	}

	close(release)
	select {
	case result := <-first:
		assert.Equal(t, ManagedCompleted, result.Outcome, "first outcome = %v, want completed", result.Outcome)
		require.Len(t, result.Mutations, 1, "completed mutation evidence = %#v, want immutable killed settlement", result.Mutations)
		assert.Equal(t, ManagedAttemptSettled, result.Mutations[0].Primary.Kind, "completed mutation evidence = %#v, want immutable killed settlement", result.Mutations)
		assert.Equal(t, time.Second, result.Mutations[0].Primary.CommandDuration, "completed mutation evidence = %#v, want immutable killed settlement", result.Mutations)
		assert.False(t, result.Mutations[0].Primary.Passed, "completed mutation evidence = %#v, want immutable killed settlement", result.Mutations)
	case <-time.After(time.Second):
		require.FailNow(t, "first release did not return after terminal settlement")
	}
}

func TestManagedCleanupFailureRetainsOrderedResidualEvidence(t *testing.T) {
	result := presentManagedRelease(managedCampaignResult{failure: cleanupUnconfirmedFault{
		residual: nonEmptyResidualCustody{
			head: campaignResidualCustody{attempt: "campaign-1:2", generation: 7, stage: admissionOwned, transferred: true},
			tail: []campaignResidualCustody{{attempt: "campaign-1:3", generation: 9, stage: admissionProspective}},
		},
	}})

	want := []ManagedResidualCustody{
		{Attempt: "campaign-1:2", Generation: 7, Stage: ManagedResidualOwned, Transferred: true},
		{Attempt: "campaign-1:3", Generation: 9, Stage: ManagedResidualProspective},
	}
	assert.Equal(t, ManagedCleanupUnconfirmed, result.Outcome, "cleanup result = %#v, want ordered residual %#v", result, want)
	assert.Equal(t, want, result.Residual, "cleanup result = %#v, want ordered residual %#v", result, want)
}

func TestManagedProcessReturnsInvariantPresentationAfterEmergencySettlement(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	attempts := &managedAttemptFixture{
		emergencyEmpty: true,
		terminals: []Terminal{
			Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
			Tripped{Trip: nil, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		},
	}
	process := &managedProcess{capacity: 1, runtime: shell, attempts: attempts}
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}

	result := process.release(ManagedReleaseConfiguration{
		Lineage: 11, Repository: repository, TemporaryDir: &managedTemporaryDirectory{},
		Command: []string{"test"}, Profile: AutomaticProfile,
		Viruses: []viruses.Virus{integerincrement.New()},
	})

	assert.Equal(t, ManagedInvariantViolation, result.Outcome, "invariant result/evidence/emergencies = %#v/%#v/%d", result, result.Invariant, attempts.emergencies)
	require.NotNil(t, result.Invariant, "invariant result/evidence/emergencies = %#v/%#v/%d", result, result.Invariant, attempts.emergencies)
	assert.EqualValues(t, "campaign present attempt", result.Invariant.Operation, "invariant result/evidence/emergencies = %#v/%#v/%d", result, result.Invariant, attempts.emergencies)
	assert.EqualValues(t, "trip kind is invalid", result.Invariant.Reason, "invariant result/evidence/emergencies = %#v/%#v/%d", result, result.Invariant, attempts.emergencies)
	assert.EqualValues(t, 1, attempts.emergencies, "invariant result/evidence/emergencies = %#v/%#v/%d", result, result.Invariant, attempts.emergencies)
}

func TestManagedProcessSnapshotsCallerConfigurationBeforeFilesystemWork(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	attempts := &managedAttemptFixture{terminals: []Terminal{
		Settled{Exit: ExitStatus{}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
		Settled{Exit: ExitStatus{Code: 1}, ExecutionData: ExecutionData{CommandDuration: time.Second}},
	}}
	process := &managedProcess{capacity: 1, runtime: shell, attempts: attempts}
	repository := &managedMemoryRepository{files: []*gosourcefile.GoSourceFile{
		gosourcefile.New("source.go", []byte("package source\nvar number = 0\n")),
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	temporaryDirectory := &blockingManagedTemporaryDirectory{
		managedTemporaryDirectory: managedTemporaryDirectory{}, started: started, release: release,
	}
	configuration := ManagedReleaseConfiguration{
		Lineage: 11, Repository: repository, TemporaryDir: temporaryDirectory,
		Command: []string{"original-command"}, Profile: AutomaticProfile,
		Viruses: []viruses.Virus{integerincrement.New()},
	}
	completed := make(chan ManagedReleaseResult, 1)
	go func() { completed <- process.release(configuration) }()
	<-started
	configuration.Command[0] = "mutated-command"
	configuration.Viruses[0] = loopbreak.New()
	close(release)

	result := <-completed
	assert.Equal(t, ManagedCompleted, result.Outcome, "snapshotted result/specs = %#v/%#v", result, attempts.specs)
	assert.EqualValues(t, 1, len(result.Mutations), "snapshotted result/specs = %#v/%#v", result, attempts.specs)
	require.Len(t, attempts.specs, 2, "snapshotted result/specs = %#v/%#v", result, attempts.specs)
	assert.EqualValues(t, "original-command", attempts.specs[0].Command[0], "snapshotted result/specs = %#v/%#v", result, attempts.specs)
}
