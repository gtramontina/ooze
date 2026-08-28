package supervision_test

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
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

func TestMachineStopDispositionFollowsReachableLifecycle(t *testing.T) {
	running, _ := supervisionRunningMachine(t)
	stop, disposition := running.StopFact(1)
	require.Equal(t, supervision.StopReady, disposition)
	intentLatched, transition := running.Apply(stop)
	force := supervisionEffect(t, transition.Effects(), supervision.ForceOwnedEffect)

	emergencyFact, ok := running.DeterministicEmergencyFact(time.Unix(103, 0), 5*time.Second)
	require.True(t, ok)
	emergencyDraining, _ := running.Apply(emergencyFact)

	forced, transition := applySupervisionCompletion(t, intentLatched, force, supervision.CompletionBeforeBoundary)
	observe := supervisionEffect(t, transition.Effects(), supervision.ObserveEmptinessEffect)

	empty, transition := applySupervisionCompletion(t, forced, observe, supervision.CompletionBeforeBoundary)
	capture := supervisionEffect(t, transition.Effects(), supervision.CaptureOutputEffect)
	captured, transition := applySupervisionCompletion(t, empty, capture, supervision.CompletionBeforeBoundary)
	seal := supervisionEffect(t, transition.Effects(), supervision.SealStopAdmissionEffect)
	releasing, transition := applySupervisionCompletion(t, captured, seal, supervision.CompletionBeforeBoundary)
	release := supervisionEffect(t, transition.Effects(), supervision.ReleaseDomainEffect)
	settling, _ := applySupervisionCompletion(t, releasing, release, supervision.CompletionBeforeBoundary)

	residual, transition := applySupervisionCompletion(t, forced, observe, supervision.CompletionAtBoundary)
	capture = supervisionEffect(t, transition.Effects(), supervision.CaptureOutputEffect)
	residual, transition = applySupervisionCompletion(t, residual, capture, supervision.CompletionBeforeBoundary)
	seal = supervisionEffect(t, transition.Effects(), supervision.SealStopAdmissionEffect)
	transferring, transition := applySupervisionCompletion(t, residual, seal, supervision.CompletionBeforeBoundary)
	transfer := supervisionEffect(t, transition.Effects(), supervision.TransferResidualCustodyEffect)

	runtime := processruntime.New(1)
	registration := runtime.RegisterCampaign(1)
	require.Equal(t, processruntime.CampaignRegistered, registration.Decision())
	request := runtime.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "mutant-1", Class: processruntime.SharedAdmission,
		Profile: processruntime.AutomaticProfile, Deadline: time.Minute,
	})
	grant, received := request.Receive()
	require.True(t, received)
	start := runtime.CommitStart(grant, processruntime.NewStartCell())
	require.Equal(t, processruntime.StartAccepted, start.Decision())
	runtime.Observe(start.Generation(), start.Launch(func(processruntime.Generation) processruntime.Observation {
		return processruntime.Owned()
	}))
	closure := runtime.Close("stop disposition fixture")
	require.NotZero(t, closure.Epoch())
	receipt := runtime.Observe(start.Generation(), processruntime.DrainUnconfirmed())
	runtimeFact, ok := transferring.RuntimeReceiptFactFor(transfer, receipt)
	require.True(t, ok)
	awaitingEmergency, _ := transferring.Apply(runtimeFact)

	registeredAt := time.Unix(200, 0)
	prospective, registered := supervision.NewMachine().Apply(supervision.ProspectiveRegistration(
		2, "mutant-2", registeredAt, registeredAt.Add(time.Second), supervision.AutomaticProfile, time.Minute,
	))
	prospectiveLaunch := supervisionEffect(t, registered.Effects(), supervision.LaunchNativeEffect)
	boundaryFacts, ok := prospective.LaunchFacts(prospectiveLaunch, supervision.LaunchReleasedAfterBoundary)
	require.True(t, ok)
	require.Len(t, boundaryFacts, 2)
	prospective, _ = prospective.Apply(boundaryFacts[0])
	lateNotReleased, ok := prospectiveLaunch.LaunchNotReleasedFact(
		registeredAt.Add(time.Second+time.Nanosecond), supervision.LaunchFailed, 1,
	)
	require.True(t, ok)
	closingProspective, _ := prospective.Apply(lateNotReleased)

	for _, test := range []struct {
		name    string
		machine *supervision.Machine
		want    supervision.StopDisposition
	}{
		{name: "running", machine: running, want: supervision.StopReady},
		{name: "intent latched", machine: intentLatched, want: supervision.StopReady},
		{name: "emergency draining", machine: emergencyDraining, want: supervision.StopReady},
		{name: "releasing domain", machine: releasing, want: supervision.StopResolved},
		{name: "transferring residual custody", machine: transferring, want: supervision.StopResolved},
		{name: "settling runtime", machine: settling, want: supervision.StopResolved},
		{name: "awaiting emergency settlement", machine: awaitingEmergency, want: supervision.StopResolved},
		{name: "closing prospective", machine: closingProspective, want: supervision.StopNotReady},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, disposition := test.machine.StopFact(1)
			if test.name == "closing prospective" {
				_, disposition = test.machine.StopFact(2)
			}
			assert.Equal(t, test.want, disposition)
		})
	}
}

func supervisionRunningMachine(t *testing.T) (*supervision.Machine, supervision.Effect) {
	t.Helper()
	registeredAt := time.Unix(100, 0)
	machine, transition := supervision.NewMachine().Apply(supervision.ProspectiveRegistration(
		1, "mutant-1", registeredAt, registeredAt.Add(time.Second), supervision.AutomaticProfile, time.Minute,
	))
	launch := supervisionEffect(t, transition.Effects(), supervision.LaunchNativeEffect)
	facts, ok := machine.LaunchFacts(launch, supervision.LaunchReleasedBeforeBoundary)
	require.True(t, ok)
	require.Len(t, facts, 1)
	machine, _ = machine.Apply(facts[0])

	return machine, launch
}

func applySupervisionCompletion(
	t *testing.T,
	machine *supervision.Machine,
	effect supervision.Effect,
	position supervision.CompletionPosition,
) (*supervision.Machine, supervision.Transition) {
	t.Helper()
	fact, ok := machine.CompletionFact(effect, position)
	require.True(t, ok)

	return machine.Apply(fact)
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
