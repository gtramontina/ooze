package ooze

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestSupervisorDriverEmergencyDuringProspectiveLaunchReturnsUnconfirmedAndSettlesLateNoRelease(t *testing.T) {
	registeredAt := time.Unix(7_000, 0)
	launchBy := registeredAt.Add(time.Second)
	emergencyAt := registeredAt.Add(500 * time.Millisecond)
	drainBy := emergencyAt.Add(5 * time.Second)
	nativeStarted := make(chan struct{})
	nativeDone := make(chan struct{})
	boundary := make(chan time.Time)

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 88})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-prospective-emergency",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now:     func() time.Time { return registeredAt },
		launchBoundary: func(got time.Time) <-chan time.Time {
			if !got.Equal(launchBy) {
				t.Fatalf("launch boundary = %v, want %v", got, launchBy)
			}

			return boundary
		},
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				close(nativeStarted)
				<-nativeDone
				completedAt := emergencyAt.Add(time.Nanosecond)
				completion := supervisorLaunchCompletion{
					generation: action.generation,
					action:     action.token,
					at:         completedAt,
					kind:       supervisorLaunchProvenNotReleased,
					failure:    LaunchFailed,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: completedAt, completion: &completion,
				}
			case supervisorRevokeLaunchRelease:
				return nil
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	launched := make(chan LaunchResult, 1)
	go func() {
		launched <- supervisor.Launch(Spec{
			Attempt: "driver-prospective-emergency", Command: []string{"blocked-start"}, Dir: "/tmp",
			Profile: SerialProfile, Deadline: 10 * time.Second,
		})
	}()
	<-nativeStarted
	closure := shell.closeRuntime(runtimeFatalCause("prospective launch emergency"))
	if len(closure.residual) != 1 || closure.residual[0].generation == 0 {
		t.Fatalf("runtime closure = %#v, want exact prospective generation", closure)
	}
	settled := make(chan SweepResult, 1)
	go func() {
		settled <- supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	}()

	select {
	case result := <-launched:
		if result != (LaunchUnconfirmed{Residual: ProspectiveUnresolved}) {
			t.Fatalf("launch = %#v, want prospective LaunchUnconfirmed", result)
		}
	case <-time.After(time.Second):
		t.Fatal("pre-Owned emergency did not release the launch callback")
	}

	close(nativeDone)
	select {
	case settlement := <-settled:
		if _, ok := settlement.(SweepDrained); !ok {
			t.Fatalf("emergency settlement = %#v, want SweepDrained", settlement)
		}
	case <-time.After(time.Second):
		t.Fatal("late no-release did not complete emergency settlement")
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeClosedDrained ||
		len(snapshot.admissions) != 0 {
		t.Fatalf("prospective emergency final runtime = %#v", snapshot)
	}
}

func TestSupervisorDriverEmergencyCoordinatesMultipleProspectiveEqualityCompletions(t *testing.T) {
	registeredAt := time.Unix(7_250, 0)
	emergencyAt := registeredAt.Add(500 * time.Millisecond)
	drainBy := emergencyAt.Add(5 * time.Second)
	type pendingLaunch struct {
		action  supervisorAction
		release chan struct{}
	}
	started := make(chan pendingLaunch, 2)
	boundary := make(chan time.Time)
	shell := newProcessRuntimeShell(2)
	grants := make(map[attemptIdentity]admissionGrant)
	for index, attempt := range []attemptIdentity{"multi-prospective-a", "multi-prospective-b"} {
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(120 + index)})
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: attempt, class: sharedAdmission,
		})
		grants[attempt] = <-requested.delivery
	}
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time { return registeredAt },
		launchBoundary: func(time.Time) <-chan time.Time { return boundary },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				release := make(chan struct{})
				started <- pendingLaunch{action: action, release: release}
				<-release
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token, at: emergencyAt,
					kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: emergencyAt, completion: &completion,
				}
			case supervisorRevokeLaunchRelease:
				return nil
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			grant, ok := grants[attempt]
			if !ok {
				t.Fatalf("unexpected attempt %q", attempt)
			}

			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	results := make(chan LaunchResult, 2)
	for _, attempt := range []string{"multi-prospective-a", "multi-prospective-b"} {
		attempt := attempt
		go func() {
			results <- supervisor.Launch(Spec{
				Attempt: attempt, Command: []string{"blocked-start"}, Dir: "/tmp",
				Profile: AutomaticProfile, Deadline: 10 * time.Second,
			})
		}()
	}
	pending := []pendingLaunch{<-started, <-started}
	closure := shell.closeRuntime(runtimeFatalCause("multi prospective emergency"))
	if len(closure.residual) != 2 {
		t.Fatalf("runtime closure residuals = %#v, want two prospective generations", closure.residual)
	}
	settled := make(chan SweepResult, 1)
	go func() {
		settled <- supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	}()
	for range 2 {
		select {
		case result := <-results:
			if result != (LaunchUnconfirmed{Residual: ProspectiveUnresolved}) {
				t.Fatalf("launch = %#v, want LaunchUnconfirmed", result)
			}
		case <-time.After(time.Second):
			t.Fatal("multi-prospective emergency did not release every callback")
		}
	}
	for _, launch := range pending {
		close(launch.release)
	}
	select {
	case settlement := <-settled:
		if _, ok := settlement.(SweepDrained); !ok {
			t.Fatalf("settlement = %#v, want SweepDrained", settlement)
		}
	case <-time.After(time.Second):
		t.Fatal("equality no-release completions did not settle one emergency epoch")
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeClosedDrained || len(snapshot.admissions) != 0 {
		t.Fatalf("multi-prospective final runtime = %#v", snapshot)
	}
}

