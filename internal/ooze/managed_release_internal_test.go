package ooze

import (
	"reflect"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
	"github.com/gtramontina/ooze/viruses/loopbreak"
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
		t.Fatal("first release did not launch its baseline")
	}
	recursive := process.release(configuration)
	if recursive.Outcome != ManagedAborted || attempts.launches != 1 {
		t.Fatalf("recursive outcome/launches = %v/%d, want aborted before a second launch", recursive.Outcome, attempts.launches)
	}
	select {
	case result := <-first:
		t.Fatalf("first release returned before owned wait settled: %#v", result)
	default:
	}

	close(release)
	select {
	case result := <-first:
		if result.Outcome != ManagedCompleted {
			t.Fatalf("first outcome = %v, want completed", result.Outcome)
		}
		if len(result.Mutations) != 1 || result.Mutations[0].Primary.Kind != ManagedAttemptSettled ||
			result.Mutations[0].Primary.CommandDuration != time.Second || result.Mutations[0].Primary.Passed {
			t.Fatalf("completed mutation evidence = %#v, want immutable killed settlement", result.Mutations)
		}
	case <-time.After(time.Second):
		t.Fatal("first release did not return after terminal settlement")
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
	if result.Outcome != ManagedCleanupUnconfirmed || !reflect.DeepEqual(result.Residual, want) {
		t.Fatalf("cleanup result = %#v, want ordered residual %#v", result, want)
	}
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

	if result.Outcome != ManagedInvariantViolation || result.Invariant == nil ||
		result.Invariant.Operation != "campaign present attempt" ||
		result.Invariant.Reason != "trip kind is invalid" || attempts.emergencies != 1 {
		t.Fatalf("invariant result/evidence/emergencies = %#v/%#v/%d", result, result.Invariant, attempts.emergencies)
	}
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
	if result.Outcome != ManagedCompleted || len(result.Mutations) != 1 ||
		len(attempts.specs) != 2 || attempts.specs[0].Command[0] != "original-command" {
		t.Fatalf("snapshotted result/specs = %#v/%#v", result, attempts.specs)
	}
}
