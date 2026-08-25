package ooze

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/integerincrement"
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
	case <-time.After(time.Second):
		t.Fatal("first release did not return after terminal settlement")
	}
}
