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

type campaignMachineHarness struct {
	t               *testing.T
	definition      campaign.Definition
	machine         campaign.Machine
	runtime         processruntime.Replay
	runtimeCampaign processruntime.Campaign
	runtimeBinding  campaign.RuntimeBinding
	effects         []campaign.Effect
}

type launchedMachineAttempt struct {
	effect     campaign.Effect
	generation processruntime.Generation
}

func newCampaignMachineHarness(
	t *testing.T,
	mutants []string,
	profile processruntime.Profile,
	peers int,
) *campaignMachineHarness {
	t.Helper()
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"go", "test"},
		Profile: profile, Peers: peers,
	}
	machine, transition := campaign.NewMachine(definition)
	harness := &campaignMachineHarness{
		t: t, definition: definition, machine: machine, runtime: processruntime.NewReplay(peers),
		effects: transition.Effects(),
	}
	registered := harness.applyRuntime(harness.effects[0]).Registration()
	harness.runtimeCampaign = registered.Campaign()
	harness.runtimeBinding = campaign.BindRuntime(registered)
	harness.advance(campaign.Registered(registered))
	harness.advance(campaign.SnapshotEstablished("snapshot-a"))
	harness.advance(campaign.CatalogueDiscovered("snapshot-a", mutants))
	return harness
}

func newRunningCampaignMachineHarness(
	t *testing.T,
	mutants []string,
	peers int,
) (*campaignMachineHarness, []campaign.Effect) {
	t.Helper()
	harness := newCampaignMachineHarness(t, mutants, processruntime.AutomaticProfile, peers)
	baseline := harness.launch(harness.effects[0], "workspace-baseline")
	effects := harness.settle(baseline, supervision.Settled{
		Exit: supervision.ExitStatus{},
		ExecutionData: supervision.ExecutionData{
			Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second,
		},
	}, 20*time.Second)
	require.Equal(t, []campaign.EffectKind{campaign.ReleaseWorkspaceEffect}, effectKinds(effects))
	harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-baseline"))
	return harness, harness.effects
}

func (harness *campaignMachineHarness) advance(fact campaign.Fact) []campaign.Effect {
	harness.t.Helper()
	var transition campaign.Transition
	harness.machine, transition = harness.machine.Apply(fact)
	harness.effects = transition.Effects()
	return harness.effects
}

func (harness *campaignMachineHarness) applyRuntime(effect campaign.Effect) processruntime.ReplayResult {
	harness.t.Helper()
	cut, ok := harness.runtimeBinding.Cut(effect, harness.definition)
	require.True(harness.t, ok)
	var result processruntime.ReplayResult
	harness.runtime, result = harness.runtime.Apply(cut)
	return result
}

func (harness *campaignMachineHarness) launch(effect campaign.Effect, workspace string) launchedMachineAttempt {
	harness.t.Helper()
	attempt := harness.start(effect, workspace)
	owned := harness.applyCut(processruntime.ObserveAttemptCut(attempt.generation, processruntime.Owned())).Receipt()
	harness.advance(campaign.AttemptLaunched(attempt.effect, supervision.Owned{}, owned))
	return attempt
}

func (harness *campaignMachineHarness) start(effect campaign.Effect, workspace string) launchedMachineAttempt {
	harness.t.Helper()
	effects := harness.advance(campaign.WorkspaceMaterialized(effect, workspace))
	require.Len(harness.t, effects, 1)
	if effects[0].Kind() == campaign.BindConfirmationBarrierEffect {
		bound := harness.applyRuntime(effects[0]).Barrier()
		effects = harness.advance(campaign.ConfirmationBarrierBound(effects[0], bound))
	} else {
		admitted := harness.applyRuntime(effects[0]).Admission()
		require.Len(harness.t, admitted.Deliveries(), 1)
		effects = harness.advance(campaign.AdmissionGranted(effects[0], admitted.Deliveries()[0]))
	}
	require.Equal(harness.t, []campaign.EffectKind{campaign.RequestStartCommitmentEffect}, effectKinds(effects))
	started := harness.applyRuntime(effects[0]).Start()
	effects = harness.advance(campaign.StartCommitted(effects[0], started))
	require.Equal(harness.t, []campaign.EffectKind{campaign.LaunchAttemptEffect}, effectKinds(effects))
	return launchedMachineAttempt{effect: effects[0], generation: started.Generation()}
}

