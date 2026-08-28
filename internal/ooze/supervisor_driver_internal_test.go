package ooze

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorDriverEmergencyPreemptsCommittedStartBeforeRegistration(t *testing.T) {
	registeredAt := time.Unix(6_500, 0)
	emergencyAt := registeredAt.Add(500 * time.Millisecond)
	drainBy := emergencyAt.Add(5 * time.Second)

	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 87})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token, attempt: "committed-before-registration", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	nativeCalls := 0
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time { return registeredAt },
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			nativeCalls++
			assert.Fail(t, "pre-registered emergency executed native action", "action=%#v", action)

			return nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	spec := Spec{
		Attempt: "committed-before-registration", Command: []string{"blocked-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	}
	cell := processruntime.NewStartCell()
	driver.reserveLaunch(cell, spec)
	prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})
	closeRuntimeForTest(shell, runtimeFatalCause("committed start registration race"))

	settlement := driver.emergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	{
		_, ok := settlement.(SweepDrained)
		require.True(t, ok, "pre-registration emergency settlement = %#v, want SweepDrained", settlement)
	}
	launch := driver.launchManaged(prepared.start, spec)
	assert.Equal(t, (NotReleased{Kind: LaunchFailed}), launch.result, "preempted launch/calls = %#v/%d, want not released/zero", launch.result, nativeCalls)
	assert.EqualValues(t, 0, nativeCalls, "preempted launch/calls = %#v/%d, want not released/zero", launch.result, nativeCalls)
}

func TestSupervisorDriverDiscardsReservationWhenEmergencyPrecedesStartCommitment(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 86})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token, attempt: "emergency-before-start", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time { return time.Unix(6_000, 0) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			assert.Fail(t, "rejected start executed native action", "action=%#v", action)

			return nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	closeRuntimeForTest(shell, runtimeFatalCause("emergency before start commitment"))
	driver.emergencyStarted = true
	cell := processruntime.NewStartCell()
	spec := Spec{
		Attempt: "emergency-before-start", Command: []string{"never-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	}
	driver.reserveLaunch(cell, spec)
	prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})
	assert.Equal(t, processruntime.StartRejectedClosed, prepared.result.decision, "start decision = %v, want closed rejection", prepared.result.decision)
	driver.discardLaunch(cell)
	assert.EqualValues(t, 0, len(driver.reservations), "rejected reservation/cell = %#v/%d, want empty/zero", driver.reservations, cell.InstalledGeneration())
	assert.EqualValues(t, 0, cell.InstalledGeneration(), "rejected reservation/cell = %#v/%d, want empty/zero", driver.reservations, cell.InstalledGeneration())
}