func TestSupervisorDriverEmergencyAdoptsReleasedProspectiveWithRootSnapshot(t *testing.T) {
	registeredAt := time.Unix(7_500, 0)
	releasedAt := registeredAt.Add(100 * time.Millisecond)
	emergencyAt := registeredAt.Add(500 * time.Millisecond)
	drainBy := emergencyAt.Add(5 * time.Second)
	boundary := make(chan time.Time)
	boundaryEntered := make(chan struct{})
	allowBoundaryReturn := make(chan struct{})
	rootSnapshot := make(chan struct{})
	nextAt := emergencyAt
	registered := false

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 87})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-released-prospective-emergency",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			if !registered {
				registered = true

				return registeredAt
			}
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time {
			close(boundaryEntered)
			<-allowBoundaryReturn

			return boundary
		},
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		recheckRoot: func(attemptGeneration) (ExitStatus, time.Time, bool, error) {
			close(rootSnapshot)

			return ExitStatus{}, time.Time{}, false, nil
		},
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}
				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorForceOwned:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorObserveEmptiness:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainObservedEmpty,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorCaptureOutput:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, ref: 3,
				}

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: completion.at, output: &completion,
				}
			case supervisorReleaseDomain:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt,
				}

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: completion.at, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	launched := make(chan LaunchResult, 1)
	go func() {
		launched <- supervisor.Launch(Spec{
			Attempt: "driver-released-prospective-emergency", Command: []string{"released-start"}, Dir: "/tmp",
			Profile: SerialProfile, Deadline: 10 * time.Second,
		})
	}()
	<-boundaryEntered
	deadline := time.After(time.Second)
	for {
		driver.mutex.Lock()
		published := len(driver.attempts) == 1
		for _, attempt := range driver.attempts {
			published = published && attempt.launchEvent != nil
		}
		driver.mutex.Unlock()
		if published {
			break
		}
		select {
		case <-deadline:
			t.Fatal("released completion was not published while launch callback was held")
		default:
			runtime.Gosched()
		}
	}
	closure := shell.closeRuntime(runtimeFatalCause("released prospective emergency"))
	if len(closure.residual) != 1 || closure.residual[0].generation == 0 {
		t.Fatalf("runtime closure = %#v, want exact prospective generation", closure)
	}
	settled := make(chan SweepResult, 1)
	go func() {
		settled <- supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	}()
	<-rootSnapshot
	close(allowBoundaryReturn)

	select {
	case result := <-launched:
		owned, ok := result.(Owned)
		if !ok || owned.Attempt == nil {
			t.Fatalf("launch = %#v, want emergency-adopted Owned", result)
		}
	case <-time.After(time.Second):
		t.Fatal("released pre-Owned emergency did not release the launch callback")
	}
	select {
	case settlement := <-settled:
		if _, ok := settlement.(SweepDrained); !ok {
			t.Fatalf("emergency settlement = %#v, want SweepDrained", settlement)
		}
	case <-time.After(time.Second):
		t.Fatal("released pre-Owned emergency did not settle")
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeClosedDrained ||
		len(snapshot.admissions) != 0 {
		t.Fatalf("released prospective emergency final runtime = %#v", snapshot)
	}
}

func TestSupervisorDriverBoundarySnapshotIncludesAlreadyPublishedEqualityCompletion(t *testing.T) {
	registeredAt := time.Unix(8_000, 0)
	launchBy := registeredAt.Add(time.Second)
	launchErr := errors.New("typed launch failure")
	completionReturned := make(chan struct{})

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 89})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-equality",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	var driver *supervisorDriver
	driver = newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now:     func() time.Time { return registeredAt },
		launchBoundary: func(got time.Time) <-chan time.Time {
			<-completionReturned
			for {
				driver.mutex.Lock()
				published := len(driver.attempts) == 1
				for _, attempt := range driver.attempts {
					published = published && attempt.launchEvent != nil
				}
				driver.mutex.Unlock()
				if published {
					break
				}
				runtime.Gosched()
			}
			boundary := make(chan time.Time, 1)
			boundary <- got

			return boundary
		},
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			completion := supervisorLaunchCompletion{
				generation: action.generation,
				action:     action.token,
				at:         launchBy,
				kind:       supervisorLaunchProvenNotReleased,
				failure:    LaunchFailed,
				diagnostic: 19,
			}
			close(completionReturned)

			return &supervisorEvent{
				kind: supervisorLaunchCompleted, generation: action.generation,
				at: launchBy, completion: &completion,
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			if ref != 19 {
				t.Fatalf("launch diagnostic ref = %d, want 19", ref)
			}

			return launchErr
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-equality", Command: []string{"equal-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	notReleased, ok := result.(NotReleased)
	if !ok || notReleased.Kind != LaunchFailed || !errors.Is(notReleased.Err, launchErr) {
		t.Fatalf("launch = %#v, want equality NotReleased", result)
	}
	if snapshot := shell.snapshot(); len(snapshot.admissions) != 0 {
		t.Fatalf("equality completion retained prospective custody: %#v", snapshot)
	}
}

func TestSupervisorLaunchBoundaryUsesTheAbsoluteLaunchBy(t *testing.T) {
	launchBy := time.Now().Add(20 * time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	started := time.Now()
	<-waitForSupervisorLaunchBoundary(launchBy)
	if elapsed := time.Since(started); elapsed > 20*time.Millisecond {
		t.Fatalf("expired absolute launch boundary waited %v", elapsed)
	}
}

func TestSupervisorDriverStartsOwnedMonitoringBeforeWait(t *testing.T) {
	registeredAt := time.Unix(8_500, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	nextAt := drainBy
	registered := false
	monitorStarted := make(chan struct{})
	allowExit := make(chan struct{})

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 101})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "driver-monitor-before-wait", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time {
			if !registered {
				registered = true
				return registeredAt
			}
			nextAt = nextAt.Add(time.Nanosecond)
			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token, at: releasedAt,
					kind: supervisorLaunchReleased,
				}
				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				close(monitorStarted)
				<-allowExit
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: exitedAt,
				}
				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: exitedAt, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorObserveEmptiness:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         drainBy, kind: supervisorDrainObservedEmpty,
				}
				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: drainBy, drain: &completion,
				}
			case supervisorCaptureOutput:
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         drainBy, ref: 1,
				}
				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: drainBy, output: &completion,
				}
			case supervisorReleaseDomain:
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: drainBy,
				}
				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: drainBy, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)
				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-monitor-before-wait", Command: []string{"managed"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	select {
	case <-monitorStarted:
	case <-time.After(time.Second):
		t.Fatal("owned root monitoring did not start before Wait")
	}
	close(allowExit)
	if terminal := owned.Attempt.Wait(); terminal == nil {
		t.Fatal("owned attempt returned no terminal after autonomous monitor completion")
	}
}

func TestSupervisorDriverReleasesRecorderOwnerCutBeforeNativeAction(t *testing.T) {
	recorder := newSimulationRecorder()
	fixture := newRunningReducerFixture(t, SerialProfile)
	reentered := make(chan struct{})
	returned := make(chan struct{})
	driver := &supervisorDriver{
		state: fixture.state, recorder: recorder,
		execute: func(supervisorAction) *supervisorEvent {
			leaveRecorder := recorder.enter()
			leaveRecorder()
			close(reentered)

			return nil
		},
	}
	eventAt := fixture.startedAt.Add(time.Second)
	event := supervisorEvent{
		kind: supervisorRunningObserved, generation: fixture.generation,
		at: eventAt, drainBy: fixture.drainBy,
		running: &supervisorRunningBundle{
			generation: fixture.generation, waitAction: fixture.waitAction,
			facts: []supervisorRunningFact{{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningRootExited, at: eventAt,
			}},
		},
	}
	go func() {
		defer func() {
			_ = recover()
			close(returned)
		}()
		driver.applyMonitorEvent(event)
	}()

	select {
	case <-reentered:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("native action could not enter a recorder owner cut")
	}
	<-returned
}

