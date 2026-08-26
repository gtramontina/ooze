package ooze

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorReducerDrainForcedIntentsForceThenObserveUnderOneBound(t *testing.T) {
	for _, kind := range []supervisorRunningIntentKind{
		supervisorIntentFuse,
		supervisorIntentDeadline,
		supervisorIntentStop,
		supervisorIntentObservationFailure,
	} {
		t.Run(runningIntentName(kind), func(t *testing.T) {
			fixture := newForcedDrainReducerFixture(t, kind)
			assert.Equal(t, supervisorForceOwned, fixture.first.kind, "first drain action = %#v", fixture.first)
			attempt := supervisorAttemptByGeneration(t, fixture.state, fixture.generation)
			assert.True(t, attempt.drain.effectiveDrainBy.Equal(fixture.drainBy), "forced drain start = %#v action=%#v", attempt, fixture.first)
			assert.True(t, attempt.drain.forced, "forced drain start = %#v action=%#v", attempt, fixture.first)
			assert.Equal(t, (supervisorPendingAction{kind: fixture.first.kind, token: fixture.first.token}), attempt.pendingAction, "forced drain start = %#v action=%#v", attempt, fixture.first)

			at := fixture.first.at.Add(time.Nanosecond)
			next, actions := fixture.complete(t, fixture.state, fixture.first, supervisorDrainForceCompleted, at, 0)
			assertSupervisorActions(t, actions, supervisorObserveEmptiness)
			assert.False(t, actions[0].token <= fixture.first.token, "force completion reset drain bound or token: first=%#v next=%#v", fixture.first, actions[0])
			assert.True(t, actions[0].drainBy.Equal(fixture.drainBy), "force completion reset drain bound or token: first=%#v next=%#v", fixture.first, actions[0])
			attempt = supervisorAttemptByGeneration(t, next, fixture.generation)
			assert.Equal(t, (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}), attempt.pendingAction, "post-force observation = %#v actions=%#v", attempt, actions)
			assert.True(t, attempt.drain.effectiveDrainBy.Equal(fixture.drainBy), "post-force observation = %#v actions=%#v", attempt, actions)
		})
	}
}

func TestSupervisorReducerDrainRootExitObservesAndTimelyEmptyCapturesWithoutForce(t *testing.T) {
	fixture := newRootExitDrainReducerFixture(t)
	assert.Equal(t, supervisorObserveEmptiness, fixture.first.kind, "root exit first drain action = %#v", fixture.first)
	before := supervisorAttemptByGeneration(t, fixture.state, fixture.generation)
	assert.False(t, before.drain.forced, "root exit forced before authoritative observation: %#v", before)

	at := fixture.first.at.Add(time.Nanosecond)
	next, actions := fixture.complete(t, fixture.state, fixture.first, supervisorDrainObservedEmpty, at, 0)
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	after := supervisorAttemptByGeneration(t, next, fixture.generation)
	assert.Equal(t, supervisorCapturingOutput, after.phase, "timely empty root-exit drain = %#v actions=%#v", after, actions)
	assert.Equal(t, supervisorDrainProvenEmpty, after.drain.decision, "timely empty root-exit drain = %#v actions=%#v", after, actions)
	assert.False(t, after.drain.forced, "timely empty root-exit drain = %#v actions=%#v", after, actions)
	assert.True(t, actions[0].drainBy.Equal(fixture.drainBy), "timely empty root-exit drain = %#v actions=%#v", after, actions)
	assert.Equal(t, (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}), after.pendingAction, "timely empty root-exit drain = %#v actions=%#v", after, actions)
}