func TestSupervisorDriverEmergencyDuringProspectiveLaunchReturnsUnconfirmedAndSettlesLateNoRelease(t *testing.T) {
	registeredAt := time.Unix(7_000, 0)
	launchBy := registeredAt.Add(time.Second)
	emergencyAt := registeredAt.Add(500 * time.Millisecond)
	drainBy := emergencyAt.Add(5 * time.Second)
	nativeStarted := make(chan struct{})
	nativeDone := make(chan struct{})
	boundary := make(chan time.Time)

	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 88})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-prospective-emergency",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now:     func() time.Time { return registeredAt },
		launchBoundary: func(got time.Time) <-chan time.Time {
			assert.True(t, got.Equal(launchBy), "launch boundary = %v, want %v", got, launchBy)

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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})

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
	closure := closeRuntimeForTest(shell, runtimeFatalCause("prospective launch emergency"))
	require.Len(t, closure.residual, 1, "runtime closure = %#v, want exact prospective generation", closure)
	assert.NotEqual(t, 0, closure.residual[0].generation, "runtime closure = %#v, want exact prospective generation", closure)
	settled := make(chan SweepResult, 1)
	go func() {
		settled <- supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	}()

	select {
	case result := <-launched:
		assert.Equal(t, (LaunchUnconfirmed{Residual: ProspectiveUnresolved}), result, "launch = %#v, want prospective LaunchUnconfirmed", result)
	case <-time.After(time.Second):
		require.FailNow(t, "pre-Owned emergency did not release the launch callback")
	}

	close(nativeDone)
	select {
	case settlement := <-settled:
		{
			_, ok := settlement.(SweepDrained)
			require.True(t, ok, "emergency settlement = %#v, want SweepDrained", settlement)
		}
	case <-time.After(time.Second):
		require.FailNow(t, "late no-release did not complete emergency settlement")
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
	observationStarted := make(chan struct{})
	allowObservation := make(chan struct{})
	blocked := false
	shell := newProcessRuntimeShellWithObserver(2, processruntime.ObserverFunc(func(event processruntime.RecordedCut) {
		if event.Operation() != processruntime.ObserveAttemptOperation ||
			!event.Result().Receipt().SettlementAcknowledged() || blocked {
			return
		}
		blocked = true
		close(observationStarted)
		<-allowObservation

	}))
	grants := make(map[attemptIdentity]admissionGrant)
	for index, attempt := range []attemptIdentity{"multi-prospective-a", "multi-prospective-b"} {
		campaign := registerCampaignForTest(shell, campaignProvenance{lineage: campaignLineage(120 + index)})
		requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			grant, ok := grants[attempt]
			assert.True(t, ok, "unexpected attempt %q", attempt)

			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
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
	closure := closeRuntimeForTest(shell, runtimeFatalCause("multi prospective emergency"))
	assert.EqualValues(t, 2, len(closure.residual), "runtime closure residuals = %#v, want two prospective generations", closure.residual)
	settled := make(chan SweepResult, 1)
	go func() {
		settled <- supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	}()
	for range 2 {
		select {
		case result := <-results:
			assert.Equal(t, (LaunchUnconfirmed{Residual: ProspectiveUnresolved}), result, "launch = %#v, want LaunchUnconfirmed", result)
		case <-time.After(time.Second):
			require.FailNow(t, "multi-prospective emergency did not release every callback")
		}
	}
	for _, launch := range pending {
		close(launch.release)
	}
	select {
	case <-observationStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "prospective close did not reach runtime publication")
	}
	closuresPending := assert.Eventually(t, func() bool {
		driver.mutex.Lock()
		defer driver.mutex.Unlock()
		closing := 0
		for _, attempt := range driver.supervisorState().attempts {
			if attempt.phase == supervisorClosingProspective {
				closing++
			}
		}

		return closing != 0
	}, time.Second, time.Millisecond, "prospective closes did not wait for their runtime receipts")
	settledEarly := false
	select {
	case <-settled:
		settledEarly = true
	default:
	}
	close(allowObservation)
	require.True(t, closuresPending, "prospective close receipt boundary was not reached")
	require.False(t, settledEarly, "emergency settled before prospective close receipts")
	select {
	case settlement := <-settled:
		{
			_, ok := settlement.(SweepDrained)
			require.True(t, ok, "settlement = %#v, want SweepDrained", settlement)
		}
	case <-time.After(time.Second):
		require.FailNow(t, "equality no-release completions did not settle one emergency epoch")
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 87})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})

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
			require.FailNow(t, "released completion was not published while launch callback was held")
		default:
			runtime.Gosched()
		}
	}
	closure := closeRuntimeForTest(shell, runtimeFatalCause("released prospective emergency"))
	require.Len(t, closure.residual, 1, "runtime closure = %#v, want exact prospective generation", closure)
	assert.NotEqual(t, 0, closure.residual[0].generation, "runtime closure = %#v, want exact prospective generation", closure)
	settled := make(chan SweepResult, 1)
	go func() {
		settled <- supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	}()
	<-rootSnapshot
	close(allowBoundaryReturn)

	select {
	case result := <-launched:
		owned, ok := result.(Owned)
		require.True(t, ok, "launch = %#v, want emergency-adopted Owned", result)
		require.NotNil(t, owned.Attempt, "launch = %#v, want emergency-adopted Owned", result)
	case <-time.After(time.Second):
		require.FailNow(t, "released pre-Owned emergency did not release the launch callback")
	}
	select {
	case settlement := <-settled:
		{
			_, ok := settlement.(SweepDrained)
			require.True(t, ok, "emergency settlement = %#v, want SweepDrained", settlement)
		}
	case <-time.After(time.Second):
		require.FailNow(t, "released pre-Owned emergency did not settle")
	}
}

