package ooze

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorMachinePublishesDomainEventsAndEffects(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine := newSupervisorMachine()

	_, transition := machine.Apply(supervisionFactFromEvent(fact))

	assert.Equal(t, supervisionAttemptRegistered, transition.Event().kind)
	assert.Equal(t, attemptGeneration(1), transition.Event().generation)
	assert.Equal(t, attemptIdentity("mutant-1"), transition.Event().attempt)
	require.Len(t, transition.Effects(), 1)
	assert.Equal(t, supervisorLaunchNative, transition.Effects()[0].kind)
	assert.Equal(t, attemptGeneration(1), transition.Effects()[0].generation)
}

func TestSupervisorMachineTransitionIsImmutable(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine := newSupervisorMachine()
	_, transition := machine.Apply(supervisionFactFromEvent(fact))

	effects := transition.Effects()
	effects[0].kind = supervisorDeliverTerminal

	assert.Equal(t, attemptIdentity("mutant-1"), transition.Event().attempt)
	assert.Equal(t, supervisorLaunchNative, transition.Effects()[0].kind)
}

func TestSupervisorMachineCanForkWithoutSharingState(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine := newSupervisorMachine()

	left, leftTransition := machine.Apply(supervisionFactFromEvent(fact))
	right, rightTransition := machine.Apply(supervisionFactFromEvent(fact))

	assert.Equal(t, left.snapshot(), right.snapshot())
	assert.Equal(t, leftTransition.Event(), rightTransition.Event())
	assert.Equal(t, leftTransition.Effects(), rightTransition.Effects())
	assert.Empty(t, machine.snapshot().attempts)
}

func TestSupervisorMachineProjectionDoesNotExposeReducerState(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine, _ := newSupervisorMachine().Apply(supervisionFactFromEvent(fact))

	projection := machine.Projection()
	want := cloneSupervisionProjection(projection)
	projection.attempts[0].attempt = "corrupted"

	assert.True(t, machine.Projection().Equal(want))
	assert.False(t, machine.Projection().Equal(projection))
}

func TestSupervisorProjectionMeasuresItsOwnFactBoundary(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	machine, transition := newSupervisorMachine().Apply(supervisionProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	))
	launch := effectOfKind(t, transition.Effects(), supervisorLaunchNative)
	facts, ok := machine.LaunchFacts(launch, supervisionLaunchReleasedBeforeBoundary)
	require.True(t, ok)
	machine, _ = machine.Apply(facts[0])

	assert.Equal(t, 1, machine.Projection().BoundaryDistance(facts[0], []supervisionEffect{launch}))
}

func TestSupervisorMachineUsesCanonicalDomainVocabulary(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisionProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	)

	machine, transition := newSupervisorMachine().Apply(fact)

	assert.Equal(t, supervisionAttemptRegistered, transition.Event().kind)
	require.Len(t, transition.Effects(), 1)
	assert.Equal(t, supervisorLaunchNative, transition.Effects()[0].kind)
	assert.Equal(t, machine.Projection(), transition.Projection())
	for name, value := range map[string]any{
		"fact": fact, "effect": transition.Effects()[0], "projection": transition.Projection(),
	} {
		t.Run(name+" is capability free", func(t *testing.T) {
			assert.Empty(t, simulationForbiddenValuePath(reflect.ValueOf(value), name))
		})
	}
}

func TestSupervisorMachinePreparesEmergencyFromOwnedState(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	machine, _ := newSupervisorMachine().Apply(supervisionProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	))

	t.Run("before the launch boundary", func(t *testing.T) {
		fact, ready := machine.PrepareEmergency(registeredAt, registeredAt.Add(5*time.Second))

		require.True(t, ready)
		assert.Equal(t, supervisorEmergencyStarted, fact.kind)
		assert.Equal(t, supervisionInstantFromTime(registeredAt), fact.at)
		require.Len(t, fact.emergencySnapshots, 1)
		assert.Equal(t, attemptGeneration(1), fact.emergencySnapshots[0].generation)
	})

	t.Run("after an unresolved launch boundary", func(t *testing.T) {
		_, ready := machine.PrepareEmergency(
			registeredAt.Add(time.Second+time.Nanosecond), registeredAt.Add(5*time.Second),
		)

		assert.False(t, ready)
	})
}