func (harness *campaignMachineHarness) settle(
	attempt launchedMachineAttempt,
	terminal supervision.Terminal,
	resolved time.Duration,
) []campaign.Effect {
	harness.t.Helper()
	spec := attempt.effect.Spec()
	var observation processruntime.Observation
	switch terminal := terminal.(type) {
	case supervision.Settled:
		observation = processruntime.Settled(spec.Profile, spec.Deadline)
	case supervision.Tripped:
		_, fuse := terminal.Trip.(supervision.FuseTrip)
		observation = processruntime.Tripped(fuse, spec.Profile, spec.Deadline)
	case supervision.Stopped:
		observation = processruntime.Stopped()
	case supervision.Infrastructure:
		observation = processruntime.Infrastructure("campaign fixture")
	case supervision.DrainUnconfirmed:
		observation = processruntime.DrainUnconfirmed()
	default:
		require.FailNow(harness.t, "unsupported terminal")
	}
	receipt := harness.applyCut(processruntime.ObserveAttemptCut(attempt.generation, observation)).Receipt()
	fact := campaign.AttemptTerminal(attempt.effect, terminal, receipt, resolved)
	if attempt.effect.AttemptRole() == campaign.ConfirmationAttempt && receipt.ConfirmationObserved() {
		queue := harness.applyCut(processruntime.CompleteConfirmationQueueCut(harness.runtimeCampaign)).Queue()
		fact = fact.WithConfirmationQueueCompleted(queue)
	}
	return harness.advance(fact)
}

func (harness *campaignMachineHarness) applyCut(cut processruntime.Cut) processruntime.ReplayResult {
	harness.t.Helper()
	var result processruntime.ReplayResult
	harness.runtime, result = harness.runtime.Apply(cut)
	return result
}

func effectKinds(effects []campaign.Effect) []campaign.EffectKind {
	kinds := make([]campaign.EffectKind, len(effects))
	for index, effect := range effects {
		kinds[index] = effect.Kind()
	}
	return kinds
}

func TestMachineRunsOneBaselineAndOnePrimaryPerMutant(t *testing.T) {
	harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a", "mutant-b"}, 2)
	require.Len(t, primaries, 2)

	t.Run("fans out primaries only after the baseline settles", func(t *testing.T) {
		assert.Equal(t, campaign.PrimaryAttempt, primaries[0].AttemptRole())
		assert.Equal(t, campaign.PrimaryAttempt, primaries[1].AttemptRole())
		assert.Equal(t, "snapshot-a", primaries[0].Snapshot())
	})

	first := harness.launch(primaries[0], "workspace-a")
	second := harness.launch(primaries[1], "workspace-b")
	harness.settle(first, supervision.Settled{
		ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second, CommandDuration: time.Second},
	}, 0)
	harness.settle(second, supervision.Settled{
		Exit:          supervision.ExitStatus{Code: 1},
		ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second, CommandDuration: time.Second},
	}, 0)
	harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-a"))
	effects := harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-b"))
	require.Equal(t, []campaign.EffectKind{campaign.ReleaseSnapshotEffect}, effectKinds(effects))
	effects = harness.advance(campaign.ResourceSettled(campaign.SnapshotResource, "snapshot-a"))
	require.Equal(t, []campaign.EffectKind{campaign.ProposeTerminalEffect}, effectKinds(effects))
	committed := harness.applyRuntime(effects[0]).Terminal()
	harness.advance(campaign.TerminalCommitted(committed.Decision()))

	t.Run("retains ordered mutation evidence", func(t *testing.T) {
		assert.Equal(t, campaign.CompletedOutcome, harness.machine.Outcome().Kind())
		require.Len(t, harness.machine.Mutations(), 2)
		assert.Equal(t, "mutant-a", harness.machine.Mutations()[0].Identity())
		assert.Equal(t, campaign.ManagedSurvived, harness.machine.Mutations()[0].Result())
		assert.Equal(t, campaign.ManagedKilled, harness.machine.Mutations()[1].Result())
		assert.Equal(t, campaign.AttemptSettled, harness.machine.Mutations()[0].Primary())
		assert.Equal(t, 3, harness.machine.CommandCount())
		assert.Empty(t, harness.machine.Projection().Obligations())
	})
}

