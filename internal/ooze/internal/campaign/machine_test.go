package campaign_test

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMachineCommitsAnEmptyCatalogueWithoutLaunchingAnAttempt(t *testing.T) {
	machine, transition := campaign.NewMachine(campaign.Definition{
		Identity: "campaign-a",
		Lineage:  11,
		Command:  []string{"go", "test", "./..."},
		Profile:  processruntime.AutomaticProfile,
		Peers:    4,
	})
	require.Equal(t, []campaign.EffectKind{campaign.RegisterEffect}, transition.EffectKinds())
	runtime := processruntime.NewReplay(4)
	runtime, registered := runtime.Apply(processruntime.RegisterCampaignCut(11))

	t.Run("records the runtime registration", func(t *testing.T) {
		machine, transition = machine.Apply(campaign.Registered(registered.Registration()))
		assert.Equal(t, []campaign.EffectKind{campaign.EstablishSnapshotEffect}, transition.EffectKinds())
	})
	_ = runtime

	t.Run("establishes and discovers the repository snapshot", func(t *testing.T) {
		machine, transition = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
		assert.Equal(t, []campaign.EffectKind{campaign.DiscoverCatalogueEffect}, transition.EffectKinds())

		machine, transition = machine.Apply(campaign.CatalogueDiscovered("snapshot-a", nil))
		assert.Equal(t, []campaign.EffectKind{campaign.ReleaseSnapshotEffect}, transition.EffectKinds())
	})

	t.Run("commits no mutants after releasing the snapshot", func(t *testing.T) {
		machine, transition = machine.Apply(campaign.ResourceSettled(campaign.SnapshotResource, "snapshot-a"))
		assert.Equal(t, []campaign.EffectKind{campaign.ProposeTerminalEffect}, transition.EffectKinds())

		machine, transition = machine.Apply(campaign.TerminalCommitted(processruntime.TerminalCommitted))
		assert.Empty(t, transition.EffectKinds())
		assert.Equal(t, campaign.NoMutantsOutcome, machine.Outcome().Kind())
		assert.Empty(t, machine.Projection().Obligations())
	})
}

func TestCanonicalCampaignEventDoesNotRetainRuntimeAuthority(t *testing.T) {
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"test"},
		Profile: processruntime.SerialProfile, Peers: 1,
	}
	runtime := processruntime.NewReplay(1)
	runtime, registered := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
	machine, _ := campaign.NewMachine(definition)
	_, registration := machine.Apply(campaign.Registered(registered.Registration()))

	fresh, _ := campaign.NewMachine(definition)
	assert.False(t, fresh.Accepts(registration.Event().Canonical().Fact()))
	_ = runtime
}

func TestMachineOwnsBaselinePolicyAndEarlierProjections(t *testing.T) {
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"go", "test"},
		Env: []string{"A=1"}, Profile: processruntime.AutomaticProfile, Peers: 2,
	}
	runtime := processruntime.NewReplay(2)
	machine, _ := campaign.NewMachine(definition)
	runtime, registered := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
	machine, _ = machine.Apply(campaign.Registered(registered.Registration()))
	machine, _ = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
	beforeCatalogue := machine.Projection()
	machine, transition := machine.Apply(campaign.CatalogueDiscovered("snapshot-a", []string{"mutant-a"}))

	t.Run("owns the fixed baseline deadline", func(t *testing.T) {
		require.Equal(t, []campaign.EffectKind{campaign.MaterializeWorkspaceEffect}, transition.EffectKinds())
		materialize := transition.Effects()[0]
		machine, transition = machine.Apply(campaign.WorkspaceMaterialized(materialize, "workspace-a"))
		require.Equal(t, []campaign.EffectKind{campaign.RequestAdmissionEffect}, transition.EffectKinds())
		request := transition.Effects()[0]
		cut, ok := request.RuntimeCut(definition)
		require.True(t, ok)
		runtime, admitted := runtime.Apply(cut)
		require.Len(t, admitted.Admission().Deliveries(), 1)
		machine, transition = machine.Apply(campaign.AdmissionGranted(request, admitted.Admission().Deliveries()[0]))
		require.Equal(t, []campaign.EffectKind{campaign.RequestStartCommitmentEffect}, transition.EffectKinds())
		start := transition.Effects()[0]
		cut, ok = start.RuntimeCut(definition)
		require.True(t, ok)
		_, started := runtime.Apply(cut)
		machine, transition = machine.Apply(campaign.StartCommitted(start, started.Start()))
		require.Equal(t, []campaign.EffectKind{campaign.LaunchAttemptEffect}, transition.EffectKinds())
		assert.Equal(t, 10*time.Minute, transition.Effects()[0].Spec().Deadline)
		assert.Equal(t, []string{"go", "test"}, transition.Effects()[0].Spec().Command)
		assert.Equal(t, []string{"A=1"}, transition.Effects()[0].Spec().Env)
	})

	t.Run("does not mutate an earlier projection", func(t *testing.T) {
		fork := beforeCatalogue.Fork()
		assert.True(t, beforeCatalogue.Equal(fork.Projection()))
		assert.Empty(t, beforeCatalogue.Catalogue())
	})

	_ = runtime
}