func TestSupervisorDriverReportsUnconfirmedAtLaunchBoundaryAndClosesLateNotReleased(t *testing.T) {
	registeredAt := time.Unix(9_000, 0)
	launchBy := registeredAt.Add(time.Second)
	boundary := make(chan time.Time, 1)
	nativeDone := make(chan struct{})

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 90})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-unconfirmed",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now:     func() time.Time { return registeredAt },
		launchBoundary: func(got time.Time) <-chan time.Time {
			if !got.Equal(launchBy) {
				t.Fatalf("launch boundary = %v, want %v", got, launchBy)
			}

			return boundary
		},
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				<-nativeDone
				completedAt := launchBy.Add(time.Nanosecond)
				completion := supervisorLaunchCompletion{
					generation: action.generation,
					action:     action.token,
					at:         completedAt,
					kind:       supervisorLaunchProvenNotReleased,
					failure:    LaunchFailed,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: completedAt, completion: &completion,
				}
			case supervisorRevokeLaunchRelease:
				return nil
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	launched := make(chan LaunchResult, 1)
	go func() {
		launched <- supervisor.Launch(Spec{
			Attempt: "driver-unconfirmed", Command: []string{"blocked-start"}, Dir: "/tmp",
			Profile: SerialProfile, Deadline: 10 * time.Second,
		})
	}()
	boundary <- launchBy
	select {
	case result := <-launched:
		if result != (LaunchUnconfirmed{Residual: ProspectiveUnresolved}) {
			t.Fatalf("launch = %#v, want prospective LaunchUnconfirmed", result)
		}
	case <-time.After(time.Second):
		t.Fatal("Launch remained blocked after LaunchBy")
	}

	close(nativeDone)
	deadline := time.After(time.Second)
	for {
		snapshot := shell.snapshot()
		if len(snapshot.admissions) == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("late not-released completion retained custody: %#v", snapshot)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestSupervisorDriverReleaseUnknownReturnsUnconfirmedAndDrainsAdoptedCustody(t *testing.T) {
	registeredAt := time.Unix(9_500, 0)
	releaseAt := registeredAt.Add(time.Millisecond)
	drainBy := releaseAt.Add(5 * time.Second)
	releaseErr := errors.New("resume release unknown")
	nextAt := releaseAt
	registered := false
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 96})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "driver-release-unknown", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time {
			if !registered {
				registered = true

				return registeredAt
			}
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			nextAt = nextAt.Add(time.Nanosecond)
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token, at: releaseAt,
					kind: supervisorLaunchReleaseUnconfirmed, diagnostic: 23,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releaseAt, drainBy: drainBy, completion: &completion,
				}
			case supervisorForceOwned:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{kind: supervisorDrainCompleted, generation: action.generation, at: nextAt, drain: &completion}
			case supervisorObserveEmptiness:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainObservedEmpty,
				}

				return &supervisorEvent{kind: supervisorDrainCompleted, generation: action.generation, at: nextAt, drain: &completion}
			case supervisorCaptureOutput:
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: nextAt, ref: 1,
				}

				return &supervisorEvent{kind: supervisorOutputCompleted, generation: action.generation, at: nextAt, output: &completion}
			case supervisorReleaseDomain:
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: nextAt,
				}

				return &supervisorEvent{kind: supervisorReleaseCompleted, generation: action.generation, at: nextAt, release: &completion}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			if ref != 23 {
				t.Fatalf("release diagnostic ref = %d, want 23", ref)
			}

			return releaseErr
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-release-unknown", Command: []string{"unknown-release"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	if result != (LaunchUnconfirmed{Residual: ProspectiveUnresolved}) {
		t.Fatalf("launch = %#v, want LaunchUnconfirmed", result)
	}
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: releaseAt.Add(time.Second), DrainBy: releaseAt.Add(6 * time.Second),
	})
	if _, ok := settlement.(SweepDrained); !ok {
		t.Fatalf("release-unknown settlement = %#v, want SweepDrained", settlement)
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeClosedDrained || len(snapshot.admissions) != 0 {
		t.Fatalf("release-unknown final runtime = %#v", snapshot)
	}
}

func TestSupervisorDriverAdoptsAndDrainsReleaseCompletedAfterLaunchBoundary(t *testing.T) {
	registeredAt := time.Unix(9_500, 0)
	launchBy := registeredAt.Add(time.Second)
	boundary := make(chan time.Time, 1)
	nativeDone := make(chan struct{})
	nextAt := launchBy
	registered := false

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 94})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-late-release",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			if !registered {
				registered = true

				return registeredAt
			}
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time {
			return boundary
		},
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				<-nativeDone
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorLaunchCompletion{
					generation: action.generation,
					action:     action.token,
					at:         nextAt,
					kind:       supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: nextAt, completion: &completion,
				}
			case supervisorRevokeLaunchRelease:
				return nil
			case supervisorForceOwned:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at: nextAt, kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: nextAt, drain: &completion,
				}
			case supervisorObserveEmptiness:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at: nextAt, kind: supervisorDrainObservedEmpty,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: nextAt, drain: &completion,
				}
			case supervisorCaptureOutput:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at: nextAt, ref: 1,
				}

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: nextAt, output: &completion,
				}
			case supervisorReleaseDomain:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at: nextAt,
				}

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: nextAt, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	launched := make(chan LaunchResult, 1)
	go func() {
		launched <- supervisor.Launch(Spec{
			Attempt: "driver-late-release", Command: []string{"late-release"}, Dir: "/tmp",
			Profile: SerialProfile, Deadline: 10 * time.Second,
		})
	}()
	boundary <- launchBy
	result := <-launched
	if result != (LaunchUnconfirmed{Residual: ProspectiveUnresolved}) {
		t.Fatalf("launch = %#v, want prospective LaunchUnconfirmed", result)
	}

	close(nativeDone)
	deadline := time.After(time.Second)
	for {
		driver.mutex.Lock()
		phase := driver.state.attempts[0].phase
		driver.mutex.Unlock()
		if phase == supervisorAwaitingEmergencySettlement {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("late released attempt did not await emergency settlement: phase=%d runtime=%#v",
				phase, shell.snapshot())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	emergencyAt := launchBy.Add(time.Second)
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
	})
	if _, ok := settlement.(SweepDrained); !ok {
		t.Fatalf("late adoption emergency settlement = %#v, want SweepDrained", settlement)
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeClosedDrained ||
		len(snapshot.admissions) != 0 {
		t.Fatalf("late adoption final runtime = %#v", snapshot)
	}
}