func TestSupervisorReducerDrainResidualAndObservationFailureForceOrReobserveCausally(t *testing.T) {
	for _, completion := range []struct {
		name       string
		kind       supervisorDrainCompletionKind
		diagnostic supervisorDiagnosticRef
	}{
		{name: "residual", kind: supervisorDrainObservedResidual},
		{name: "observation fault", kind: supervisorDrainObservationFailed, diagnostic: 701},
	} {
		t.Run("natural "+completion.name+" at bound still forces", func(t *testing.T) {
			fixture := newRootExitDrainReducerFixture(t)
			next, actions := fixture.complete(
				t, fixture.state, fixture.first, completion.kind, fixture.drainBy, completion.diagnostic,
			)
			assertSupervisorActions(t, actions, supervisorForceOwned)
			attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
			assert.True(t, attempt.drain.forced, "natural residual/fault escaped force: %#v actions=%#v", attempt, actions)
			assert.True(t, actions[0].drainBy.Equal(fixture.drainBy), "natural residual/fault escaped force: %#v actions=%#v", attempt, actions)
			assert.Equal(t, (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}), attempt.pendingAction, "natural residual/fault escaped force: %#v actions=%#v", attempt, actions)
		})

		t.Run("post-force "+completion.name+" reobserves before same bound", func(t *testing.T) {
			fixture := newForcedDrainReducerFixture(t, supervisorIntentFuse)
			forcedAt := fixture.first.at.Add(time.Nanosecond)
			forced, observe := fixture.complete(
				t, fixture.state, fixture.first, supervisorDrainForceCompleted, forcedAt, 0,
			)
			observedAt := forcedAt.Add(time.Nanosecond)
			next, actions := fixture.complete(
				t, forced, observe[0], completion.kind, observedAt, completion.diagnostic,
			)
			assertSupervisorActions(t, actions, supervisorObserveEmptiness)
			attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
			assert.True(t, actions[0].drainBy.Equal(fixture.drainBy), "post-force observation changed epoch: %#v actions=%#v", attempt, actions)
			assert.Equal(t, (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}), attempt.pendingAction, "post-force observation changed epoch: %#v actions=%#v", attempt, actions)
		})
	}
}

func TestSupervisorReducerDrainEqualityNeverManufacturesEmptiness(t *testing.T) {
	t.Run("force completion at equality", func(t *testing.T) {
		fixture := newForcedDrainReducerFixture(t, supervisorIntentDeadline)
		next, actions := fixture.complete(
			t, fixture.state, fixture.first, supervisorDrainForceCompleted, fixture.drainBy, 0,
		)
		assertUnconfirmedCapture(t, next, actions, fixture.generation, fixture.drainBy)
	})

	t.Run("authoritative empty at equality", func(t *testing.T) {
		fixture := newRootExitDrainReducerFixture(t)
		next, actions := fixture.complete(
			t, fixture.state, fixture.first, supervisorDrainObservedEmpty, fixture.drainBy, 0,
		)
		assertUnconfirmedCapture(t, next, actions, fixture.generation, fixture.drainBy)
		assert.NotEqual(t, supervisorDrainProvenEmpty, supervisorAttemptByGeneration(t, next, fixture.generation).drain.decision, "equality manufactured timely emptiness: %#v", next)
	})

	t.Run("post-force residual at equality", func(t *testing.T) {
		fixture := newForcedDrainReducerFixture(t, supervisorIntentFuse)
		forcedAt := fixture.first.at.Add(time.Nanosecond)
		forced, observe := fixture.complete(
			t, fixture.state, fixture.first, supervisorDrainForceCompleted, forcedAt, 0,
		)
		next, actions := fixture.complete(
			t, forced, observe[0], supervisorDrainObservedResidual, fixture.drainBy, 0,
		)
		assertUnconfirmedCapture(t, next, actions, fixture.generation, fixture.drainBy)
	})
}

func TestSupervisorReducerDrainControlDiagnosticNeedsTimelyEmptyProof(t *testing.T) {
	const control = supervisorDiagnosticRef(801)
	t.Run("timely empty retains control diagnostic", func(t *testing.T) {
		fixture := newForcedDrainReducerFixture(t, supervisorIntentStop)
		forcedAt := fixture.first.at.Add(time.Nanosecond)
		forced, observe := fixture.complete(
			t, fixture.state, fixture.first, supervisorDrainForceCompleted, forcedAt, control,
		)
		next, actions := fixture.complete(
			t, forced, observe[0], supervisorDrainObservedEmpty, forcedAt.Add(time.Nanosecond), 0,
		)
		assertSupervisorActions(t, actions, supervisorCaptureOutput)
		attempt := supervisorAttemptByGeneration(t, next, fixture.generation)
		assert.Equal(t, supervisorDrainProvenEmpty, attempt.drain.decision, "control diagnostic displaced timely empty proof: %#v", attempt)
		assert.Equal(t, control, attempt.drain.controlDiagnostic, "control diagnostic displaced timely empty proof: %#v", attempt)
	})

	t.Run("control fault without timely empty stays unconfirmed", func(t *testing.T) {
		fixture := newForcedDrainReducerFixture(t, supervisorIntentStop)
		next, actions := fixture.complete(
			t, fixture.state, fixture.first, supervisorDrainForceCompleted, fixture.drainBy, control,
		)
		assertUnconfirmedCapture(t, next, actions, fixture.generation, fixture.drainBy)
		assert.Equal(t, control, supervisorAttemptByGeneration(t, next, fixture.generation).drain.controlDiagnostic, "unconfirmed drain lost control diagnostic: %#v", next)
	})
}