func TestSupervisorDriverBoundarySnapshotIncludesAlreadyPublishedEqualityCompletion(t *testing.T) {
	registeredAt := time.Unix(8_000, 0)
	launchBy := registeredAt.Add(time.Second)
	launchErr := errors.New("typed launch failure")
	completionReturned := make(chan struct{})

	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 89})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
			assert.EqualValues(t, 19, ref, "launch diagnostic ref = %d, want 19", ref)

			return launchErr
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-equality", Command: []string{"equal-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	notReleased, ok := result.(NotReleased)
	require.True(t, ok, "launch = %#v, want equality NotReleased", result)
	assert.Equal(t, LaunchFailed, notReleased.Kind, "launch = %#v, want equality NotReleased", result)
	assert.ErrorIs(t, notReleased.Err, launchErr, "launch = %#v, want equality NotReleased", result)
}

func TestSupervisorLaunchBoundaryUsesTheAbsoluteLaunchBy(t *testing.T) {
	launchBy := time.Now().Add(20 * time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	started := time.Now()
	<-waitForSupervisorLaunchBoundary(launchBy)
	{
		elapsed := time.Since(started)
		assert.False(t, elapsed > 20*time.Millisecond, "expired absolute launch boundary waited %v", elapsed)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 101})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)
				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-monitor-before-wait", Command: []string{"managed"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	select {
	case <-monitorStarted:
	case <-time.After(time.Second):
		require.FailNow(t, "owned root monitoring did not start before Wait")
	}
	close(allowExit)
	{
		terminal := owned.Attempt.Wait()
		assert.NotNil(t, terminal, "owned attempt returned no terminal after autonomous monitor completion")
	}
}

func TestSupervisorDriverReleasesRecorderOwnerCutBeforeNativeAction(t *testing.T) {
	recorder := newSimulationRecorder()
	fixture := newRunningReducerFixture(t, SerialProfile)
	reentered := make(chan struct{})
	returned := make(chan struct{})
	driver := &supervisorDriver{
		machine: newSupervisorMachineFrom(fixture.state), observer: recorder, ownerSequence: &recorder.next,
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
		require.FailNow(t, "native action could not enter a recorder owner cut")
	}
	<-returned
}

func TestSupervisorDriverReportsUnconfirmedAtLaunchBoundaryAndClosesLateNotReleased(t *testing.T) {
	registeredAt := time.Unix(9_000, 0)
	launchBy := registeredAt.Add(time.Second)
	boundary := make(chan time.Time, 1)
	nativeDone := make(chan struct{})

	lateSettlement := make(chan struct{}, 1)
	shell := newProcessRuntimeShellWithObserver(1, processruntime.ObserverFunc(func(event processruntime.RecordedCut) {
		if event.Operation() == processruntime.ObserveAttemptOperation &&
			event.Result().Receipt().SettlementAcknowledged() {
			lateSettlement <- struct{}{}
		}
	}))
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 90})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token,
		attempt:  "driver-unconfirmed",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery

	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell,
		now:     func() time.Time { return registeredAt },
		launchBoundary: func(got time.Time) <-chan time.Time {
			assert.True(t, got.Equal(launchBy), "launch boundary = %v, want %v", got, launchBy)

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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})

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
		assert.Equal(t, (LaunchUnconfirmed{Residual: ProspectiveUnresolved}), result, "launch = %#v, want prospective LaunchUnconfirmed", result)
	case <-time.After(time.Second):
		require.FailNow(t, "Launch remained blocked after LaunchBy")
	}

	close(nativeDone)
	select {
	case <-lateSettlement:
	case <-time.After(time.Second):
		require.FailNow(t, "late not-released completion did not settle runtime custody")
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 96})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			assert.EqualValues(t, 23, ref, "release diagnostic ref = %d, want 23", ref)

			return releaseErr
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-release-unknown", Command: []string{"unknown-release"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	assert.Equal(t, (LaunchUnconfirmed{Residual: ProspectiveUnresolved}), result, "launch = %#v, want LaunchUnconfirmed", result)
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: releaseAt.Add(time.Second), DrainBy: releaseAt.Add(6 * time.Second),
	})
	{
		_, ok := settlement.(SweepDrained)
		require.True(t, ok, "release-unknown settlement = %#v, want SweepDrained", settlement)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 94})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})

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
	assert.Equal(t, (LaunchUnconfirmed{Residual: ProspectiveUnresolved}), result, "launch = %#v, want prospective LaunchUnconfirmed", result)

	close(nativeDone)
	deadline := time.After(time.Second)
	for {
		driver.mutex.Lock()
		phase := driver.supervisorState().attempts[0].phase
		driver.mutex.Unlock()
		if phase == supervisorAwaitingEmergencySettlement {
			break
		}
		select {
		case <-deadline:
			require.FailNowf(t, "late released attempt did not await emergency settlement", "phase=%d", phase)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	emergencyAt := launchBy.Add(time.Second)
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
	})
	{
		_, ok := settlement.(SweepDrained)
		require.True(t, ok, "late adoption emergency settlement = %#v, want SweepDrained", settlement)
	}
}