func TestSupervisorDriverDeliversOwnedAttemptWaitThroughPublicLifecycle(t *testing.T) {
	registeredAt := time.Unix(10_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(2 * time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 91})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-owned",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	executed := make([]supervisorActionKind, 0, 6)
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary:  func(time.Time) <-chan time.Time { return make(chan time.Time) },
		commandBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress:  time.Second,
		drainEpoch:      5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			executed = append(executed, action.kind)
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation,
					action:     action.token,
					at:         releasedAt,
					kind:       supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind:       supervisorLaunchCompleted,
					generation: action.generation,
					at:         releasedAt,
					completion: &completion,
				}
			case supervisorWaitRoot:
				nextAt = exitedAt
				fact := supervisorRunningFact{
					generation: action.generation,
					action:     action.token,
					kind:       supervisorRunningRootExited,
					at:         exitedAt,
					exitCode:   17,
				}

				return &supervisorEvent{
					kind:       supervisorRunningObserved,
					generation: action.generation,
					at:         exitedAt,
					drainBy:    drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation,
						waitAction: action.token,
						facts:      []supervisorRunningFact{fact},
					},
				}
			case supervisorObserveEmptiness:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at:   nextAt.Add(time.Nanosecond),
					kind: supervisorDrainObservedEmpty,
				}
				nextAt = completion.at

				return &supervisorEvent{
					kind:       supervisorDrainCompleted,
					generation: action.generation,
					at:         completion.at,
					drain:      &completion,
				}
			case supervisorCaptureOutput:
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at: nextAt.Add(time.Nanosecond), ref: 1, cutoff: 3, prefixLength: 3,
				}
				nextAt = completion.at

				return &supervisorEvent{
					kind:       supervisorOutputCompleted,
					generation: action.generation,
					at:         completion.at,
					output:     &completion,
				}
			case supervisorReleaseDomain:
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action: supervisorPendingAction{
						kind: action.kind, token: action.token,
					},
					at: nextAt.Add(time.Nanosecond),
				}
				nextAt = completion.at

				return &supervisorEvent{
					kind:       supervisorReleaseCompleted,
					generation: action.generation,
					at:         completion.at,
					release:    &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "bad" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			if attempt != grant.attempt {
				t.Fatalf("start attempt = %q, want %q", attempt, grant.attempt)
			}
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-owned", Command: []string{"false"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	launched := shell.snapshot()
	if len(launched.admissions) != 1 || launched.admissions[0].stage != admissionOwned {
		t.Fatalf("Owned published before runtime ownership: %#v", launched)
	}
	terminal := owned.Attempt.Wait()
	settled, ok := terminal.(Settled)
	if !ok {
		t.Fatalf("terminal = %#v, want Settled", terminal)
	}
	if settled.Exit != (ExitStatus{Code: 17}) || settled.Output.Bytes != "bad" ||
		settled.Deadline != 10*time.Second || settled.CommandDuration != 2*time.Second {
		t.Fatalf("settled evidence = %#v", settled)
	}
	wantActions := []supervisorActionKind{
		supervisorLaunchNative,
		supervisorWaitRoot,
		supervisorObserveEmptiness,
		supervisorCaptureOutput,
		supervisorReleaseDomain,
	}
	if !reflect.DeepEqual(executed, wantActions) {
		t.Fatalf("native actions = %v, want %v", executed, wantActions)
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeOpen || len(snapshot.admissions) != 0 {
		t.Fatalf("runtime after terminal delivery = %#v", snapshot)
	}
}

func TestSupervisorDriverDeliversDrainUnconfirmedAndEmergencyResidual(t *testing.T) {
	registeredAt := time.Unix(15_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	emergencyAt := drainBy.Add(2 * time.Nanosecond)
	nextAt := registeredAt.Add(-time.Nanosecond)
	observations := 0
	captureEntered := make(chan struct{})
	captureDone := make(chan struct{})

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 95})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-residual",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: exitedAt, exitCode: 19,
				}

				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: exitedAt, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorObserveEmptiness:
				observations++
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         drainBy, kind: supervisorDrainObservedResidual,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorForceOwned:
				if observations != 1 {
					t.Fatalf("force followed %d emptiness observations, want 1", observations)
				}
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         drainBy, kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorCaptureOutput:
				close(captureEntered)
				<-captureDone
				nextAt = emergencyAt.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, ref: 4, cutoff: 7, prefixLength: 7,
				}

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: completion.at, output: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "partial" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-residual", Command: []string{"residual"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	terminalReady := make(chan Terminal, 1)
	go func() { terminalReady <- owned.Attempt.Wait() }()
	<-captureEntered
	closure := shell.closeRuntime(runtimeFatalCause("emergency during residual output capture"))
	if len(closure.residual) != 1 {
		t.Fatalf("runtime closure = %#v, want one owned residual", closure)
	}
	settlementReady := make(chan SweepResult, 1)
	go func() {
		settlementReady <- supervisor.EmergencyDrain(EmergencyRequest{
			At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
		})
	}()
	deadline := time.After(time.Second)
	for {
		driver.mutex.Lock()
		emergencyActive := driver.state.emergency.active
		driver.mutex.Unlock()
		if emergencyActive {
			break
		}
		select {
		case <-deadline:
			t.Fatal("emergency did not snapshot in-flight output capture")
		default:
			runtime.Gosched()
		}
	}
	close(captureDone)
	terminal := <-terminalReady
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	if !ok || unconfirmed.Residual != OwnedUndrained || unconfirmed.Output.Bytes != "partial" ||
		unconfirmed.Output.Final {
		t.Fatalf("terminal = %#v, want partial DrainUnconfirmed", terminal)
	}
	settlement := <-settlementReady
	residuals, ok := settlement.(SweepUnconfirmed)
	if !ok || !reflect.DeepEqual(residuals.Residuals(), []ResidualRef{{
		Attempt: "driver-residual", Kind: OwnedUndrained,
	}}) {
		t.Fatalf("emergency settlement = %#v, want exact owned residual", settlement)
	}
}