func TestMachineAbortsWithoutInventingMoreWork(t *testing.T) {
	t.Run("a proven baseline launch failure starts no primary", func(t *testing.T) {
		harness := newCampaignMachineHarness(t, []string{"mutant-a"}, processruntime.AutomaticProfile, 1)
		baseline := harness.start(harness.effects[0], "workspace-baseline")
		receipt := harness.applyCut(processruntime.ObserveAttemptCut(
			baseline.generation, processruntime.NotReleased(false),
		)).Receipt()
		effects := harness.advance(campaign.AttemptLaunched(
			baseline.effect, supervision.NotReleased{Kind: supervision.LaunchFailed}, receipt,
		))

		assert.Equal(t, []campaign.EffectKind{campaign.ReleaseWorkspaceEffect}, effectKinds(effects))
		assert.Equal(t, 1, harness.machine.CommandCount())
		harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-baseline"))
		assert.Equal(t, []campaign.EffectKind{campaign.ReleaseSnapshotEffect}, effectKinds(harness.effects))
	})

	t.Run("an infrastructure failure stops an owned peer without retry", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a", "mutant-b"}, 2)
		first := harness.launch(primaries[0], "workspace-a")
		second := harness.launch(primaries[1], "workspace-b")
		effects := harness.settle(first, supervision.Infrastructure{
			Cause: supervision.CensusFailed, Err: assert.AnError,
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)

		assert.Equal(t, []campaign.EffectKind{campaign.StopAttemptEffect, campaign.ReleaseWorkspaceEffect}, effectKinds(effects))
		assert.Equal(t, second.effect.Attempt(), effects[0].Attempt())
		harness.settle(second, supervision.Stopped{
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)
		assert.Equal(t, 3, harness.machine.CommandCount())
	})

	t.Run("a workspace failure stops an owned peer", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a", "mutant-b"}, 2)
		peer := harness.launch(primaries[0], "workspace-a")
		effects := harness.advance(campaign.WorkspaceMaterializationFailed(
			primaries[1], "workspace copy failed", nil,
		))

		assert.Equal(t, []campaign.EffectKind{campaign.StopAttemptEffect}, effectKinds(effects))
		assert.Equal(t, peer.effect.Attempt(), effects[0].Attempt())
	})

	t.Run("a workspace release failure stops an owned peer", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a", "mutant-b"}, 2)
		first := harness.launch(primaries[0], "workspace-a")
		second := harness.launch(primaries[1], "workspace-b")
		harness.settle(first, supervision.Settled{
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)
		effects := harness.advance(campaign.ResourceSettlementFailed(
			campaign.WorkspaceResource, "workspace-a", "remove failed",
		))

		assert.Equal(t, []campaign.EffectKind{campaign.StopAttemptEffect}, effectKinds(effects))
		assert.Equal(t, second.effect.Attempt(), effects[0].Attempt())
	})
}

