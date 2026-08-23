package ooze

import (
	"reflect"
	"testing"
	"time"
)

func TestSupervisorReducerLaunchCompletionBeforeAndAtBoundary(t *testing.T) {
	launchBy := time.Unix(100, 0)
	for _, test := range []struct {
		name           string
		completionKind supervisorLaunchCompletionKind
		failure        LaunchFailure
		completion     time.Time
		boundary       bool
		wantAction     supervisorActionKind
		wantPhase      supervisorAttemptPhase
	}{
		{
			name: "not released before", completionKind: supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
			completion: launchBy.Add(-time.Nanosecond), wantAction: supervisorPublishNotReleased,
			wantPhase: supervisorLaunchClosedNotReleased,
		},
		{
			name: "owned before", completionKind: supervisorLaunchReleased,
			completion: launchBy.Add(-time.Nanosecond), wantAction: supervisorPublishOwned,
			wantPhase: supervisorLaunchOwned,
		},
		{
			name: "not released at equality", completionKind: supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
			completion: launchBy, boundary: true, wantAction: supervisorPublishNotReleased,
			wantPhase: supervisorLaunchClosedNotReleased,
		},
		{
			name: "owned at equality", completionKind: supervisorLaunchReleased,
			completion: launchBy, boundary: true, wantAction: supervisorPublishOwned,
			wantPhase: supervisorLaunchOwned,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, launch := registeredReducerLaunch(t, 11, "attempt-a", launchBy)
			completion := supervisorLaunchCompletion{
				generation: 11,
				action:     launch.token,
				at:         test.completion,
				kind:       test.completionKind,
				failure:    test.failure,
			}
			event := supervisorEvent{
				kind:       supervisorLaunchCompleted,
				generation: 11,
				at:         test.completion,
				completion: &completion,
			}
			if test.boundary {
				event.kind = supervisorLaunchBoundary
				event.at = launchBy
			}

			next, actions := reduceSupervisor(state, event)
			assertSupervisorActions(t, actions, test.wantAction)
			if actions[0].generation != 11 || actions[0].token <= launch.token ||
				actions[0].launchKind != test.completionKind || actions[0].launchFailure != test.failure {
				t.Fatalf("classified launch action = %#v, launch token %d", actions[0], launch.token)
			}
			attempt := supervisorAttemptByGeneration(t, next, 11)
			if attempt.phase != test.wantPhase {
				t.Fatalf("launch phase = %d, want %d", attempt.phase, test.wantPhase)
			}
		})
	}

	t.Run("published before but notification delayed until boundary", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 12, "attempt-b", launchBy)
		completion := supervisorLaunchCompletion{
			generation: 12,
			action:     launch.token,
			at:         launchBy.Add(-time.Nanosecond),
			kind:       supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
		}
		next, actions := reduceSupervisor(state, supervisorEvent{
			kind: supervisorLaunchBoundary, generation: 12, at: launchBy, completion: &completion,
		})
		assertSupervisorActions(t, actions, supervisorPublishNotReleased)
		if supervisorAttemptByGeneration(t, next, 12).phase != supervisorLaunchClosedNotReleased {
			t.Fatalf("delayed notification lost the serialized boundary snapshot: %#v", next)
		}
	})
}

func TestSupervisorReducerLaunchNilBoundaryRevokesAndLateCompletionRetainsCustody(t *testing.T) {
	launchBy := time.Unix(200, 0)
	for _, test := range []struct {
		name           string
		generation     attemptGeneration
		completionKind supervisorLaunchCompletionKind
		failure        LaunchFailure
		wantActions    []supervisorActionKind
		wantPhase      supervisorAttemptPhase
	}{
		{
			name: "late proven no release closes prospective custody", generation: 21,
			completionKind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
			wantActions: []supervisorActionKind{supervisorCloseProspective},
			wantPhase:   supervisorLaunchClosedNotReleased,
		},
		{
			name: "late unavoidable release is adopted and forced", generation: 22,
			completionKind: supervisorLaunchReleased,
			wantActions:    []supervisorActionKind{supervisorAdoptOwned, supervisorForceOwned},
			wantPhase:      supervisorLaunchOwned,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, launch := registeredReducerLaunch(t, test.generation, "attempt-late", launchBy)
			unconfirmed, boundaryActions := reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: test.generation, at: launchBy,
			})
			original := supervisorAttemptByGeneration(t, state, test.generation)
			if original.phase != supervisorLaunchEstablishing || original.releaseRevoked {
				t.Fatalf("reducer mutated its input state: %#v", original)
			}
			assertSupervisorActions(t, boundaryActions,
				supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed)
			attempt := supervisorAttemptByGeneration(t, unconfirmed, test.generation)
			if attempt.phase != supervisorLaunchReportedUnconfirmed || !attempt.releaseRevoked {
				t.Fatalf("nil boundary did not latch custody/revocation: %#v", attempt)
			}
			if boundaryActions[0].token <= launch.token ||
				boundaryActions[1].token <= boundaryActions[0].token {
				t.Fatalf("nil boundary actions = %#v", boundaryActions)
			}

			completion := supervisorLaunchCompletion{
				generation: test.generation,
				action:     launch.token,
				at:         launchBy.Add(time.Nanosecond),
				kind:       test.completionKind,
				failure:    test.failure,
			}
			next, lateActions := reduceSupervisor(unconfirmed, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: test.generation,
				at: completion.at, completion: &completion,
			})
			assertSupervisorActions(t, lateActions, test.wantActions...)
			if lateActions[0].token <= boundaryActions[len(boundaryActions)-1].token {
				t.Fatalf("late action token did not advance: boundary=%#v late=%#v", boundaryActions, lateActions)
			}
			if supervisorAttemptByGeneration(t, next, test.generation).phase != test.wantPhase {
				t.Fatalf("late completion phase = %#v", next)
			}
		})
	}
}