func TestSupervisorDriverLocalResidualTransfersCustodyBeforeEmergencySweep(t *testing.T) {
	registeredAt := time.Unix(17_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 107})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "driver-local-residual", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)
			return nextAt
		},
		launchBoundary:  func(time.Time) <-chan time.Time { return make(chan time.Time) },
		commandBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress:  time.Second,
		drainEpoch:      5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}
				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: exitedAt, exitCode: 29,
				}
				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: exitedAt, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorObserveEmptiness:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         drainBy, kind: supervisorDrainObservedResidual,
				}
				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: drainBy, drain: &completion,
				}
			case supervisorForceOwned:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         drainBy, kind: supervisorDrainForceCompleted,
				}
				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: drainBy, drain: &completion,
				}
			case supervisorCaptureOutput:
				nextAt = drainBy.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, ref: 8, cutoff: 7, prefixLength: 7,
				}
				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: nextAt, output: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)
				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "partial" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-local-residual", Command: []string{"residual"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	terminal := owned.Attempt.Wait()
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	if !ok || unconfirmed.Residual != OwnedUndrained || unconfirmed.Output.Bytes != "partial" ||
		unconfirmed.Output.Final {
		t.Fatalf("terminal = %#v, want locally transferred DrainUnconfirmed", terminal)
	}
	select {
	case <-shell.runtimeEmergency():
	default:
		t.Fatal("local residual custody transfer did not broadcast runtime emergency")
	}
	snapshot := shell.snapshot()
	if snapshot.lifecycle != runtimeFatalClosing || len(snapshot.admissions) != 1 ||
		snapshot.admissions[0].disposition != dispositionCustodyTransferred {
		t.Fatalf("runtime after local residual transfer = %#v", snapshot)
	}
	emergencyAt := drainBy.Add(time.Second)
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
	})
	residuals, ok := settlement.(SweepUnconfirmed)
	if !ok || !reflect.DeepEqual(residuals.Residuals(), []ResidualRef{{
		Attempt: "driver-local-residual", Kind: OwnedUndrained,
	}}) {
		t.Fatalf("local residual emergency settlement = %#v", settlement)
	}
}

func TestSupervisorDriverPreservesIndependentWaitAndTerminationFailures(t *testing.T) {
	registeredAt := time.Unix(18_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	failedAt := releasedAt.Add(time.Second)
	drainBy := failedAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)
	waitErr := errors.New("root completion cell failed")
	terminationErr := errors.New("terminate execution domain")

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 96})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "driver-wait-diagnostic", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningObservationFailed, at: failedAt,
					source: supervisorObservationWait, diagnostic: 11,
				}

				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: failedAt, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorForceOwned:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         failedAt.Add(time.Nanosecond), kind: supervisorDrainForceCompleted,
					diagnostic: 12,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorObserveEmptiness:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         failedAt.Add(2 * time.Nanosecond), kind: supervisorDrainObservedEmpty,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorCaptureOutput:
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         failedAt.Add(3 * time.Nanosecond), ref: 5,
				}
				nextAt = completion.at

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: completion.at, output: &completion,
				}
			case supervisorReleaseDomain:
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         failedAt.Add(5 * time.Nanosecond),
				}

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: completion.at, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			switch ref {
			case 11:
				return waitErr
			case 12:
				return terminationErr
			default:
				t.Fatalf("diagnostic ref = %d, want 11 or 12", ref)
			}

			return nil
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-wait-diagnostic", Command: []string{"wait-failure"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	terminal := owned.Attempt.Wait()
	infrastructure, ok := terminal.(Infrastructure)
	if !ok || infrastructure.Cause != TerminationControlFailed ||
		!errors.Is(infrastructure.Err, terminationErr) ||
		infrastructure.Failures.Wait != waitErr.Error() ||
		infrastructure.Failures.Termination != terminationErr.Error() {
		t.Fatalf("terminal = %#v, want independent wait and primary termination failures", terminal)
	}
}

func TestSupervisorDriverPromotesConfirmedDrainCensusFailureToInfrastructure(t *testing.T) {
	registeredAt := time.Unix(18_500, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)
	drainErr := errors.New("execution domain census failed")
	observation := 0

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 98})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "driver-drain-census-diagnostic", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: exitedAt,
				}

				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: exitedAt, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorObserveEmptiness:
				observation++
				kind := supervisorDrainObservationFailed
				diagnostic := supervisorDiagnosticRef(31)
				if observation == 2 {
					kind = supervisorDrainObservedEmpty
					diagnostic = 0
				}
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         action.at.Add(time.Nanosecond), kind: kind, diagnostic: diagnostic,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorForceOwned:
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         action.at.Add(time.Nanosecond), kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorCaptureOutput:
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         action.at.Add(time.Nanosecond), ref: 7,
				}
				nextAt = completion.at

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: completion.at, output: &completion,
				}
			case supervisorReleaseDomain:
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         action.at.Add(time.Nanosecond),
				}

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: completion.at, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			if ref != 31 {
				t.Fatalf("diagnostic ref = %d, want 31", ref)
			}

			return drainErr
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-drain-census-diagnostic", Command: []string{"drain-census"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	terminal := owned.Attempt.Wait()
	infrastructure, ok := terminal.(Infrastructure)
	if !ok || infrastructure.Cause != CensusFailed || !errors.Is(infrastructure.Err, drainErr) ||
		infrastructure.Failures.DrainCensus != drainErr.Error() {
		t.Fatalf("terminal = %#v, want confirmed drain census infrastructure", terminal)
	}
}

func TestSupervisorDriverPromotesForceTimeWaitFailureToInfrastructure(t *testing.T) {
	terminal, waitErr, _ := runSupervisorDriverLateWaitFailure(t, false)
	infrastructure, ok := terminal.(Infrastructure)
	if !ok || infrastructure.Cause != WaitFailed || infrastructure.Err == nil ||
		infrastructure.Err.Error() != waitErr.Error() || infrastructure.Failures.Wait != waitErr.Error() {
		t.Fatalf("terminal = %#v, want force-time wait infrastructure", terminal)
	}
}

func TestSupervisorDriverPreservesWaitFailureThatArrivesAfterDrainBound(t *testing.T) {
	terminal, waitErr, terminationErr := runSupervisorDriverLateWaitFailure(t, true)
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	if !ok || unconfirmed.Failures.Wait != waitErr.Error() ||
		unconfirmed.Failures.Termination != terminationErr.Error() {
		t.Fatalf("terminal = %#v, want late wait and termination failures", terminal)
	}
}