func TestSupervisorDriverDeliversOwnedAttemptWaitThroughPublicLifecycle(t *testing.T) {
	registeredAt := time.Unix(10_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(2 * time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)

	ownedAccepted := make(chan attemptGeneration, 1)
	shell := newProcessRuntimeShellWithObserver(1, processruntime.ObserverFunc(func(event processruntime.RecordedCut) {
		if event.Operation() == processruntime.ObserveAttemptOperation &&
			!event.Result().Receipt().SettlementAcknowledged() {
			ownedAccepted <- event.Result().Receipt().Generation()
		}
	}))
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 91})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "bad" },
	})
	var generation attemptGeneration
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			assert.Equal(t, grant.attempt, attempt, "start attempt = %q, want %q", attempt, grant.attempt)
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})
			generation = prepared.result.generation

			return prepared.start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-owned", Command: []string{"false"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	select {
	case observed := <-ownedAccepted:
		assert.Equal(t, generation, observed)
	default:
		require.FailNow(t, "Owned published before runtime ownership")
	}
	terminal := owned.Attempt.Wait()
	settled, ok := terminal.(Settled)
	require.True(t, ok, "terminal = %#v, want Settled", terminal)
	assert.Equal(t, (ExitStatus{Code: 17}), settled.Exit, "settled evidence = %#v", settled)
	assert.EqualValues(t, "bad", settled.Output.Bytes, "settled evidence = %#v", settled)
	assert.Equal(t, 10*time.Second, settled.Deadline, "settled evidence = %#v", settled)
	assert.Equal(t, 2*time.Second, settled.CommandDuration, "settled evidence = %#v", settled)
	wantActions := []supervisorActionKind{
		supervisorLaunchNative,
		supervisorWaitRoot,
		supervisorObserveEmptiness,
		supervisorCaptureOutput,
		supervisorReleaseDomain,
	}
	assert.Equal(t, wantActions, executed, "native actions = %v, want %v", executed, wantActions)
}

func TestSupervisorDriverPublishesOwnerCutsOutsideItsLock(t *testing.T) {
	observer := &blockingSupervisionObserver{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	driver := &supervisorDriver{machine: newSupervisorMachine(), observer: observer}
	done := make(chan struct{})
	go func() {
		driver.reduce(supervisorEvent{
			kind: supervisorProspectiveRegistered, generation: 1, attempt: "attempt-a",
			at: time.Unix(100, 0), launchBy: time.Unix(101, 0),
			profile: AutomaticProfile, commandDeadline: time.Minute,
		})
		close(done)
	}()
	<-observer.started
	locked := make(chan struct{})
	go func() {
		driver.mutex.Lock()
		locked <- struct{}{}
		driver.mutex.Unlock()
	}()

	select {
	case <-locked:
	case <-time.After(100 * time.Millisecond):
		close(observer.release)
		<-done
		require.Fail(t, "owner-cut observer ran while the supervisor lock was held")
	}
	close(observer.release)
	<-done
}

type blockingSupervisionObserver struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingSupervisionObserver) Enter() func() {
	return func() {}
}

