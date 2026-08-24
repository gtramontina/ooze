package ooze

import (
	"reflect"
	"testing"
	"time"
)

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
	supervisor := newSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			if attempt != grant.attempt {
				t.Fatalf("start attempt = %q, want %q", attempt, grant.attempt)
			}
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver.launch,
	)

	result := supervisor.Launch(Spec{
		Attempt: "driver-owned", Command: []string{"false"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := result.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", result)
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