func TestSupervisorMachineProducesLaunchFactsFromItsOwnEffect(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	machine, transition := newSupervisorMachine().Apply(supervisionProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	))
	require.Len(t, transition.Effects(), 1)
	launch := transition.Effects()[0]

	tests := []struct {
		name       string
		outcome    supervisionLaunchOutcome
		wantKinds  []supervisorEventKind
		wantAt     []time.Time
		wantLaunch supervisorLaunchCompletionKind
	}{
		{
			name: "released before boundary", outcome: supervisionLaunchReleasedBeforeBoundary,
			wantKinds:  []supervisorEventKind{supervisorLaunchCompleted},
			wantAt:     []time.Time{registeredAt.Add(time.Second - time.Nanosecond)},
			wantLaunch: supervisorLaunchReleased,
		},
		{
			name: "released at boundary", outcome: supervisionLaunchReleasedAtBoundary,
			wantKinds:  []supervisorEventKind{supervisorLaunchBoundary},
			wantAt:     []time.Time{registeredAt.Add(time.Second)},
			wantLaunch: supervisorLaunchReleased,
		},
		{
			name: "released after boundary", outcome: supervisionLaunchReleasedAfterBoundary,
			wantKinds:  []supervisorEventKind{supervisorLaunchBoundary, supervisorLaunchCompleted},
			wantAt:     []time.Time{registeredAt.Add(time.Second), registeredAt.Add(time.Second + time.Nanosecond)},
			wantLaunch: supervisorLaunchReleased,
		},
		{
			name: "proven not released", outcome: supervisionLaunchProvenNotReleased,
			wantKinds:  []supervisorEventKind{supervisorLaunchCompleted},
			wantAt:     []time.Time{registeredAt.Add(time.Second - time.Nanosecond)},
			wantLaunch: supervisorLaunchProvenNotReleased,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts, ok := machine.LaunchFacts(launch, test.outcome)

			require.True(t, ok)
			require.Len(t, facts, len(test.wantKinds))
			for index := range facts {
				assert.Equal(t, test.wantKinds[index], facts[index].kind)
				assert.True(t, test.wantAt[index].Equal(facts[index].at.production()))
			}
			require.NotNil(t, facts[len(facts)-1].completion)
			assert.Equal(t, test.wantLaunch, facts[len(facts)-1].completion.kind)
		})
	}
}

func TestSupervisorMachineProducesRunningFactsFromItsOwnEffects(t *testing.T) {
	machine, effects := runningSupervisorMachine(t, AutomaticProfile)
	wait := effectOfKind(t, effects, supervisorWaitRoot)
	sample := effectOfKind(t, effects, supervisorSampleRunning)

	tests := []struct {
		name    string
		outcome supervisionRunningOutcome
		kind    supervisorRunningFactKind
	}{
		{name: "passed", outcome: supervisionRunningPassed, kind: supervisorRunningRootExited},
		{name: "failed", outcome: supervisionRunningFailed, kind: supervisorRunningRootExited},
		{name: "deadline", outcome: supervisionRunningAtDeadline},
		{name: "after deadline", outcome: supervisionRunningAfterDeadline, kind: supervisorRunningRootExited},
		{name: "fuse", outcome: supervisionRunningFuse, kind: supervisorRunningFuseObserved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fact, ok := machine.RunningFact(wait, sample, test.outcome)

			require.True(t, ok)
			require.NotNil(t, fact.running)
			if test.kind != 0 {
				require.Len(t, fact.running.facts, 1)
				assert.Equal(t, test.kind, fact.running.facts[0].kind)
			}
		})
	}
}

func TestSupervisorMachineOwnsStopAdmission(t *testing.T) {
	machine, _ := runningSupervisorMachine(t, AutomaticProfile)

	fact, disposition := machine.StopFact(1)

	assert.Equal(t, supervisionStopReady, disposition)
	assert.Equal(t, supervisorRunningObserved, fact.kind)
	require.NotNil(t, fact.running)
	require.Len(t, fact.running.facts, 1)
	assert.Equal(t, supervisorRunningStopRequested, fact.running.facts[0].kind)
}

func TestSupervisorMachineProducesCompletionFactsFromItsOwnEffect(t *testing.T) {
	machine, effects := runningSupervisorMachine(t, AutomaticProfile)
	wait := effectOfKind(t, effects, supervisorWaitRoot)
	sample := effectOfKind(t, effects, supervisorSampleRunning)
	running, ok := machine.RunningFact(wait, sample, supervisionRunningFailed)
	require.True(t, ok)
	machine, transition := machine.Apply(running)
	observe := effectOfKind(t, transition.Effects(), supervisorObserveEmptiness)

	fact, ok := machine.CompletionFact(observe, supervisionCompletionBeforeBoundary)

	require.True(t, ok)
	assert.Equal(t, supervisorDrainCompleted, fact.kind)
	require.NotNil(t, fact.drain)
	assert.Equal(t, supervisorDrainObservedEmpty, fact.drain.kind)
	assert.Equal(t, observe.token, fact.drain.action.token)
}

func runningSupervisorMachine(t *testing.T, profile Profile) (*supervisorMachine, []supervisionEffect) {
	t.Helper()
	registeredAt := time.Unix(100, 0)
	machine, transition := newSupervisorMachine().Apply(supervisionProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), profile, time.Minute,
	))
	launch := effectOfKind(t, transition.Effects(), supervisorLaunchNative)
	facts, ok := machine.LaunchFacts(launch, supervisionLaunchReleasedBeforeBoundary)
	require.True(t, ok)
	require.Len(t, facts, 1)
	machine, transition = machine.Apply(facts[0])

	return machine, transition.Effects()
}

func effectOfKind(t *testing.T, effects []supervisionEffect, kind supervisorActionKind) supervisionEffect {
	t.Helper()
	for _, effect := range effects {
		if effect.kind == kind {
			return effect
		}
	}
	require.Failf(t, "effect not found", "kind %d in %v", kind, effects)

	return supervisionEffect{}
}
