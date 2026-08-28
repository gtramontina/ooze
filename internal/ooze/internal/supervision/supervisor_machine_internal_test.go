package supervision

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorMachineTransitionIsImmutable(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine := NewMachine()
	_, transition := machine.Apply(supervisionFactFromEvent(fact))

	effects := transition.Effects()
	effects[0].kind = supervisorDeliverTerminal

	assert.Equal(t, attemptIdentity("mutant-1"), transition.Event().attempt)
	assert.Equal(t, supervisorLaunchNative, transition.Effects()[0].kind)
}

func TestSupervisorMachineProjectionDoesNotExposeReducerState(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine, _ := NewMachine().Apply(supervisionFactFromEvent(fact))

	projection := machine.Projection()
	want := cloneSupervisionProjection(projection)
	projection.value.attempts[0].attempt = "corrupted"

	assert.True(t, machine.Projection().Equal(want))
	assert.False(t, machine.Projection().Equal(projection))
}

func TestSupervisorMachinePublishedValuesRemainCapabilityFree(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	)

	_, transition := NewMachine().Apply(fact)

	require.Len(t, transition.Effects(), 1)
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
	machine, _ := NewMachine().Apply(ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	))

	t.Run("before the launch boundary", func(t *testing.T) {
		plan, ready := machine.planEmergency(
			registeredAt, registeredAt.Add(5*time.Second), 5*time.Second, nil,
		)
		require.True(t, ready)
		fact, ready := machine.prepareEmergencyPlan(plan, plan.deterministicRootEvidence())

		require.True(t, ready)
		assert.Equal(t, supervisorEmergencyStarted, fact.kind)
		assert.Equal(t, supervisionInstantFromTime(registeredAt), fact.at)
		require.Len(t, fact.emergencySnapshots, 1)
		assert.Equal(t, attemptGeneration(1), fact.emergencySnapshots[0].generation)
	})

	t.Run("after an unresolved launch boundary", func(t *testing.T) {
		at := registeredAt.Add(time.Second + time.Nanosecond)
		plan, planned := machine.planEmergency(at, registeredAt.Add(5*time.Second), 5*time.Second, nil)
		require.True(t, planned)
		_, ready := machine.prepareEmergencyPlan(plan, plan.deterministicRootEvidence())

		assert.False(t, ready)
	})
}

func TestSupervisorMachineLaunchFactsRetainOpaqueCompletionKind(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	machine, transition := NewMachine().Apply(ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), AutomaticProfile, time.Minute,
	))
	require.Len(t, transition.Effects(), 1)
	launch := transition.Effects()[0]

	tests := []struct {
		name       string
		outcome    LaunchOutcome
		wantFacts  int
		wantLaunch supervisorLaunchCompletionKind
	}{
		{
			name: "released before boundary", outcome: LaunchReleasedBeforeBoundary,
			wantFacts: 1, wantLaunch: supervisorLaunchReleased,
		},
		{
			name: "released at boundary", outcome: LaunchReleasedAtBoundary,
			wantFacts: 1, wantLaunch: supervisorLaunchReleased,
		},
		{
			name: "released after boundary", outcome: LaunchReleasedAfterBoundary,
			wantFacts: 2, wantLaunch: supervisorLaunchReleased,
		},
		{
			name: "proven not released", outcome: LaunchProvenNotReleased,
			wantFacts: 1, wantLaunch: supervisorLaunchProvenNotReleased,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts, ok := machine.LaunchFacts(launch, test.outcome)

			require.True(t, ok)
			require.Len(t, facts, test.wantFacts)
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
		outcome RunningOutcome
		kind    supervisorRunningFactKind
	}{
		{name: "passed", outcome: RunningPassed, kind: supervisorRunningRootExited},
		{name: "failed", outcome: RunningFailed, kind: supervisorRunningRootExited},
		{name: "deadline", outcome: RunningAtDeadline},
		{name: "after deadline", outcome: RunningAfterDeadline, kind: supervisorRunningRootExited},
		{name: "fuse", outcome: RunningFuse, kind: supervisorRunningFuseObserved},
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

func TestSupervisorMachineProducesCompletionFactsFromItsOwnEffect(t *testing.T) {
	machine, effects := runningSupervisorMachine(t, AutomaticProfile)
	wait := effectOfKind(t, effects, supervisorWaitRoot)
	sample := effectOfKind(t, effects, supervisorSampleRunning)
	running, ok := machine.RunningFact(wait, sample, RunningFailed)
	require.True(t, ok)
	machine, transition := machine.Apply(running)
	observe := effectOfKind(t, transition.Effects(), supervisorObserveEmptiness)

	fact, ok := machine.CompletionFact(observe, CompletionBeforeBoundary)

	require.True(t, ok)
	assert.Equal(t, supervisorDrainCompleted, fact.kind)
	require.NotNil(t, fact.drain)
	assert.Equal(t, supervisorDrainObservedEmpty, fact.drain.kind)
	assert.Equal(t, observe.token, fact.drain.action.token)
}

func runningSupervisorMachine(t *testing.T, profile Profile) (*Machine, []Effect) {
	t.Helper()
	registeredAt := time.Unix(100, 0)
	machine, transition := NewMachine().Apply(ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), profile, time.Minute,
	))
	launch := effectOfKind(t, transition.Effects(), supervisorLaunchNative)
	facts, ok := machine.LaunchFacts(launch, LaunchReleasedBeforeBoundary)
	require.True(t, ok)
	require.Len(t, facts, 1)
	machine, transition = machine.Apply(facts[0])
	assert.Equal(t, supervisionLaunchReleasedEvent, transition.Event().outcome)

	return machine, transition.Effects()
}

func effectOfKind(t *testing.T, effects []Effect, kind supervisorActionKind) Effect {
	t.Helper()
	for _, effect := range effects {
		if effect.kind == kind {
			return effect
		}
	}
	require.Failf(t, "effect not found", "kind %d in %v", kind, effects)

	return Effect{}
}