func runSupervisorDriverLateWaitFailure(
	t *testing.T,
	afterDrainBound bool,
) (Terminal, error, error) {
	t.Helper()
	registeredAt := time.Now().Add(-2 * time.Second)
	releasedAt := registeredAt.Add(time.Millisecond)
	stopAt := releasedAt.Add(time.Second)
	drainBy := stopAt.Add(500 * time.Millisecond)
	if !afterDrainBound {
		drainBy = stopAt.Add(10 * time.Second)
	}
	nextAt := registeredAt.Add(-time.Nanosecond)
	waitErr := errors.New("root tracking failed during termination")
	terminationErr := errors.New("terminate execution domain")
	waitReleased := make(chan struct{})
	nativeWaitDone := make(chan struct{})
	nativeExecutor := &supervisorNativeExecutor{
		attempts: make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:  make(map[supervisorOutputRef]string), diagnostics: make(map[supervisorDiagnosticRef]error),
		forceDomain: func(nativePlatformState, int, time.Time) error {
			if afterDrainBound {
				return terminationErr
			}

			return nil
		},
		domainEmpty:    func(nativePlatformState, int) (bool, error) { return true, nil },
		readOutputFile: func(*os.File) (string, uint64, error) { return "", 0, nil },
	}

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 97})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "driver-independent-force-diagnostics", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary:  func(time.Time) <-chan time.Time { return make(chan time.Time) },
		commandBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress:  time.Second,
		drainEpoch:      5 * time.Second,
		recheckRoot: func(attemptGeneration) (ExitStatus, time.Time, bool, error) {
			return ExitStatus{}, time.Time{}, false, nil
		},
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				<-waitReleased
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: stopAt.Add(time.Nanosecond),
				}

				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: fact.at, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorForceOwned:
				nativeAttempt := &supervisorNativeAttempt{
					command: &exec.Cmd{Process: &os.Process{Pid: 123}},
					output:  &os.File{}, waitDone: nativeWaitDone, trackingErr: waitErr,
				}
				if !afterDrainBound {
					nativeAttempt.waitOnce.Do(func() {})
					close(nativeWaitDone)
				}
				nativeExecutor.attempts[action.generation] = nativeAttempt

				return nativeExecutor.force(action)
			case supervisorObserveEmptiness:
				if afterDrainBound {
					t.Fatalf("expired force unexpectedly observed emptiness")
				}

				return nativeExecutor.observeEmpty(action)
			case supervisorCaptureOutput:
				if afterDrainBound {
					close(nativeWaitDone)
				}
				event := nativeExecutor.captureOutput(action)
				nextAt = event.at

				return event
			case supervisorReleaseDomain:
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         action.at.Add(time.Nanosecond),
				}
				nextAt = completion.at

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: completion.at, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput:     nativeExecutor.readOutput,
		readDiagnostic: nativeExecutor.readDiagnostic,
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-independent-force-diagnostics", Command: []string{"stop"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	owned.Attempt.Stop(StopRequest{At: stopAt, DrainBy: drainBy})
	terminal := owned.Attempt.Wait()
	close(waitReleased)

	return terminal, waitErr, terminationErr
}

func TestSupervisorDriverDueAutomaticFuseBeatsLaterWaitFailureRegardlessOfReadiness(t *testing.T) {
	for iteration := range 100 {
		terminal, _ := runDueAutomaticFuseWaitFailureRace(t, iteration, time.Nanosecond)
		tripped, ok := terminal.(Tripped)
		if !ok {
			t.Fatalf("iteration %d terminal = %#v, want due FuseTrip instead of WaitFailed", iteration, terminal)
		}
		fuse, ok := tripped.Trip.(FuseTrip)
		if !ok || fuse.Live != 65 {
			t.Fatalf("iteration %d fuse evidence = %#v, want exact count", iteration, tripped)
		}
	}
}

func TestSupervisorDriverEqualAutomaticFuseRetainsWaitFailureDiagnostic(t *testing.T) {
	terminal, waitErr := runDueAutomaticFuseWaitFailureRace(t, 0, 0)
	tripped, ok := terminal.(Tripped)
	if !ok {
		t.Fatalf("equal-time terminal = %#v, want FuseTrip", terminal)
	}
	if fuse, ok := tripped.Trip.(FuseTrip); !ok || fuse.Live != 65 {
		t.Fatalf("equal-time fuse evidence = %#v, want exact count", tripped.Trip)
	}
	if tripped.Failures.Wait != waitErr.Error() {
		t.Fatalf("equal-time wait diagnostic = %q, want %q", tripped.Failures.Wait, waitErr)
	}
}

func TestSupervisorDriverRetainsAutomaticPeakThroughReadyDeadline(t *testing.T) {
	for iteration := range 100 {
		terminal := automaticDeadlineTerminalWithReadyPeak(t, iteration, 9, false)
		tripped, ok := terminal.(Tripped)
		if !ok {
			t.Fatalf("iteration %d terminal = %#v, want automatic deadline", iteration, terminal)
		}
		deadline, ok := tripped.Trip.(AutomaticDeadlineTrip)
		if !ok || deadline.Peak != (ObservedCount{Value: 9, Present: true}) {
			t.Fatalf("iteration %d deadline evidence = %#v, want inclusive-boundary peak 9", iteration, tripped)
		}
	}
}

func TestSupervisorDriverEqualAutomaticFuseBeatsReadyDeadline(t *testing.T) {
	for iteration := range 100 {
		terminal := automaticDeadlineTerminalWithReadyPeak(t, iteration, 65, false)
		tripped, ok := terminal.(Tripped)
		if !ok {
			t.Fatalf("iteration %d terminal = %#v, want equal-time fuse", iteration, terminal)
		}
		fuse, ok := tripped.Trip.(FuseTrip)
		if !ok || fuse.Live != 65 {
			t.Fatalf("iteration %d terminal = %#v, want equal-time fuse count 65", iteration, terminal)
		}
	}
}

func TestSupervisorDriverReadyEarlierRootExitBeatsDeadlineAndSamples(t *testing.T) {
	for iteration := range 100 {
		terminal := automaticDeadlineTerminalWithReadyPeak(t, iteration, 9, true)
		settled, ok := terminal.(Settled)
		if !ok || settled.Exit.Code != 23 {
			t.Fatalf("iteration %d terminal = %#v, want earlier root exit", iteration, terminal)
		}
	}
}