func (observer *blockingSupervisionObserver) Publish(
	supervisionOwnerCutReservation,
	supervisionFact,
	supervisorDomainEvent,
	supervisionProjection,
	[]supervisionEffect,
) {
	close(observer.started)
	<-observer.release
}

func (*blockingSupervisionObserver) Complete(supervisionEffect) {}

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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 95})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.EqualValues(t, 1, observations, "force followed %d emptiness observations, want 1", observations)
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "partial" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-residual", Command: []string{"residual"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	terminalReady := make(chan Terminal, 1)
	go func() { terminalReady <- owned.Attempt.Wait() }()
	<-captureEntered
	closure := closeRuntimeForTest(shell, runtimeFatalCause("emergency during residual output capture"))
	assert.EqualValues(t, 1, len(closure.residual), "runtime closure = %#v, want one owned residual", closure)
	settlementReady := make(chan SweepResult, 1)
	go func() {
		settlementReady <- supervisor.EmergencyDrain(EmergencyRequest{
			At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
		})
	}()
	deadline := time.After(time.Second)
	for {
		driver.mutex.Lock()
		emergencyActive := driver.supervisorState().emergency.active
		driver.mutex.Unlock()
		if emergencyActive {
			break
		}
		select {
		case <-deadline:
			require.FailNow(t, "emergency did not snapshot in-flight output capture")
		default:
			runtime.Gosched()
		}
	}
	close(captureDone)
	terminal := <-terminalReady
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	require.True(t, ok, "terminal = %#v, want partial DrainUnconfirmed", terminal)
	assert.Equal(t, OwnedUndrained, unconfirmed.Residual, "terminal = %#v, want partial DrainUnconfirmed", terminal)
	assert.EqualValues(t, "partial", unconfirmed.Output.Bytes, "terminal = %#v, want partial DrainUnconfirmed", terminal)
	assert.False(t, unconfirmed.Output.Final, "terminal = %#v, want partial DrainUnconfirmed", terminal)
	settlement := <-settlementReady
	residuals, ok := settlement.(SweepUnconfirmed)
	require.True(t, ok, "emergency settlement = %#v, want exact owned residual", settlement)
	assert.Equal(t, []ResidualRef{{
		Attempt: "driver-residual", Kind: OwnedUndrained,
	}}, residuals.Residuals(), "emergency settlement = %#v, want exact owned residual", settlement)
}

