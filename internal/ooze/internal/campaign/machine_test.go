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
	require.Equal(t, []testEffectKind{registerEffect}, effectKinds(transition.Effects()))
	runtime := processruntime.NewReplay(4)
	runtime, registered := runtime.Apply(processruntime.RegisterCampaignCut(11))

	t.Run("records the runtime registration", func(t *testing.T) {
		machine, transition = machine.Apply(campaign.Registered(registered.Registration()))
		assert.Equal(t, []testEffectKind{establishSnapshotEffect}, effectKinds(transition.Effects()))
	})
	_ = runtime

	t.Run("establishes and discovers the repository snapshot", func(t *testing.T) {
		machine, transition = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
		assert.Equal(t, []testEffectKind{discoverCatalogueEffect}, effectKinds(transition.Effects()))

		machine, transition = machine.Apply(campaign.CatalogueDiscovered("snapshot-a", nil))
		assert.Equal(t, []testEffectKind{releaseSnapshotEffect}, effectKinds(transition.Effects()))
	})

	t.Run("commits no mutants after releasing the snapshot", func(t *testing.T) {
		machine, transition = machine.Apply(campaign.ResourceSettled(campaign.SnapshotResource, "snapshot-a"))
		assert.Equal(t, []testEffectKind{proposeTerminalEffect}, effectKinds(transition.Effects()))

		machine, transition = machine.Apply(campaign.TerminalCommitted(processruntime.TerminalCommitted))
		assert.Empty(t, transition.Effects())
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

func TestCanonicalCampaignProjectionDoesNotRetainGrantedAdmissionAuthority(t *testing.T) {
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"test"},
		Profile: processruntime.SerialProfile, Peers: 1,
	}
	runtime := processruntime.NewReplay(1)
	runtime, campaignRegistration := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
	binding := campaign.BindRuntime(campaignRegistration.Registration())
	machine, _ := campaign.NewMachine(definition)
	machine, _ = machine.Apply(campaign.Registered(campaignRegistration.Registration()))
	machine, _ = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
	machine, transition := machine.Apply(campaign.CatalogueDiscovered("snapshot-a", []string{"mutant-a"}))
	machine, transition = machine.Apply(campaign.WorkspaceMaterialized(transition.Effects()[0], "workspace-a"))
	request := transition.Effects()[0]
	runtimeRequest, ok := binding.RuntimeRequest(request, definition)
	require.True(t, ok)
	runtime, admitted := runtime.Apply(runtimeRequest.Cut())
	require.Len(t, admitted.Admission().Deliveries(), 1)
	machine, _ = machine.Apply(campaign.AdmissionGranted(request, admitted.Admission().Deliveries()[0]))
	runtime, closed := runtime.Apply(processruntime.CloseCut("test closure"))

	fork := machine.Projection().Canonical().Fork()
	_, transition = fork.Apply(campaign.RuntimeEmergencyStarted(closed.Closure()).Canonical())
	require.Equal(t, []testEffectKind{returnAdmissionEffect}, effectKinds(transition.Effects()))
	_, ok = binding.RuntimeRequest(transition.Effects()[0], definition)
	assert.False(t, ok)
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
	binding := campaign.BindRuntime(registered.Registration())
	machine, _ = machine.Apply(campaign.Registered(registered.Registration()))
	machine, _ = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
	beforeCatalogue := machine.Projection()
	machine, transition := machine.Apply(campaign.CatalogueDiscovered("snapshot-a", []string{"mutant-a"}))

	t.Run("owns the fixed baseline deadline", func(t *testing.T) {
		require.Equal(t, []testEffectKind{materializeWorkspaceEffect}, effectKinds(transition.Effects()))
		materialize := transition.Effects()[0]
		machine, transition = machine.Apply(campaign.WorkspaceMaterialized(materialize, "workspace-a"))
		require.Equal(t, []testEffectKind{requestAdmissionEffect}, effectKinds(transition.Effects()))
		request := transition.Effects()[0]
		runtimeRequest, ok := binding.RuntimeRequest(request, definition)
		require.True(t, ok)
		runtime, admitted := runtime.Apply(runtimeRequest.Cut())
		require.Len(t, admitted.Admission().Deliveries(), 1)
		machine, transition = machine.Apply(campaign.AdmissionGranted(request, admitted.Admission().Deliveries()[0]))
		require.Equal(t, []testEffectKind{requestStartCommitmentEffect}, effectKinds(transition.Effects()))
		start := transition.Effects()[0]
		runtimeRequest, ok = binding.RuntimeRequest(start, definition)
		require.True(t, ok)
		_, started := runtime.Apply(runtimeRequest.Cut())
		machine, transition = machine.Apply(campaign.StartCommitted(start, started.Start()))
		require.Equal(t, []testEffectKind{launchAttemptEffect}, effectKinds(transition.Effects()))
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
		assert.Empty(t, transition.Effects())
		assert.Equal(t, campaign.AbortedOutcome, machine.Outcome().Kind())
		assert.Zero(t, machine.CommandCount())
	})

	t.Run("snapshot failure", func(t *testing.T) {
		runtime := processruntime.NewReplay(1)
		_, registered := runtime.Apply(processruntime.RegisterCampaignCut(definition.Lineage))
		machine, _ := campaign.NewMachine(definition)
		machine, _ = machine.Apply(campaign.Registered(registered.Registration()))
		machine, transition := machine.Apply(campaign.PreparationFailed(false, "snapshot failed"))
		assert.Equal(t, []testEffectKind{proposeTerminalEffect}, effectKinds(transition.Effects()))
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
	binding := campaign.BindRuntime(registered.Registration())
	machine, _ = machine.Apply(campaign.Registered(registered.Registration()))
	machine, _ = machine.Apply(campaign.SnapshotEstablished("snapshot-a"))
	machine, transition := machine.Apply(campaign.CatalogueDiscovered("snapshot-a", []string{"mutant-a"}))
	materialize := transition.Effects()[0]
	machine, transition = machine.Apply(campaign.WorkspaceMaterialized(materialize, "workspace-a"))
	request := transition.Effects()[0]
	runtimeRequest, ok := binding.RuntimeRequest(request, definition)
	require.True(t, ok)
	runtime, admitted := runtime.Apply(runtimeRequest.Cut())
	machine, transition = machine.Apply(campaign.AdmissionGranted(request, admitted.Admission().Deliveries()[0]))
	start := transition.Effects()[0]
	runtimeRequest, ok = binding.RuntimeRequest(start, definition)
	require.True(t, ok)
	runtime, started := runtime.Apply(runtimeRequest.Cut())
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
	assert.Equal(t, []testEffectKind{releaseWorkspaceEffect}, effectKinds(transition.Effects()))
	deadline, ok := transition.Event().Fact().ResolvedMutationDeadline()
	require.True(t, ok)
	assert.Equal(t, time.Nanosecond, deadline)
	assert.Equal(t, 1, machine.CommandCount())
}