func automaticDeadlineTerminalWithReadyPeak(
	t *testing.T,
	iteration int,
	equalityLive uint64,
	readyRoot bool,
) Terminal {
	t.Helper()
	registeredAt := time.Unix(29_000+int64(iteration)*100, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	deadlineAt := releasedAt.Add(3 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)
	waitReleased := make(chan struct{})
	deadlines := make(chan time.Time, 1)
	if !readyRoot {
		deadlines <- deadlineAt
	}
	samples := make(chan time.Time, 3)
	samples <- releasedAt.Add(time.Second)
	samples <- releasedAt.Add(2 * time.Second)
	samples <- deadlineAt
	sampleCount := 0

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(400 + iteration)})
	attempt := attemptIdentity(fmt.Sprintf("driver-ready-deadline-peak-%d", iteration))
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: attempt, class: sharedAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary:  func(time.Time) <-chan time.Time { return make(chan time.Time) },
		commandBoundary: func(time.Time) <-chan time.Time { return deadlines },
		sampleTicks:     func() (<-chan time.Time, func()) { return samples, func() {} },
		launchProgress:  time.Second,
		drainEpoch:      5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				if !readyRoot {
					<-waitReleased
				} else {
					go func() {
						time.Sleep(time.Millisecond)
						deadlines <- deadlineAt
					}()
				}
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: deadlineAt.Add(time.Second), exitCode: 23,
				}
				if readyRoot {
					fact.at = releasedAt.Add(500 * time.Millisecond)
				}

				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: fact.at, drainBy: fact.at.Add(5 * time.Second),
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorForceOwned:
				nextAt = action.at.Add(time.Nanosecond)
				if readyRoot && !nextAt.After(deadlineAt) {
					nextAt = deadlineAt.Add(time.Nanosecond)
				}
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: nextAt, drain: &completion,
				}
			case supervisorObserveEmptiness:
				nextAt = action.at.Add(time.Nanosecond)
				if readyRoot && !nextAt.After(deadlineAt) {
					nextAt = deadlineAt.Add(time.Nanosecond)
				}
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainObservedEmpty,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: nextAt, drain: &completion,
				}
			case supervisorCaptureOutput:
				nextAt = action.at.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, ref: 1,
				}

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: nextAt, output: &completion,
				}
			case supervisorReleaseDomain:
				nextAt = action.at.Add(time.Nanosecond)
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: nextAt,
				}

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: nextAt, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		sampleRunning: func(attemptGeneration) (bool, uint64, error) {
			sampleCount++
			if sampleCount == 1 {
				return true, 3, nil
			}

			if sampleCount == 2 {
				return true, 7, nil
			}

			return true, equalityLive, nil
		},
		recheckRoot: func(attemptGeneration) (ExitStatus, time.Time, bool, error) {
			if readyRoot {
				return ExitStatus{Code: 23}, releasedAt.Add(500 * time.Millisecond), true, nil
			}

			return ExitStatus{}, time.Time{}, false, nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	result := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	).Launch(Spec{
		Attempt: string(attempt), Command: []string{"automatic-deadline"}, Dir: "/tmp",
		Profile: AutomaticProfile, Deadline: 3 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	terminalReady := make(chan Terminal, 1)
	go func() { terminalReady <- owned.Attempt.Wait() }()
	var terminal Terminal
	select {
	case terminal = <-terminalReady:
	case <-time.After(time.Second):
		t.Fatalf("iteration %d owned attempt wait did not settle", iteration)
	}
	close(waitReleased)

	return terminal
}

func runDueAutomaticFuseWaitFailureRace(
	t *testing.T,
	iteration int,
	waitOffset time.Duration,
) (Terminal, error) {
	t.Helper()
	registeredAt := time.Unix(19_000+int64(iteration)*100, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	sampleAt := releasedAt.Add(time.Second)
	waitFailedAt := sampleAt.Add(waitOffset)
	nextAt := registeredAt.Add(-time.Nanosecond)
	waitErr := errors.New("Darwin root tracking failed after automatic sample became due")
	waitReturned := make(chan struct{})
	samples := make(chan time.Time, 1)
	samples <- sampleAt

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(200 + iteration)})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: attemptIdentity(fmt.Sprintf("driver-fuse-wait-race-%d", iteration)),
		class: sharedAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)
			return nextAt
		},
		launchBoundary:  func(time.Time) <-chan time.Time { return make(chan time.Time) },
		commandBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		sampleTicks: func() (<-chan time.Time, func()) {
			<-waitReturned
			time.Sleep(time.Millisecond)
			return samples, func() {}
		},
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}
				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningObservationFailed, at: waitFailedAt,
					source: supervisorObservationWait, diagnostic: 1,
				}
				event := &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: waitFailedAt, drainBy: waitFailedAt.Add(5 * time.Second),
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
				close(waitReturned)
				return event
			case supervisorForceOwned:
				nextAt = action.at.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainForceCompleted,
				}
				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: nextAt, drain: &completion,
				}
			case supervisorObserveEmptiness:
				nextAt = action.at.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainObservedEmpty,
				}
				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: nextAt, drain: &completion,
				}
			case supervisorCaptureOutput:
				nextAt = action.at.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, ref: 1,
				}
				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: nextAt, output: &completion,
				}
			case supervisorReleaseDomain:
				nextAt = action.at.Add(time.Nanosecond)
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token}, at: nextAt,
				}
				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: nextAt, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)
				return nil
			}
		},
		sampleRunning: func(attemptGeneration) (bool, uint64, error) { return true, 65, nil },
		recheckRoot: func(attemptGeneration) (ExitStatus, time.Time, bool, error) {
			return ExitStatus{}, time.Time{}, false, nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			if ref != 1 {
				t.Fatalf("diagnostic ref = %d, want 1", ref)
			}
			return waitErr
		},
	})
	attempt := fmt.Sprintf("driver-fuse-wait-race-%d", iteration)
	result := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	).Launch(Spec{
		Attempt: attempt, Command: []string{"automatic"}, Dir: "/tmp",
		Profile: AutomaticProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}

	return owned.Attempt.Wait(), waitErr
}