func TestSupervisorReducerLaunchEmergencyUsesSerializedCompletionOrNil(t *testing.T) {
	launchBy := time.Unix(300, 0)
	emergencyAt := launchBy.Add(-time.Second)
	for _, test := range []struct {
		name           string
		generation     attemptGeneration
		hasCompletion  bool
		completionKind supervisorLaunchCompletionKind
		failure        LaunchFailure
		wantActions    []supervisorActionKind
		wantPhase      supervisorAttemptPhase
	}{
		{
			name: "nil snapshot revokes and reports unconfirmed", generation: 31,
			wantActions: []supervisorActionKind{
				supervisorRevokeLaunchRelease, supervisorPublishLaunchUnconfirmed,
			},
			wantPhase: supervisorLaunchReportedUnconfirmed,
		},
		{
			name: "proven no release snapshot closes normally", generation: 32,
			hasCompletion: true, completionKind: supervisorLaunchProvenNotReleased,
			failure:     LaunchFailed,
			wantActions: []supervisorActionKind{supervisorPublishNotReleased},
			wantPhase:   supervisorLaunchClosedNotReleased,
		},
		{
			name: "released snapshot returns ownership and forces", generation: 33,
			hasCompletion: true, completionKind: supervisorLaunchReleased,
			wantActions: []supervisorActionKind{supervisorPublishOwned, supervisorForceOwned},
			wantPhase:   supervisorLaunchOwned,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			state, launch := registeredReducerLaunch(t, test.generation, "attempt-emergency", launchBy)
			var snapshot *supervisorLaunchCompletion
			if test.hasCompletion {
				snapshot = &supervisorLaunchCompletion{
					generation: test.generation,
					action:     launch.token,
					at:         emergencyAt,
					kind:       test.completionKind,
					failure:    test.failure,
				}
			}
			snapshots := []supervisorEmergencySnapshot{{
				generation: test.generation,
				completion: snapshot,
			}}
			next, actions := reduceSupervisor(state, supervisorEvent{
				kind: supervisorEmergencyStarted, at: emergencyAt,
				drainBy: launchBy.Add(time.Second), emergencySnapshots: snapshots,
			})
			assertSupervisorActions(t, actions, test.wantActions...)
			if test.hasCompletion &&
				(actions[0].launchKind != test.completionKind || actions[0].launchFailure != test.failure) {
				t.Fatalf("emergency normalized completion = %#v", actions[0])
			}
			assertStrictlyIncreasingActionTokens(t, append([]supervisorAction{launch}, actions...))
			attempt := supervisorAttemptByGeneration(t, next, test.generation)
			if attempt.phase != test.wantPhase {
				t.Fatalf("emergency launch phase = %d, want %d", attempt.phase, test.wantPhase)
			}
		})
	}
}