func TestSupervisorReducerDrainEmergencyClampPersistsAcrossInflightAction(t *testing.T) {
	for _, firstIntent := range []supervisorRunningIntentKind{
		supervisorIntentFuse,
		supervisorIntentRootExit,
	} {
		t.Run(runningIntentName(firstIntent), func(t *testing.T) {
			fixture := newDrainReducerFixture(t, firstIntent)
			originalPending := supervisorPendingAction{kind: fixture.first.kind, token: fixture.first.token}
			emergencyAt := fixture.first.at.Add(time.Nanosecond)
			clamp := fixture.drainBy.Add(-time.Second)
			clamped, actions := reduceSupervisor(fixture.state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: clamp,
				emergencySnapshots: []supervisorEmergencySnapshot{{
					generation: fixture.generation,
					running: &supervisorRunningBundle{
						generation: fixture.generation, sampleAction: fixture.sampleAction,
						waitAction: fixture.waitAction, drainBy: fixture.drainBy,
					},
				}},
			})
			assert.EqualValues(t, 0, len(actions), "emergency issued competing action: %#v", actions)
			attempt := supervisorAttemptByGeneration(t, clamped, fixture.generation)
			assert.Equal(t, originalPending, attempt.pendingAction, "in-flight emergency clamp = %#v", attempt)
			assert.True(t, attempt.drain.effectiveDrainBy.Equal(clamp), "in-flight emergency clamp = %#v", attempt)

			completionKind := supervisorDrainForceCompleted
			if fixture.first.kind == supervisorObserveEmptiness {
				completionKind = supervisorDrainObservedResidual
			}
			_, nextActions := fixture.complete(
				t, clamped, fixture.first, completionKind, emergencyAt.Add(time.Nanosecond), 0,
			)
			for _, action := range nextActions {
				assert.True(t, action.drainBy.Equal(clamp), "post-clamp action lengthened bound: %#v", nextActions)
			}
		})
	}

	t.Run("later emergency never lengthens local bound", func(t *testing.T) {
		fixture := newForcedDrainReducerFixture(t, supervisorIntentFuse)
		emergencyAt := fixture.first.at.Add(time.Nanosecond)
		later := fixture.drainBy.Add(time.Second)
		clamped, actions := reduceSupervisor(fixture.state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: later,
			emergencySnapshots: []supervisorEmergencySnapshot{{
				generation: fixture.generation,
				running: &supervisorRunningBundle{
					generation: fixture.generation, sampleAction: fixture.sampleAction,
					waitAction: fixture.waitAction, drainBy: fixture.drainBy,
				},
			}},
		})
		assert.EqualValues(t, 0, len(actions), "later emergency issued competing action: %#v", actions)
		{
			got := supervisorAttemptByGeneration(t, clamped, fixture.generation).drain.effectiveDrainBy
			assert.True(t, got.Equal(fixture.drainBy), "emergency lengthened local bound to %v", got)
		}
	})
}