func TestSupervisorDriverLocalResidualTransfersCustodyBeforeEmergencySweep(t *testing.T) {
	registeredAt := time.Unix(17_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	exitedAt := releasedAt.Add(time.Second)
	drainBy := exitedAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)

	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 107})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)
				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "partial" },
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-local-residual", Command: []string{"residual"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	terminal := owned.Attempt.Wait()
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	require.True(t, ok, "terminal = %#v, want locally transferred DrainUnconfirmed", terminal)
	assert.Equal(t, OwnedUndrained, unconfirmed.Residual, "terminal = %#v, want locally transferred DrainUnconfirmed", terminal)
	assert.EqualValues(t, "partial", unconfirmed.Output.Bytes, "terminal = %#v, want locally transferred DrainUnconfirmed", terminal)
	assert.False(t, unconfirmed.Output.Final, "terminal = %#v, want locally transferred DrainUnconfirmed", terminal)
	require.NotZero(t, shell.FatalEpoch(), "local residual custody transfer did not start runtime emergency")
	emergencyAt := drainBy.Add(time.Second)
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
	})
	residuals, ok := settlement.(SweepUnconfirmed)
	require.True(t, ok, "local residual emergency settlement = %#v", settlement)
	assert.Equal(t, []ResidualRef{{
		Attempt: "driver-local-residual", Kind: OwnedUndrained,
	}}, residuals.Residuals(), "local residual emergency settlement = %#v", settlement)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 96})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

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
				assert.Fail(t, "unexpected diagnostic reference", "ref=%d, want 11 or 12", ref)
			}

			return nil
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-wait-diagnostic", Command: []string{"wait-failure"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	terminal := owned.Attempt.Wait()
	infrastructure, ok := terminal.(Infrastructure)
	require.True(t, ok, "terminal = %#v, want independent wait and primary termination failures", terminal)
	assert.Equal(t, TerminationControlFailed, infrastructure.Cause, "terminal = %#v, want independent wait and primary termination failures", terminal)
	assert.ErrorIs(t, infrastructure.Err, terminationErr, "terminal = %#v, want independent wait and primary termination failures", terminal)
	assert.Equal(t, waitErr.Error(), infrastructure.Failures.Wait, "terminal = %#v, want independent wait and primary termination failures", terminal)
	assert.Equal(t, terminationErr.Error(), infrastructure.Failures.Termination, "terminal = %#v, want independent wait and primary termination failures", terminal)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 98})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			assert.EqualValues(t, 31, ref, "diagnostic ref = %d, want 31", ref)

			return drainErr
		},
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-drain-census-diagnostic", Command: []string{"drain-census"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	terminal := owned.Attempt.Wait()
	infrastructure, ok := terminal.(Infrastructure)
	require.True(t, ok, "terminal = %#v, want confirmed drain census infrastructure", terminal)
	assert.Equal(t, CensusFailed, infrastructure.Cause, "terminal = %#v, want confirmed drain census infrastructure", terminal)
	assert.ErrorIs(t, infrastructure.Err, drainErr, "terminal = %#v, want confirmed drain census infrastructure", terminal)
	assert.Equal(t, drainErr.Error(), infrastructure.Failures.DrainCensus, "terminal = %#v, want confirmed drain census infrastructure", terminal)
}

func TestSupervisorDriverPromotesForceTimeWaitFailureToInfrastructure(t *testing.T) {
	terminal, waitErr, _ := runSupervisorDriverLateWaitFailure(t, false)
	infrastructure, ok := terminal.(Infrastructure)
	require.True(t, ok, "terminal = %#v, want force-time wait infrastructure", terminal)
	assert.Equal(t, WaitFailed, infrastructure.Cause, "terminal = %#v, want force-time wait infrastructure", terminal)
	require.Error(t, infrastructure.Err, "terminal = %#v, want force-time wait infrastructure", terminal)
	assert.Equal(t, waitErr.Error(), infrastructure.Err.Error(), "terminal = %#v, want force-time wait infrastructure", terminal)
	assert.Equal(t, waitErr.Error(), infrastructure.Failures.Wait, "terminal = %#v, want force-time wait infrastructure", terminal)
}

func TestSupervisorDriverPreservesWaitFailureThatArrivesAfterDrainBound(t *testing.T) {
	terminal, waitErr, terminationErr := runSupervisorDriverLateWaitFailure(t, true)
	unconfirmed, ok := terminal.(DrainUnconfirmed)
	require.True(t, ok, "terminal = %#v, want late wait and termination failures", terminal)
	assert.Equal(t, waitErr.Error(), unconfirmed.Failures.Wait, "terminal = %#v, want late wait and termination failures", terminal)
	assert.Equal(t, terminationErr.Error(), unconfirmed.Failures.Termination, "terminal = %#v, want late wait and termination failures", terminal)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 97})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.False(t, afterDrainBound, "expired force unexpectedly observed emptiness")

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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

				return nil
			}
		},
		readOutput:     nativeExecutor.readOutput,
		readDiagnostic: nativeExecutor.readDiagnostic,
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "driver-independent-force-diagnostics", Command: []string{"stop"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	owned.Attempt.Stop(StopRequest{At: stopAt, DrainBy: drainBy})
	terminal := owned.Attempt.Wait()
	close(waitReleased)

	return terminal, waitErr, terminationErr
}