func TestSupervisorReducerLaunchGlobalEmergencyOrdersAllLiveObligationsAndPersistsEpoch(t *testing.T) {
	launchBy := time.Unix(350, 0)
	emergencyAt := launchBy.Add(-500 * time.Millisecond)
	drainBy := launchBy.Add(5 * time.Second)
	state := supervisorState{}
	var launchActions [4]supervisorAction
	for index, generation := range []attemptGeneration{51, 52, 53, 54} {
		state, launchActions[index] = appendReducerLaunch(
			t, state, generation, attemptIdentity("attempt-"+string(rune('a'+index))), launchBy,
		)
	}

	ownedCompletion := supervisorLaunchCompletion{
		generation: 53,
		action:     launchActions[2].token,
		at:         emergencyAt.Add(-time.Nanosecond),
		kind:       supervisorLaunchReleased,
	}
	state, _ = reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 53,
		at: ownedCompletion.at, completion: &ownedCompletion,
	})
	closedCompletion := supervisorLaunchCompletion{
		generation: 54,
		action:     launchActions[3].token,
		at:         emergencyAt.Add(-time.Nanosecond),
		kind:       supervisorLaunchProvenNotReleased,
		failure:    LaunchFailed,
	}
	state, _ = reduceSupervisor(state, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 54,
		at: closedCompletion.at, completion: &closedCompletion,
	})
	releasedSnapshot := supervisorLaunchCompletion{
		generation: 52,
		action:     launchActions[1].token,
		at:         emergencyAt,
		kind:       supervisorLaunchReleased,
	}
	event := supervisorEvent{
		kind:    supervisorEmergencyStarted,
		at:      emergencyAt,
		drainBy: drainBy,
		emergencySnapshots: []supervisorEmergencySnapshot{
			{generation: 51},
			{generation: 52, completion: &releasedSnapshot},
			{generation: 53},
		},
	}

	next, actions := reduceSupervisor(state, event)
	assertSupervisorActions(t, actions,
		supervisorRevokeLaunchRelease,
		supervisorPublishLaunchUnconfirmed,
		supervisorPublishOwned,
		supervisorForceOwned,
		supervisorForceOwned,
	)
	wantGenerations := []attemptGeneration{51, 51, 52, 52, 53}
	for index, action := range actions {
		if action.generation != wantGenerations[index] || !action.at.Equal(emergencyAt) ||
			!action.drainBy.Equal(drainBy) {
			t.Fatalf("emergency action order/request at %d = %#v", index, action)
		}
	}
	if !next.emergency.active || !next.emergency.at.Equal(emergencyAt) ||
		!next.emergency.drainBy.Equal(drainBy) {
		t.Fatalf("emergency epoch = %#v", next.emergency)
	}
	if supervisorAttemptByGeneration(t, next, 54).phase != supervisorLaunchClosedNotReleased {
		t.Fatalf("closed-not-released attempt reentered emergency: %#v", next)
	}

	lateReleased := supervisorLaunchCompletion{
		generation: 51,
		action:     launchActions[0].token,
		at:         emergencyAt.Add(time.Nanosecond),
		kind:       supervisorLaunchReleased,
	}
	adopted, lateActions := reduceSupervisor(next, supervisorEvent{
		kind: supervisorLaunchCompleted, generation: 51,
		at: lateReleased.at, completion: &lateReleased,
	})
	assertSupervisorActions(t, lateActions, supervisorAdoptOwned, supervisorForceOwned)
	for _, action := range lateActions {
		if !action.at.Equal(emergencyAt) || !action.drainBy.Equal(drainBy) {
			t.Fatalf("late adoption lost emergency request: %#v", lateActions)
		}
	}
	if supervisorAttemptByGeneration(t, adopted, 51).phase != supervisorLaunchOwned {
		t.Fatalf("late release was not adopted: %#v", adopted)
	}

	t.Run("duplicate emergency", func(t *testing.T) {
		assertSupervisorInvariant(t, func() { reduceSupervisor(next, event) })
	})
	t.Run("conflicting emergency", func(t *testing.T) {
		conflict := event
		conflict.drainBy = drainBy.Add(time.Second)
		assertSupervisorInvariant(t, func() { reduceSupervisor(next, conflict) })
	})
}

func TestSupervisorReducerLaunchRejectsStaleWrongAndDuplicateCompletionCorrelation(t *testing.T) {
	launchBy := time.Unix(400, 0)
	validCompletion := func(generation attemptGeneration, token supervisorActionToken) supervisorLaunchCompletion {
		return supervisorLaunchCompletion{
			generation: generation,
			action:     token,
			at:         launchBy.Add(-time.Nanosecond),
			kind:       supervisorLaunchProvenNotReleased,
			failure:    LaunchFailed,
		}
	}

	t.Run("stale generation", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 41, "attempt-reused", launchBy)
		completion := validCompletion(40, launch.token)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 40,
				at: completion.at, completion: &completion,
			})
		})
	})

	t.Run("wrong action token", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 42, "attempt-a", launchBy)
		completion := validCompletion(42, launch.token+1)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 42,
				at: completion.at, completion: &completion,
			})
		})
	})

	t.Run("duplicate completion", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 43, "attempt-a", launchBy)
		completion := validCompletion(43, launch.token)
		event := supervisorEvent{
			kind: supervisorLaunchCompleted, generation: 43,
			at: completion.at, completion: &completion,
		}
		closed, _ := reduceSupervisor(state, event)
		assertSupervisorInvariant(t, func() { reduceSupervisor(closed, event) })
	})

	t.Run("equality completion cannot bypass serialized snapshot", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 44, "attempt-a", launchBy)
		completion := validCompletion(44, launch.token)
		completion.at = launchBy
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 44,
				at: launchBy, completion: &completion,
			})
		})
	})

	t.Run("post-bound completion cannot skip nil boundary snapshot", func(t *testing.T) {
		state, launch := registeredReducerLaunch(t, 45, "attempt-a", launchBy)
		completion := validCompletion(45, launch.token)
		completion.at = launchBy.Add(time.Nanosecond)
		assertSupervisorInvariant(t, func() {
			reduceSupervisor(state, supervisorEvent{
				kind: supervisorLaunchCompleted, generation: 45,
				at: completion.at, completion: &completion,
			})
		})
	})
}

