package ooze

import (
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestSupervisorDriverBoundarySnapshotIncludesAlreadyPublishedEqualityCompletion(t *testing.T) {
	registeredAt := time.Unix(8_000, 0)
	launchBy := registeredAt.Add(time.Second)
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
			}
			close(completionReturned)

			return &supervisorEvent{
				kind: supervisorLaunchCompleted, generation: action.generation,
				at: launchBy, completion: &completion,
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

	result := supervisor.Launch(Spec{
		Attempt: "driver-equality", Command: []string{"equal-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	notReleased, ok := result.(NotReleased)
	if !ok || notReleased.Kind != LaunchFailed {
		t.Fatalf("launch = %#v, want equality NotReleased", result)
	}
	if snapshot := shell.snapshot(); len(snapshot.admissions) != 0 {
		t.Fatalf("equality completion retained prospective custody: %#v", snapshot)
	}
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
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
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

func TestSupervisorDriverDeliversEmergencyDrainWithoutOwnedWaiter(t *testing.T) {
	registeredAt := time.Unix(20_000, 0)
	releasedAt := registeredAt.Add(time.Millisecond)
	emergencyAt := releasedAt.Add(2 * time.Second)
	drainBy := emergencyAt.Add(5 * time.Second)
	nextAt := registeredAt.Add(-time.Nanosecond)

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
		launchProgress: time.Second,
		drainEpoch:     5 * time.Second,
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
			case supervisorForceOwned:
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
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
	}
	closure := shell.closeRuntime(runtimeFatalCause("test emergency"))
	if len(closure.residual) != 1 || closure.residual[0].generation == 0 {
		t.Fatalf("runtime closure = %#v", closure)
	}

	settlement := supervisor.EmergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	if _, ok := settlement.(SweepDrained); !ok {
		t.Fatalf("emergency settlement = %#v, want SweepDrained", settlement)
	}
	terminal := owned.Attempt.Wait()
	stopped, ok := terminal.(Stopped)
	if !ok || stopped.CommandDuration != emergencyAt.Sub(releasedAt) ||
		stopped.BoundFired != NoBoundFired {
		t.Fatalf("emergency terminal = %#v, want runtime-emergency Stopped", terminal)
	}
	wantActions := []supervisorActionKind{
		supervisorLaunchNative,
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