func TestSupervisorReducerDrainEmergencyDuringCapturePreservesDecisionAndPendingAction(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(*testing.T) (drainReducerFixture, supervisorState, supervisorAction)
	}{
		{
			name: "timely proven empty",
			make: func(t *testing.T) (drainReducerFixture, supervisorState, supervisorAction) {
				fixture := newRootExitDrainReducerFixture(t)
				capturing, actions := fixture.complete(
					t, fixture.state, fixture.first, supervisorDrainObservedEmpty,
					fixture.first.at.Add(time.Nanosecond), 0,
				)

				return fixture, capturing, actions[0]
			},
		},
		{
			name: "unconfirmed equality",
			make: func(t *testing.T) (drainReducerFixture, supervisorState, supervisorAction) {
				fixture := newForcedDrainReducerFixture(t, supervisorIntentDeadline)
				capturing, actions := fixture.complete(
					t, fixture.state, fixture.first, supervisorDrainForceCompleted, fixture.drainBy, 0,
				)

				return fixture, capturing, actions[0]
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, capturing, capture := test.make(t)
			before := supervisorAttemptByGeneration(t, capturing, fixture.generation)
			emergencyAt := capture.at.Add(time.Nanosecond)
			emergencyDrainBy := emergencyAt.Add(time.Second)
			next, actions := reduceSupervisor(capturing, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
				emergencySnapshots: []supervisorEmergencySnapshot{{generation: fixture.generation}},
			})
			assert.EqualValues(t, 0, len(actions), "capture-time emergency emitted competing action: %#v", actions)
			after := supervisorAttemptByGeneration(t, next, fixture.generation)
			wantBound := fixture.drainBy
			if emergencyDrainBy.Before(wantBound) {
				wantBound = emergencyDrainBy
			}
			assert.Equal(t, supervisorCapturingOutput, after.phase, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.Equal(t, before.pendingAction, after.pendingAction, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.Equal(t, (supervisorPendingAction{kind: supervisorCaptureOutput, token: capture.token}), after.pendingAction, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.Equal(t, before.drain.decision, after.drain.decision, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.Equal(t, before.drain.controlDiagnostic, after.drain.controlDiagnostic, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.Equal(t, before.drain.observationDiagnostic, after.drain.observationDiagnostic, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.True(t, after.drain.effectiveDrainBy.Equal(wantBound), "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.True(t, next.emergency.active, "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.True(t, next.emergency.at.Equal(emergencyAt), "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.True(t, next.emergency.drainBy.Equal(emergencyDrainBy), "capture-time emergency changed decision or custody: before=%#v after=%#v", before, after)
			assert.False(t, before.drain.decision == supervisorDrainProvenEmpty &&
				after.drain.decision != supervisorDrainProvenEmpty, "emergency invalidated a timely emptiness proof: before=%#v after=%#v", before, after)
		})
	}
}

func TestSupervisorReducerDrainCompletionRequiresExactCausalCorrelation(t *testing.T) {
	fixture := newForcedDrainReducerFixture(t, supervisorIntentFuse)
	validAt := fixture.first.at.Add(time.Nanosecond)
	valid := supervisorDrainCompletion{
		generation: fixture.generation,
		action:     supervisorPendingAction{kind: fixture.first.kind, token: fixture.first.token},
		at:         validAt,
		kind:       supervisorDrainForceCompleted,
	}
	for _, malformed := range []struct {
		name   string
		mutate func(*supervisorDrainCompletion)
	}{
		{name: "wrong generation", mutate: func(completion *supervisorDrainCompletion) { completion.generation++ }},
		{name: "wrong token", mutate: func(completion *supervisorDrainCompletion) { completion.action.token++ }},
		{name: "wrong action kind", mutate: func(completion *supervisorDrainCompletion) {
			completion.action.kind = supervisorObserveEmptiness
		}},
		{name: "wrong completion kind", mutate: func(completion *supervisorDrainCompletion) {
			completion.kind = supervisorDrainObservedEmpty
		}},
		{name: "backward instant", mutate: func(completion *supervisorDrainCompletion) {
			completion.at = fixture.first.at.Add(-time.Nanosecond)
		}},
	} {
		t.Run(malformed.name, func(t *testing.T) {
			completion := valid
			malformed.mutate(&completion)
			assertSupervisorInvariant(t, func() {
				reduceSupervisor(fixture.state, supervisorEvent{
					kind: supervisorDrainCompleted, generation: fixture.generation,
					at: completion.at, drain: &completion,
				})
			})
		})
	}

	completion := valid
	next, _ := reduceSupervisor(fixture.state, supervisorEvent{
		kind: supervisorDrainCompleted, generation: fixture.generation,
		at: completion.at, drain: &completion,
	})
	assertSupervisorInvariant(t, func() {
		reduceSupervisor(next, supervisorEvent{
			kind: supervisorDrainCompleted, generation: fixture.generation,
			at: completion.at, drain: &completion,
		})
	})
}

func TestSupervisorReducerLateLaunchCustodyResolvesOneAuthoritativeDrainBound(t *testing.T) {
	launchBy := time.Unix(1_200, 0)
	localDrainBy := launchBy.Add(5 * time.Second)

	t.Run("late release outside emergency requires resolved local bound", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 81, "attempt-late-local", launchBy)
		unconfirmed, _ := reduceSupervisor(state, supervisorEvent{
			kind: supervisorLaunchBoundary, generation: 81, at: launchBy,
		})
		completion := supervisorLaunchCompletion{
			generation: 81, action: launch.token, at: launchBy.Add(time.Nanosecond),
			kind: supervisorLaunchReleased,
		}
		next, actions := reduceSupervisor(unconfirmed, supervisorEvent{
			kind: supervisorLaunchCompleted, generation: 81, at: completion.at,
			drainBy: localDrainBy, completion: &completion,
		})
		assertSupervisorActions(t, actions, supervisorAdoptOwned, supervisorForceOwned)
		attempt := supervisorAttemptByGeneration(t, next, 81)
		assert.True(t, attempt.drain.effectiveDrainBy.Equal(localDrainBy), "late local adoption lacks drain epoch: %#v actions=%#v", attempt, actions)
		assert.True(t, actions[1].drainBy.Equal(localDrainBy), "late local adoption lacks drain epoch: %#v actions=%#v", attempt, actions)

		assertSupervisorInvariant(t, func() {
			reduceSupervisor(unconfirmed, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 81, at: completion.at,
				completion: &completion,
			})
		})
	})

	t.Run("late proven not released carries no drain bound", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 82, "attempt-late-closed", launchBy)
		unconfirmed, _ := reduceSupervisor(state, supervisorEvent{
			kind: supervisorLaunchBoundary, generation: 82, at: launchBy,
		})
		completion := supervisorLaunchCompletion{
			generation: 82, action: launch.token, at: launchBy.Add(time.Nanosecond),
			kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
		}
		next, actions := reduceSupervisor(unconfirmed, supervisorEvent{
			kind: supervisorLaunchCompleted, generation: 82, at: completion.at,
			completion: &completion,
		})
		assertSupervisorActions(t, actions, supervisorCloseProspective)
		assert.True(t, supervisorAttemptByGeneration(t, next, 82).drain.effectiveDrainBy.IsZero(), "not-released completion invented a drain bound: %#v", next)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(unconfirmed, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 82, at: completion.at,
				drainBy: localDrainBy, completion: &completion,
			})
		})
	})

	t.Run("emergency bound alone governs late release", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 83, "attempt-late-emergency", launchBy)
		emergencyAt := launchBy.Add(-time.Second)
		emergencyDrainBy := launchBy.Add(2 * time.Second)
		unconfirmed, _ := reduceSupervisor(state, supervisorEvent{
			kind: supervisorEmergencyStarted, at: emergencyAt, drainBy: emergencyDrainBy,
			emergencySnapshots: []supervisorEmergencySnapshot{{generation: 83}},
		})
		completion := supervisorLaunchCompletion{
			generation: 83, action: launch.token, at: emergencyAt.Add(time.Nanosecond),
			kind: supervisorLaunchReleased,
		}
		next, actions := reduceSupervisor(unconfirmed, supervisorEvent{
			kind: supervisorLaunchCompleted, generation: 83, at: completion.at,
			completion: &completion,
		})
		assertSupervisorActions(t, actions, supervisorAdoptOwned, supervisorForceOwned)
		attempt := supervisorAttemptByGeneration(t, next, 83)
		assert.True(t, attempt.drain.effectiveDrainBy.Equal(emergencyDrainBy), "late emergency adoption changed bound: %#v actions=%#v", attempt, actions)
		assert.True(t, actions[1].drainBy.Equal(emergencyDrainBy), "late emergency adoption changed bound: %#v actions=%#v", attempt, actions)
	})
}