func TestSupervisorReducerLaunchDataTypesContainNoExecutionCapability(t *testing.T) {
	// Stable slices preserve reducer order. The only permitted pointer is a
	// pointer to a data-only optional completion snapshot carried atomically by
	// a boundary event. time.Time is an explicitly approved logical fact type.
	types := []reflect.Type{
		reflect.TypeOf(supervisorState{}),
		reflect.TypeOf(supervisorEvent{}),
		reflect.TypeOf(supervisorLaunchCompletion{}),
		reflect.TypeOf(supervisorAction{}),
	}
	for _, dataType := range types {
		t.Run(dataType.Name(), func(t *testing.T) {
			assertReducerDataOnly(t, dataType, make(map[reflect.Type]bool), dataType.Name())
		})
	}
}

func registeredReducerLaunch(
	t *testing.T,
	generation attemptGeneration,
	attempt attemptIdentity,
	launchBy time.Time,
) (supervisorState, supervisorAction) {
	t.Helper()

	return appendReducerLaunch(t, supervisorState{}, generation, attempt, launchBy)
}

func appendReducerLaunch(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
	attempt attemptIdentity,
	launchBy time.Time,
) (supervisorState, supervisorAction) {
	t.Helper()
	state, actions := reduceSupervisor(state, supervisorEvent{
		kind:       supervisorProspectiveRegistered,
		generation: generation,
		attempt:    attempt,
		at:         launchBy.Add(-time.Second),
		launchBy:   launchBy,
	})
	assertSupervisorActions(t, actions, supervisorLaunchNative)
	if actions[0].generation != generation || actions[0].token == 0 {
		t.Fatalf("native launch action = %#v", actions[0])
	}
	registered := supervisorAttemptByGeneration(t, state, generation)
	if registered.attempt != attempt || registered.launchAction != actions[0].token ||
		registered.phase != supervisorLaunchEstablishing {
		t.Fatalf("registered launch = %#v action=%#v", registered, actions[0])
	}

	return state, actions[0]
}

func supervisorAttemptByGeneration(
	t *testing.T,
	state supervisorState,
	generation attemptGeneration,
) supervisorAttemptState {
	t.Helper()
	for _, attempt := range state.attempts {
		if attempt.generation == generation {
			return attempt
		}
	}
	t.Fatalf("generation %d absent from state %#v", generation, state)

	return supervisorAttemptState{}
}

func assertSupervisorActions(t *testing.T, actions []supervisorAction, want ...supervisorActionKind) {
	t.Helper()
	got := make([]supervisorActionKind, len(actions))
	for index, action := range actions {
		got[index] = action.kind
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %v, want %v (%#v)", got, want, actions)
	}
	assertStrictlyIncreasingActionTokens(t, actions)
}

func assertStrictlyIncreasingActionTokens(t *testing.T, actions []supervisorAction) {
	t.Helper()
	var previous supervisorActionToken
	for _, action := range actions {
		if action.token == 0 || action.token <= previous {
			t.Fatalf("action tokens are not strictly increasing: %#v", actions)
		}
		previous = action.token
	}
}

func assertSupervisorInvariant(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		if _, ok := recover().(runtimeInvariantViolation); !ok {
			t.Fatalf("reducer did not raise runtimeInvariantViolation")
		}
	}()
	action()
}

func assertReducerDataOnly(t *testing.T, dataType reflect.Type, visiting map[reflect.Type]bool, path string) {
	t.Helper()
	if dataType == reflect.TypeOf(time.Time{}) || visiting[dataType] {
		return
	}
	visiting[dataType] = true
	defer delete(visiting, dataType)

	switch dataType.Kind() {
	case reflect.Func, reflect.Chan, reflect.Interface, reflect.Map, reflect.UnsafePointer, reflect.Uintptr:
		t.Fatalf("reducer data contains execution capability at %s: %s", path, dataType)
	case reflect.Pointer, reflect.Slice, reflect.Array:
		assertReducerDataOnly(t, dataType.Elem(), visiting, path+" -> "+dataType.String())
	case reflect.Struct:
		for index := range dataType.NumField() {
			field := dataType.Field(index)
			assertReducerDataOnly(t, field.Type, visiting, path+"."+field.Name)
		}
	}
}
