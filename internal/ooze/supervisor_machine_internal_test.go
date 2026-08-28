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
	projection.attempts[0].attempt = "corrupted"

	assert.Equal(t, attemptIdentity("mutant-1"), machine.Projection().attempts[0].attempt)
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