func TestSupervisorDriverDueAutomaticFuseBeatsLaterWaitFailureRegardlessOfReadiness(t *testing.T) {
	for iteration := range 100 {
		terminal, _ := runDueAutomaticFuseWaitFailureRace(t, iteration, time.Nanosecond)
		tripped, ok := terminal.(Tripped)
		require.True(t, ok, "iteration %d terminal = %#v, want due FuseTrip instead of WaitFailed", iteration, terminal)
		fuse, ok := tripped.Trip.(FuseTrip)
		require.True(t, ok, "iteration %d fuse evidence = %#v, want exact count", iteration, tripped)
		assert.EqualValues(t, 65, fuse.Live, "iteration %d fuse evidence = %#v, want exact count", iteration, tripped)
	}
}

func TestSupervisorDriverEqualAutomaticFuseRetainsWaitFailureDiagnostic(t *testing.T) {
	terminal, waitErr := runDueAutomaticFuseWaitFailureRace(t, 0, 0)
	tripped, ok := terminal.(Tripped)
	require.True(t, ok, "equal-time terminal = %#v, want FuseTrip", terminal)
	{
		fuse, ok := tripped.Trip.(FuseTrip)
		require.True(t, ok, "equal-time fuse evidence = %#v, want exact count", tripped.Trip)
		assert.EqualValues(t, 65, fuse.Live, "equal-time fuse evidence = %#v, want exact count", tripped.Trip)
	}
	assert.Equal(t, waitErr.Error(), tripped.Failures.Wait, "equal-time wait diagnostic = %q, want %q", tripped.Failures.Wait, waitErr)
}

func TestSupervisorDriverRetainsAutomaticPeakThroughReadyDeadline(t *testing.T) {
	for iteration := range 100 {
		terminal := automaticDeadlineTerminalWithReadyPeak(t, iteration, 9, false)
		tripped, ok := terminal.(Tripped)
		require.True(t, ok, "iteration %d terminal = %#v, want automatic deadline", iteration, terminal)
		deadline, ok := tripped.Trip.(AutomaticDeadlineTrip)
		require.True(t, ok, "iteration %d deadline evidence = %#v, want inclusive-boundary peak 9", iteration, tripped)
		assert.Equal(t, (ObservedCount{Value: 9, Present: true}), deadline.Peak, "iteration %d deadline evidence = %#v, want inclusive-boundary peak 9", iteration, tripped)
	}
}

func TestSupervisorDriverEqualAutomaticFuseBeatsReadyDeadline(t *testing.T) {
	for iteration := range 100 {
		terminal := automaticDeadlineTerminalWithReadyPeak(t, iteration, 65, false)
		tripped, ok := terminal.(Tripped)
		require.True(t, ok, "iteration %d terminal = %#v, want equal-time fuse", iteration, terminal)
		fuse, ok := tripped.Trip.(FuseTrip)
		require.True(t, ok, "iteration %d terminal = %#v, want equal-time fuse count 65", iteration, terminal)
		assert.EqualValues(t, 65, fuse.Live, "iteration %d terminal = %#v, want equal-time fuse count 65", iteration, terminal)
	}
}