func TestSupervisorReducerLaunchEvidenceUsesImmutableRegistrationAndPhysicalCompletion(t *testing.T) {
	launchBy := time.Unix(1_300, 0)
	registeredAt := launchBy.Add(-time.Second)

	for _, test := range []struct {
		name       string
		completion *supervisorLaunchCompletion
		wantKind   supervisorActionKind
		wantAt     time.Time
	}{
		{
			name: "owned completion delivered at boundary",
			completion: &supervisorLaunchCompletion{
				generation: 91, at: launchBy.Add(-500 * time.Millisecond), kind: supervisorLaunchReleased,
			},
			wantKind: supervisorPublishOwned, wantAt: launchBy.Add(-500 * time.Millisecond),
		},
		{
			name: "not released completion delivered at boundary",
			completion: &supervisorLaunchCompletion{
				generation: 91, at: launchBy.Add(-250 * time.Millisecond),
				kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
			},
			wantKind: supervisorPublishNotReleased, wantAt: launchBy.Add(-250 * time.Millisecond),
		},
		{name: "nil boundary publishes unconfirmed", wantKind: supervisorPublishLaunchUnconfirmed, wantAt: launchBy},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, actions := reduceSupervisor(supervisorState{}, supervisorEvent{
				kind: supervisorProspectiveRegistered, generation: 91, attempt: "attempt-timing",
				at: registeredAt, launchBy: launchBy, profile: AutomaticProfile,
				commandDeadline: 20 * time.Second,
			})
			launch := actions[0]
			{
				got := supervisorAttemptByGeneration(t, state, 91).registeredAt
				assert.True(t, got.Equal(registeredAt), "registration At = %v, want %v", got, registeredAt)
			}
			if test.completion != nil {
				test.completion.action = launch.token
			}
			next, classified := reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: 91, at: launchBy, completion: test.completion,
			})
			var action supervisorAction
			for _, candidate := range classified {
				if candidate.kind == test.wantKind {
					action = candidate
					break
				}
			}
			assert.NotEqual(t, 0, action.kind, "launch timing evidence = %#v, want At=%v duration=%v", action, test.wantAt, test.wantAt.Sub(registeredAt))
			assert.True(t, action.at.Equal(test.wantAt), "launch timing evidence = %#v, want At=%v duration=%v", action, test.wantAt, test.wantAt.Sub(registeredAt))
			assert.Equal(t, test.wantAt.Sub(registeredAt), action.launchDuration, "launch timing evidence = %#v, want At=%v duration=%v", action, test.wantAt, test.wantAt.Sub(registeredAt))
			{
				got := supervisorAttemptByGeneration(t, next, 91).registeredAt
				assert.True(t, got.Equal(registeredAt), "launch transition rewrote registration At to %v", got)
			}
		})
	}

	t.Run("completion cannot predate immutable registration", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 92, "attempt-negative-duration", launchBy)
		completion := supervisorLaunchCompletion{
			generation: 92, action: launch.token, at: registeredAt.Add(-time.Nanosecond),
			kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
		}
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: 92, at: launchBy, completion: &completion,
			})
		})
	})
}