func TestMachineRejectsPreparationWithoutStartingACommand(t *testing.T) {
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
	}

	t.Run("recursive registration", func(t *testing.T) {
		runtime := processruntime.NewReplay(1)
		runtime, _ = runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
		_, rejected := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
		machine, _ := campaign.NewMachine(definition)
		machine, transition := machine.Apply(campaign.Registered(rejected.Registration()))
		assert.Empty(t, transition.EffectKinds())
		assert.Equal(t, campaign.AbortedOutcome, machine.Outcome().Kind())
		assert.Zero(t, machine.CommandCount())
	})

	t.Run("snapshot failure", func(t *testing.T) {
		runtime := processruntime.NewReplay(1)
		_, registered := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
		machine, _ := campaign.NewMachine(definition)
		machine, _ = machine.Apply(campaign.Registered(registered.Registration()))
		machine, transition := machine.Apply(campaign.PreparationFailed(false, "snapshot failed"))
		assert.Equal(t, []campaign.EffectKind{campaign.ProposeTerminalEffect}, transition.EffectKinds())
		assert.Zero(t, machine.CommandCount())
	})
}

func TestMachineRetainsRecordedMutationDeadline(t *testing.T) {
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 2,
	}
	runtime := processruntime.NewReplay(2)
	machine, _ := campaign.NewMachine(definition)
	runtime, registered := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
	machine, _ = machine.Apply(campaign.Registered(registered.Registration()))
	machine, _ = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
	machine, transition := machine.Apply(campaign.CatalogueDiscovered("snapshot-a", []string{"mutant-a"}))
	materialize := transition.Effects()[0]
	machine, transition = machine.Apply(campaign.WorkspaceMaterialized(materialize, "workspace-a"))
	request := transition.Effects()[0]
	cut, ok := request.RuntimeCut(definition)
	require.True(t, ok)
	runtime, admitted := runtime.Apply(cut)
	machine, transition = machine.Apply(campaign.AdmissionGranted(request, admitted.Admission().Deliveries()[0]))
	start := transition.Effects()[0]
	cut, ok = start.RuntimeCut(definition)
	require.True(t, ok)
	runtime, started := runtime.Apply(cut)
	machine, transition = machine.Apply(campaign.StartCommitted(start, started.Start()))
	launch := transition.Effects()[0]
	runtime, owned := runtime.Apply(processruntime.ObserveAttemptCut(
		started.Start().Generation(), processruntime.Owned(),
	))
	machine, _ = machine.Apply(campaign.AttemptLaunched(launch, supervision.Owned{}, owned.Receipt()))
	terminal := supervision.Settled{
		ExecutionData: supervision.ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second,
		},
	}
	_, settled := runtime.Apply(processruntime.ObserveAttemptCut(
		started.Start().Generation(), processruntime.Settled(definition.Profile, 10*time.Minute),
	))
	machine, transition = machine.Apply(campaign.AttemptTerminal(launch, terminal, settled.Receipt(), time.Nanosecond))
	assert.Equal(t, []campaign.EffectKind{campaign.ReleaseWorkspaceEffect}, transition.EffectKinds())
	deadline, ok := transition.Event().Fact().ResolvedMutationDeadline()
	require.True(t, ok)
	assert.Equal(t, time.Nanosecond, deadline)
	assert.Equal(t, 1, machine.CommandCount())
}