func TestPublicTerminalPreservesEveryIndependentInfrastructureDiagnostic(t *testing.T) {
	diagnostics := map[supervisorDiagnosticRef]error{
		1: errors.New("wait diagnostic"),
		2: errors.New("running diagnostic"),
		3: errors.New("drain diagnostic"),
		4: errors.New("control diagnostic"),
		5: errors.New("output diagnostic"),
		6: errors.New("release diagnostic"),
	}
	for _, test := range []struct {
		name    string
		kind    supervisorTerminalKind
		cause   Cause
		primary supervisorDiagnosticRef
	}{
		{name: "wait", kind: supervisorTerminalInfrastructureWait, cause: WaitFailed, primary: 1},
		{name: "running census", kind: supervisorTerminalInfrastructureRunning, cause: CensusFailed, primary: 2},
		{name: "drain census", kind: supervisorTerminalInfrastructureRunning, cause: CensusFailed, primary: 3},
		{name: "termination", kind: supervisorTerminalInfrastructureControl, cause: TerminationControlFailed, primary: 4},
		{name: "output", kind: supervisorTerminalInfrastructureOutput, cause: OutputCaptureFailed, primary: 5},
		{name: "release", kind: supervisorTerminalInfrastructureRelease, cause: ReleaseFailed, primary: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			runningDiagnostic := supervisorDiagnosticRef(2)
			if test.name == "drain census" {
				runningDiagnostic = 0
			}
			terminal := publicTerminal(supervisorTerminalEvidence{
				kind:   test.kind,
				output: supervisorOutputEvidence{ref: 1, diagnostic: 5},
				diagnostics: supervisorTerminalDiagnostics{
					wait: 1, running: runningDiagnostic, drain: 3, control: 4, release: 6,
				},
			}, func(supervisorOutputRef) string { return "partial" }, func(ref supervisorDiagnosticRef) error {
				return diagnostics[ref]
			}, supervisorRuntimeAcknowledged)
			infrastructure, ok := terminal.(Infrastructure)
			if !ok || infrastructure.Cause != test.cause ||
				!errors.Is(infrastructure.Err, diagnostics[test.primary]) {
				t.Fatalf("terminal = %#v, want primary diagnostic %d", terminal, test.primary)
			}
			want := FailureDiagnostics{
				Wait: diagnostics[1].Error(), DrainCensus: diagnostics[3].Error(),
				Termination: diagnostics[4].Error(),
				Output:      diagnostics[5].Error(), Release: diagnostics[6].Error(),
			}
			if runningDiagnostic != 0 {
				want.RunningCensus = diagnostics[2].Error()
			}
			if infrastructure.Failures != want || infrastructure.Output.Bytes != "partial" {
				t.Fatalf("immutable diagnostics = %#v, want %#v", infrastructure, want)
			}
		})
	}
}

func TestSupervisorDriverDeliversEmergencyDrainWithoutOwnedWaiter(t *testing.T) {
	registeredAt := time.Unix(20_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	emergencyAt := releasedAt.Add(2 * time.Second)
	drainBy := emergencyAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)
	waitEntered := make(chan struct{})
	waitReleased := make(chan struct{})

	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 92})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-emergency",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	executed := make([]supervisorActionKind, 0, 5)
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now: func() time.Time {
			nextAt = nextAt.Add(time.Nanosecond)

			return nextAt
		},
		launchBoundary:  func(time.Time) <-chan time.Time { return make(chan time.Time) },
		commandBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress:  time.Second,
		drainEpoch:      5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			executed = append(executed, action.kind)
			switch action.kind {
			case supervisorLaunchNative:
				completion := supervisorLaunchCompletion{
					generation: action.generation, action: action.token,
					at: releasedAt, kind: supervisorLaunchReleased,
				}

				return &supervisorEvent{
					kind: supervisorLaunchCompleted, generation: action.generation,
					at: releasedAt, completion: &completion,
				}
			case supervisorWaitRoot:
				close(waitEntered)
				<-waitReleased
				fact := supervisorRunningFact{
					generation: action.generation, action: action.token,
					kind: supervisorRunningRootExited, at: emergencyAt,
				}
				return &supervisorEvent{
					kind: supervisorRunningObserved, generation: action.generation,
					at: emergencyAt, drainBy: drainBy,
					running: &supervisorRunningBundle{
						generation: action.generation, waitAction: action.token,
						facts: []supervisorRunningFact{fact},
					},
				}
			case supervisorForceOwned:
				close(waitReleased)
				nextAt = emergencyAt.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainForceCompleted,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorObserveEmptiness:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorDrainCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, kind: supervisorDrainObservedEmpty,
				}

				return &supervisorEvent{
					kind: supervisorDrainCompleted, generation: action.generation,
					at: completion.at, drain: &completion,
				}
			case supervisorCaptureOutput:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorOutputCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt, ref: 2, cutoff: 0, prefixLength: 0,
				}

				return &supervisorEvent{
					kind: supervisorOutputCompleted, generation: action.generation,
					at: completion.at, output: &completion,
				}
			case supervisorReleaseDomain:
				nextAt = nextAt.Add(time.Nanosecond)
				completion := supervisorReleaseCompletion{
					generation: action.generation,
					action:     supervisorPendingAction{kind: action.kind, token: action.token},
					at:         nextAt,
				}

				return &supervisorEvent{
					kind: supervisorReleaseCompleted, generation: action.generation,
					at: completion.at, release: &completion,
				}
			default:
				t.Fatalf("unexpected native action: %#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		recheckRoot: func(attemptGeneration) (ExitStatus, time.Time, bool, error) {
			return ExitStatus{}, time.Time{}, false, nil
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			if attempt != grant.attempt {
				t.Fatalf("start attempt = %q, want %q", attempt, grant.attempt)
			}
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-emergency", Command: []string{"sleep", "10"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 2 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	<-waitEntered
	closure := shell.closeRuntime(runtimeFatalCause("test emergency"))
	if len(closure.residual) != 1 || closure.residual[0].generation == 0 {
		t.Fatalf("runtime closure = %#v", closure)
	}

	settlement := supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	if _, ok := settlement.(SweepDrained); !ok {
		t.Fatalf("emergency settlement = %#v, want SweepDrained", settlement)
	}
	terminal := owned.Attempt.Wait()
	tripped, ok := terminal.(Tripped)
	if !ok || tripped.CommandDuration != 2*time.Second ||
		tripped.BoundFired != CommandDeadlineFired {
		t.Fatalf("emergency terminal = %#v, want inclusive command-deadline Tripped", terminal)
	}
	if _, ok := tripped.Trip.(SerialDeadlineTrip); !ok {
		t.Fatalf("emergency trip = %#v, want SerialDeadlineTrip", tripped.Trip)
	}
	wantActions := []supervisorActionKind{
		supervisorLaunchNative,
		supervisorWaitRoot,
		supervisorForceOwned,
		supervisorObserveEmptiness,
		supervisorCaptureOutput,
		supervisorReleaseDomain,
	}
	if !reflect.DeepEqual(executed, wantActions) {
		t.Fatalf("native actions = %v, want %v", executed, wantActions)
	}
	snapshot := shell.snapshot()
	if snapshot.lifecycle != runtimeClosedDrained || len(snapshot.admissions) != 0 {
		t.Fatalf("runtime after emergency settlement = %#v", snapshot)
	}
}