type drainReducerFixture struct {
	state        supervisorState
	generation   attemptGeneration
	first        supervisorAction
	drainBy      time.Time
	sampleAction supervisorActionToken
	waitAction   supervisorActionToken
}

func newDrainReducerFixture(t *testing.T, kind supervisorRunningIntentKind) drainReducerFixture {
	t.Helper()
	if kind == supervisorIntentRootExit {
		return newRootExitDrainReducerFixture(t)
	}

	return newForcedDrainReducerFixture(t, kind)
}

func newForcedDrainReducerFixture(t *testing.T, kind supervisorRunningIntentKind) drainReducerFixture {
	t.Helper()
	running := newRunningReducerFixture(t, AutomaticProfile)
	at := running.startedAt.Add(time.Second)
	fact := supervisorRunningFact{generation: running.generation, at: at}
	through := at
	recheck := supervisorExitRecheck{}
	switch kind {
	case supervisorIntentFuse:
		fact.kind = supervisorRunningFuseObserved
		fact.action = running.sampleAction
		fact.rootLive = true
		fact.live = 65
	case supervisorIntentDeadline:
		through = running.deadlineAt
		fact = supervisorRunningFact{}
		recheck = supervisorExitRecheck{performed: true, at: running.deadlineAt}
	case supervisorIntentStop:
		fact.kind = supervisorRunningStopRequested
		fact.stop = StopRequest{At: at, DrainBy: running.drainBy.Add(-time.Second)}
	case supervisorIntentObservationFailure:
		fact.kind = supervisorRunningObservationFailed
		fact.action = running.sampleAction
		fact.source = supervisorObservationRunning
		fact.diagnostic = 601
	default:
		require.FailNowf(t, "unsupported forced drain intent", "intent: %d", kind)
	}
	facts := []supervisorRunningFact{fact}
	if kind == supervisorIntentDeadline {
		facts = nil
	}
	state, actions := running.reduceBundle(t, through, facts, recheck)
	assertSupervisorActions(t, actions, supervisorForceOwned)
	drainBy := actions[0].drainBy

	return drainReducerFixture{
		state: state, generation: running.generation, first: actions[0], drainBy: drainBy,
		sampleAction: running.sampleAction, waitAction: running.waitAction,
	}
}