func TestMachineRejectsContradictoryEvidence(t *testing.T) {
	t.Run("a primary terminal must carry its resolved deadline", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a"}, 1)
		primary := harness.launch(primaries[0], "workspace-a")
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			harness.settle(primary, supervision.Settled{
				ExecutionData: supervision.ExecutionData{Deadline: time.Nanosecond},
			}, 0)
		}()
		violation, ok := recovered.(campaign.Violation)
		require.True(t, ok)
		assert.Equal(t, "campaign observe terminal", violation.Operation())
	})

	t.Run("terminal facts retain evidence distinctions", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a"}, 1)
		primary := harness.launch(primaries[0], "workspace-a")
		firstReceipt := harness.applyCut(processruntime.ObserveAttemptCut(
			primary.generation, processruntime.Settled(processruntime.AutomaticProfile, 20*time.Second),
		)).Receipt()
		first := campaign.AttemptTerminal(primary.effect, supervision.Settled{
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, firstReceipt, 0)
		second := campaign.AttemptTerminal(primary.effect, supervision.Settled{
			Exit:          supervision.ExitStatus{Code: 1},
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, firstReceipt, 0)

		assert.False(t, first.Equal(second))
	})
}

func newConfirmationMachineHarness(
	t *testing.T,
) (*campaignMachineHarness, launchedMachineAttempt) {
	t.Helper()
	harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a", "mutant-b"}, 2)
	first := harness.launch(primaries[0], "workspace-a")
	second := harness.launch(primaries[1], "workspace-b")
	harness.settle(first, supervision.Tripped{
		Trip: supervision.AutomaticDeadlineTrip{},
		ExecutionData: supervision.ExecutionData{
			Deadline: 20 * time.Second, CommandDuration: 20 * time.Second,
			BoundFired: supervision.CommandDeadlineFired,
		},
	}, 0)
	harness.settle(second, supervision.Settled{
		ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second, CommandDuration: time.Second},
	}, 0)
	harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-a"))
	effects := harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-b"))
	require.Equal(t, []campaign.EffectKind{campaign.MaterializeWorkspaceEffect}, effectKinds(effects))
	return harness, harness.launch(effects[0], "workspace-confirmation")
}

func TestMachineConfirmsOnlyAttributableDeadlinePressure(t *testing.T) {
	t.Run("confirmation infrastructure aborts without scoring the mutant", func(t *testing.T) {
		harness, confirmation := newConfirmationMachineHarness(t)
		effects := harness.settle(confirmation, supervision.Infrastructure{
			Cause: supervision.CensusFailed, Err: assert.AnError,
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)

		assert.Equal(t, []campaign.EffectKind{campaign.ReleaseWorkspaceEffect}, effectKinds(effects))
	})

	t.Run("confirmation fuse remains an attributable runaway", func(t *testing.T) {
		harness, confirmation := newConfirmationMachineHarness(t)
		effects := harness.settle(confirmation, supervision.Tripped{
			Trip:          supervision.FuseTrip{Live: 9},
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)
		assert.Equal(t, []campaign.EffectKind{campaign.ReleaseWorkspaceEffect}, effectKinds(effects))
		harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-confirmation"))
		require.Equal(t, []campaign.EffectKind{campaign.ReleaseSnapshotEffect}, effectKinds(harness.effects))
		harness.advance(campaign.ResourceSettled(campaign.SnapshotResource, "snapshot-a"))
		committed := harness.applyRuntime(harness.effects[0]).Terminal()
		harness.advance(campaign.TerminalCommitted(committed.Decision()))

		require.Len(t, harness.machine.Mutations(), 2)
		assert.Equal(t, campaign.ManagedRunaway, harness.machine.Mutations()[0].Result())
	})

	t.Run("confirmation requires its own runtime receipt", func(t *testing.T) {
		harness, confirmation := newConfirmationMachineHarness(t)
		receipt := harness.applyCut(processruntime.ObserveAttemptCut(
			confirmation.generation,
			processruntime.Settled(processruntime.AutomaticProfile, 20*time.Second),
		)).Receipt()
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			harness.advance(campaign.AttemptTerminal(confirmation.effect, supervision.Settled{
				ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
			}, receipt, 0))
		}()
		violation, ok := recovered.(campaign.Violation)
		require.True(t, ok)
		assert.Equal(t, "campaign observe confirmation terminal", violation.Operation())
	})
}
