package supervision_test

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachinePublishesCanonicalTransitions(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	fact := supervision.ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second),
		supervision.AutomaticProfile, time.Minute,
	)

	left, transition := supervision.NewMachine().Apply(fact)

	assert.Equal(t, supervision.AttemptRegisteredEvent, transition.Event().Kind())
	assert.Equal(t, supervision.Generation(1), transition.Event().Generation())
	assert.Equal(t, supervision.Identity("mutant-1"), transition.Event().Attempt())
	require.Len(t, transition.Effects(), 1)
	assert.Equal(t, supervision.LaunchNativeEffect, transition.Effects()[0].Kind())
	assert.Equal(t, supervision.Generation(1), transition.Effects()[0].Generation())

	right, repeated := supervision.NewMachine().Apply(fact)
	assert.True(t, left.Projection().Equal(right.Projection()))
	assert.True(t, transition.Event().Equal(repeated.Event()))
	assert.True(t, transition.Effects()[0].Equal(repeated.Effects()[0]))
	assert.True(t, supervision.NewMachine().Projection().Quiescent())
}

func TestMachineDerivesFactsFromPublishedEffects(t *testing.T) {
	registeredAt := time.Unix(100, 0)
	machine, transition := supervision.NewMachine().Apply(supervision.ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second),
		supervision.AutomaticProfile, time.Minute,
	))
	launch := transition.Effects()[0]

	for _, test := range []struct {
		name      string
		outcome   supervision.LaunchOutcome
		wantKinds []supervision.FactKind
	}{
		{name: "before boundary", outcome: supervision.LaunchReleasedBeforeBoundary, wantKinds: []supervision.FactKind{supervision.LaunchCompletedFact}},
		{name: "at boundary", outcome: supervision.LaunchReleasedAtBoundary, wantKinds: []supervision.FactKind{supervision.LaunchBoundaryFact}},
		{name: "after boundary", outcome: supervision.LaunchReleasedAfterBoundary, wantKinds: []supervision.FactKind{supervision.LaunchBoundaryFact, supervision.LaunchCompletedFact}},
		{name: "not released", outcome: supervision.LaunchProvenNotReleased, wantKinds: []supervision.FactKind{supervision.LaunchCompletedFact}},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts, ok := machine.LaunchFacts(launch, test.outcome)
			require.True(t, ok)
			require.Len(t, facts, len(test.wantKinds))
			for index := range facts {
				assert.Equal(t, test.wantKinds[index], facts[index].Kind())
			}
		})
	}

	facts, ok := machine.LaunchFacts(launch, supervision.LaunchReleasedBeforeBoundary)
	require.True(t, ok)
	machine, transition = machine.Apply(facts[0])
	wait := supervisionEffect(t, transition.Effects(), supervision.WaitRootEffect)
	sample := supervisionEffect(t, transition.Effects(), supervision.SampleRunningEffect)
	running, ok := machine.RunningFact(wait, sample, supervision.RunningPassed)
	require.True(t, ok)
	assert.Equal(t, supervision.RunningObservedFact, running.Kind())
	machine, _ = machine.Apply(running)

	stop, disposition := machine.StopFact(1)
	assert.Equal(t, supervision.StopReady, disposition)
	assert.Equal(t, supervision.RunningObservedFact, stop.Kind())
}

func supervisionEffect(
	t *testing.T,
	effects []supervision.Effect,
	kind supervision.EffectKind,
) supervision.Effect {
	t.Helper()
	for _, effect := range effects {
		if effect.Kind() == kind {
			return effect
		}
	}
	require.Failf(t, "effect not found", "kind %d in %v", kind, effects)

	return supervision.Effect{}
}