func newRootExitDrainReducerFixture(t *testing.T) drainReducerFixture {
	t.Helper()
	running := newRunningReducerFixture(t, AutomaticProfile)
	at := running.startedAt.Add(time.Second)
	fact := supervisorRunningFact{
		generation: running.generation, action: running.waitAction,
		kind: supervisorRunningRootExited, at: at,
	}
	state, actions := running.reduceBundle(t, at, []supervisorRunningFact{fact}, supervisorExitRecheck{})
	assertSupervisorActions(t, actions, supervisorObserveEmptiness)

	return drainReducerFixture{
		state: state, generation: running.generation, first: actions[0], drainBy: actions[0].drainBy,
		sampleAction: running.sampleAction, waitAction: running.waitAction,
	}
}

func (fixture drainReducerFixture) complete(
	t *testing.T,
	state supervisorState,
	action supervisorAction,
	kind supervisorDrainCompletionKind,
	at time.Time,
	diagnostic supervisorDiagnosticRef,
) (supervisorState, []supervisorAction) {
	t.Helper()
	completion := supervisorDrainCompletion{
		generation: fixture.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at,
		kind:       kind,
		diagnostic: diagnostic,
	}

	return reduceSupervisor(state, supervisorEvent{
		kind: supervisorDrainCompleted, generation: fixture.generation,
		at: at, drain: &completion,
	})
}

func assertUnconfirmedCapture(
	t *testing.T,
	state supervisorState,
	actions []supervisorAction,
	generation attemptGeneration,
	drainBy time.Time,
) {
	t.Helper()
	assertSupervisorActions(t, actions, supervisorCaptureOutput)
	attempt := supervisorAttemptByGeneration(t, state, generation)
	assert.Equal(t, supervisorCapturingOutput, attempt.phase, "unconfirmed capture = %#v actions=%#v", attempt, actions)
	assert.Equal(t, supervisorDrainUnconfirmed, attempt.drain.decision, "unconfirmed capture = %#v actions=%#v", attempt, actions)
	assert.True(t, attempt.drain.effectiveDrainBy.Equal(drainBy), "unconfirmed capture = %#v actions=%#v", attempt, actions)
	assert.True(t, actions[0].drainBy.Equal(drainBy), "unconfirmed capture = %#v actions=%#v", attempt, actions)
	assert.Equal(t, (supervisorPendingAction{kind: actions[0].kind, token: actions[0].token}), attempt.pendingAction, "unconfirmed capture = %#v actions=%#v", attempt, actions)
}

func runningIntentName(kind supervisorRunningIntentKind) string {
	switch kind {
	case supervisorIntentFuse:
		return "fuse"
	case supervisorIntentRootExit:
		return "root exit"
	case supervisorIntentObservationFailure:
		return "running fault"
	case supervisorIntentDeadline:
		return "deadline"
	case supervisorIntentStop:
		return "stop"
	default:
		return "invalid"
	}
}

func TestSupervisorReducerDrainDataRemainsCapabilityFree(t *testing.T) {
	for _, dataType := range []reflect.Type{
		reflect.TypeOf(supervisorDrainCompletion{}),
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorAction{}),
	} {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}