func TestSupervisorDriverReadyEarlierRootExitBeatsDeadlineAndSamples(t *testing.T) {
	for iteration := range 100 {
		terminal := automaticDeadlineTerminalWithReadyPeak(t, iteration, 9, true)
		settled, ok := terminal.(Settled)
		require.True(t, ok, "iteration %d terminal = %#v, want earlier root exit", iteration, terminal)
		assert.EqualValues(t, 23, settled.Exit.Code, "iteration %d terminal = %#v, want earlier root exit", iteration, terminal)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: campaignLineage(400 + iteration)})
	attempt := attemptIdentity(fmt.Sprintf("driver-ready-deadline-peak-%d", iteration))
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

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
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	).Launch(Spec{
		Attempt: string(attempt), Command: []string{"automatic-deadline"}, Dir: "/tmp",
		Profile: AutomaticProfile, Deadline: 3 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	terminalReady := make(chan Terminal, 1)
	go func() { terminalReady <- owned.Attempt.Wait() }()
	var terminal Terminal
	select {
	case terminal = <-terminalReady:
	case <-time.After(time.Second):
		require.FailNowf(t, "owned attempt wait did not settle", "iteration %d", iteration)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: campaignLineage(200 + iteration)})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)
				return nil
			}
		},
		sampleRunning: func(attemptGeneration) (bool, uint64, error) { return true, 65, nil },
		recheckRoot: func(attemptGeneration) (ExitStatus, time.Time, bool, error) {
			return ExitStatus{}, time.Time{}, false, nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
		readDiagnostic: func(ref supervisorDiagnosticRef) error {
			assert.EqualValues(t, 1, ref, "diagnostic ref = %d, want 1", ref)
			return waitErr
		},
	})
	attempt := fmt.Sprintf("driver-fuse-wait-race-%d", iteration)
	result := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	).Launch(Spec{
		Attempt: attempt, Command: []string{"automatic"}, Dir: "/tmp",
		Profile: AutomaticProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)

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
			require.True(t, ok, "terminal = %#v, want primary diagnostic %d", terminal, test.primary)
			assert.Equal(t, test.cause, infrastructure.Cause, "terminal = %#v, want primary diagnostic %d", terminal, test.primary)
			assert.ErrorIs(t, infrastructure.Err, diagnostics[test.primary], "terminal = %#v, want primary diagnostic %d", terminal, test.primary)
			want := FailureDiagnostics{
				Wait: diagnostics[1].Error(), DrainCensus: diagnostics[3].Error(),
				Termination: diagnostics[4].Error(),
				Output:      diagnostics[5].Error(), Release: diagnostics[6].Error(),
			}
			if runningDiagnostic != 0 {
				want.RunningCensus = diagnostics[2].Error()
			}
			assert.Equal(t, want, infrastructure.Failures, "immutable diagnostics = %#v, want %#v", infrastructure, want)
			assert.EqualValues(t, "partial", infrastructure.Output.Bytes, "immutable diagnostics = %#v, want %#v", infrastructure, want)
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 92})
	requested := requestAdmissionForTest(shell, admissionRequest{
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
				assert.Fail(t, "unexpected native action", "action=%#v", action)

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
			assert.Equal(t, grant.attempt, attempt, "start attempt = %q, want %q", attempt, grant.attempt)
			prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-emergency", Command: []string{"sleep", "10"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 2 * time.Second,
	})
	owned, ok := result.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", result)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", result)
	<-waitEntered
	closure := closeRuntimeForTest(shell, runtimeFatalCause("test emergency"))
	require.Len(t, closure.residual, 1, "runtime closure = %#v", closure)
	assert.NotEqual(t, 0, closure.residual[0].generation, "runtime closure = %#v", closure)

	settlement := supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	{
		_, ok := settlement.(SweepDrained)
		require.True(t, ok, "emergency settlement = %#v, want SweepDrained", settlement)
	}
	terminal := owned.Attempt.Wait()
	tripped, ok := terminal.(Tripped)
	require.True(t, ok, "emergency terminal = %#v, want inclusive command-deadline Tripped", terminal)
	assert.Equal(t, 2*time.Second, tripped.CommandDuration, "emergency terminal = %#v, want inclusive command-deadline Tripped", terminal)
	assert.Equal(t, CommandDeadlineFired, tripped.BoundFired, "emergency terminal = %#v, want inclusive command-deadline Tripped", terminal)
	{
		_, ok := tripped.Trip.(SerialDeadlineTrip)
		require.True(t, ok, "emergency trip = %#v, want SerialDeadlineTrip", tripped.Trip)
	}
	wantActions := []supervisorActionKind{
		supervisorLaunchNative,
		supervisorWaitRoot,
		supervisorForceOwned,
		supervisorObserveEmptiness,
		supervisorCaptureOutput,
		supervisorReleaseDomain,
	}
	assert.Equal(t, wantActions, executed, "native actions = %v, want %v", executed, wantActions)
}
