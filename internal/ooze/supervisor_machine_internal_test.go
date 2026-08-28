package ooze

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorMachinePublishesAcceptedFactsAndEffects(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: 1, attempt: "mutant-1",
		at: registeredAt, launchBy: registeredAt.Add(time.Second),
		profile: AutomaticProfile, commandDeadline: time.Minute,
	}
	machine := newSupervisorMachine()

	transition := machine.Apply(fact)

	assert.Equal(t, fact, transition.Event().Fact())
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
	transition := machine.Apply(fact)

	event := transition.Event()
	effects := transition.Effects()
	corrupted := event.Fact()
	corrupted.attempt = "corrupted"
	effects[0].kind = supervisorDeliverTerminal

	assert.Equal(t, attemptIdentity("mutant-1"), transition.Event().Fact().attempt)
	assert.Equal(t, supervisorLaunchNative, transition.Effects()[0].kind)
}
