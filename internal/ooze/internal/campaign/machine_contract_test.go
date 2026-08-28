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

type testEffectKind string

const (
	registerEffect                testEffectKind = "register"
	establishSnapshotEffect       testEffectKind = "establish snapshot"
	discoverCatalogueEffect       testEffectKind = "discover catalogue"
	releaseSnapshotEffect         testEffectKind = "release snapshot"
	materializeWorkspaceEffect    testEffectKind = "materialize workspace"
	requestAdmissionEffect        testEffectKind = "request admission"
	requestStartCommitmentEffect  testEffectKind = "request start"
	launchAttemptEffect           testEffectKind = "launch attempt"
	cancelAdmissionEffect         testEffectKind = "cancel admission"
	returnAdmissionEffect         testEffectKind = "return admission"
	stopAttemptEffect             testEffectKind = "stop attempt"
	releaseWorkspaceEffect        testEffectKind = "release workspace"
	bindConfirmationBarrierEffect testEffectKind = "bind confirmation barrier"
	proposeTerminalEffect         testEffectKind = "propose terminal"
)

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
	require.Equal(t, []testEffectKind{releaseWorkspaceEffect}, effectKinds(effects))
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
	if effectKind(effects[0]) == bindConfirmationBarrierEffect {
		bound := harness.applyRuntime(effects[0]).Barrier()
		effects = harness.advance(campaign.ConfirmationBarrierBound(effects[0], bound))
	} else {
		admitted := harness.applyRuntime(effects[0]).Admission()
		require.Len(harness.t, admitted.Deliveries(), 1)
		effects = harness.advance(campaign.AdmissionGranted(effects[0], admitted.Deliveries()[0]))
	}
	require.Equal(harness.t, []testEffectKind{requestStartCommitmentEffect}, effectKinds(effects))
	started := harness.applyRuntime(effects[0]).Start()
	effects = harness.advance(campaign.StartCommitted(effects[0], started))
	require.Equal(harness.t, []testEffectKind{launchAttemptEffect}, effectKinds(effects))
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

func effectKinds(effects []campaign.Effect) []testEffectKind {
	kinds := make([]testEffectKind, len(effects))
	for index, effect := range effects {
		kinds[index] = effectKind(effect)
	}
	return kinds
}

func effectKind(effect campaign.Effect) testEffectKind {
	if operation, ok := effect.RuntimeOperation(); ok {
		switch operation {
		case processruntime.RegisterCampaignOperation:
			return registerEffect
		case processruntime.RequestAdmissionOperation:
			return requestAdmissionEffect
		case processruntime.CancelAdmissionOperation:
			return cancelAdmissionEffect
		case processruntime.ReturnGrantOperation:
			return returnAdmissionEffect
		case processruntime.BindConfirmationBarrierOperation:
			return bindConfirmationBarrierEffect
		case processruntime.CommitStartOperation:
			return requestStartCommitmentEffect
		case processruntime.CommitTerminalOperation, processruntime.AuthorizeForcedAbortOperation:
			return proposeTerminalEffect
		}
	}
	if request, ok := effect.ArtifactRequest(); ok {
		if request.EstablishesSnapshot() {
			return establishSnapshotEffect
		}
		if _, ok := request.CatalogueSnapshot(); ok {
			return discoverCatalogueEffect
		}
		if _, _, ok := request.Workspace(); ok {
			return materializeWorkspaceEffect
		}
		if kind, _, ok := request.Settlement(); ok {
			if kind == campaign.SnapshotResource {
				return releaseSnapshotEffect
			}
			return releaseWorkspaceEffect
		}
	}
	if request, ok := effect.SupervisionRequest(); ok {
		if _, launches := request.Prospective(time.Time{}, time.Time{}); launches {
			return launchAttemptEffect
		}
		if _, stops := request.StopGeneration(); stops {
			return stopAttemptEffect
		}
	}
	panic("campaign test effect is invalid")
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
	require.Equal(t, []testEffectKind{releaseSnapshotEffect}, effectKinds(effects))
	effects = harness.advance(campaign.ResourceSettled(campaign.SnapshotResource, "snapshot-a"))
	require.Equal(t, []testEffectKind{proposeTerminalEffect}, effectKinds(effects))
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

		assert.Equal(t, []testEffectKind{releaseWorkspaceEffect}, effectKinds(effects))
		assert.Equal(t, 1, harness.machine.CommandCount())
		harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-baseline"))
		assert.Equal(t, []testEffectKind{releaseSnapshotEffect}, effectKinds(harness.effects))
	})

	t.Run("an infrastructure failure stops an owned peer without retry", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a", "mutant-b"}, 2)
		first := harness.launch(primaries[0], "workspace-a")
		second := harness.launch(primaries[1], "workspace-b")
		effects := harness.settle(first, supervision.Infrastructure{
			Cause: supervision.CensusFailed, Err: assert.AnError,
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)

		assert.Equal(t, []testEffectKind{stopAttemptEffect, releaseWorkspaceEffect}, effectKinds(effects))
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

		assert.Equal(t, []testEffectKind{stopAttemptEffect}, effectKinds(effects))
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

		assert.Equal(t, []testEffectKind{stopAttemptEffect}, effectKinds(effects))
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
	require.Equal(t, []testEffectKind{materializeWorkspaceEffect}, effectKinds(effects))
	return harness, harness.launch(effects[0], "workspace-confirmation")
}

func TestMachineConfirmsOnlyAttributableDeadlinePressure(t *testing.T) {
	t.Run("confirmation infrastructure aborts without scoring the mutant", func(t *testing.T) {
		harness, confirmation := newConfirmationMachineHarness(t)
		effects := harness.settle(confirmation, supervision.Infrastructure{
			Cause: supervision.CensusFailed, Err: assert.AnError,
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)

		assert.Equal(t, []testEffectKind{releaseWorkspaceEffect}, effectKinds(effects))
	})

	t.Run("confirmation fuse remains an attributable runaway", func(t *testing.T) {
		harness, confirmation := newConfirmationMachineHarness(t)
		effects := harness.settle(confirmation, supervision.Tripped{
			Trip:          supervision.FuseTrip{Live: 9},
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)
		assert.Equal(t, []testEffectKind{releaseWorkspaceEffect}, effectKinds(effects))
		harness.advance(campaign.ResourceSettled(campaign.WorkspaceResource, "workspace-confirmation"))
		require.Equal(t, []testEffectKind{releaseSnapshotEffect}, effectKinds(harness.effects))
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

func TestMachineCompletesRuntimeCleanupThroughPublicFacts(t *testing.T) {
	t.Run("cancels a waiting admission before releasing its workspace", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(
			t, []string{"mutant-a", "mutant-b"}, 2,
		)
		peer := harness.applyCut(processruntime.RegisterCampaignCut(99)).Registration()
		peerAdmission := harness.applyCut(processruntime.RequestAdmissionCut(processruntime.Admission{
			Campaign: peer.Campaign(), Attempt: "peer:1", Class: processruntime.SharedAdmission,
		})).Admission()
		require.Len(t, peerAdmission.Deliveries(), 1)
		peerStart := harness.applyCut(processruntime.CommitStartCut(peerAdmission.Deliveries()[0])).Start()
		harness.applyCut(processruntime.ObserveAttemptCut(peerStart.Generation(), processruntime.Owned()))
		first := harness.launch(primaries[0], "workspace-a")
		requestEffects := harness.advance(campaign.WorkspaceMaterialized(primaries[1], "workspace-b"))
		request := runtimeRequestForOperation(t, harness, requestEffects, processruntime.RequestAdmissionOperation)
		waiting := applyRuntimeRequest(t, harness, request)
		assert.Empty(t, waiting.Admission().Deliveries())

		cleanup := harness.settle(first, supervision.Infrastructure{
			Cause: supervision.CensusFailed, Err: assert.AnError,
			ExecutionData: supervision.ExecutionData{Deadline: 20 * time.Second},
		}, 0)
		cancel := runtimeRequestForOperation(t, harness, cleanup, processruntime.CancelAdmissionOperation)
		cancelled := applyRuntimeRequest(t, harness, cancel)
		facts := cancel.Complete(cancelled.RecordedCut())
		require.Len(t, facts, 1)
		effects := harness.advance(facts[0])

		require.Len(t, effects, 1)
		artifact, ok := effects[0].ArtifactRequest()
		require.True(t, ok)
		kind, identity, settles := artifact.Settlement()
		assert.True(t, settles)
		assert.Equal(t, campaign.WorkspaceResource, kind)
		assert.Equal(t, "workspace-b", identity)
	})

	t.Run("acknowledges a grant returned after fatal closure", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a"}, 1)
		effects := harness.advance(campaign.WorkspaceMaterialized(primaries[0], "workspace-a"))
		request := runtimeRequestForOperation(t, harness, effects, processruntime.RequestAdmissionOperation)
		admitted := applyRuntimeRequest(t, harness, request)
		facts := request.Complete(admitted.RecordedCut())
		require.Len(t, facts, 1)
		effects = harness.advance(facts[0])
		start := runtimeRequestForOperation(t, harness, effects, processruntime.CommitStartOperation)
		harness.applyCut(processruntime.CloseCut("fixture closure"))
		started := applyRuntimeRequest(t, harness, start)
		effects = harness.advance(start.Complete(started.RecordedCut())[0])
		returned := runtimeRequestForOperation(t, harness, effects, processruntime.ReturnGrantOperation)
		acknowledged := applyRuntimeRequest(t, harness, returned)
		facts = returned.Complete(acknowledged.RecordedCut())
		require.Len(t, facts, 1)

		assert.NotPanics(t, func() { harness.advance(facts[0]) })
	})

	t.Run("retains transferred custody after emergency settlement", func(t *testing.T) {
		harness, primaries := newRunningCampaignMachineHarness(t, []string{"mutant-a"}, 1)
		primary := harness.launch(primaries[0], "workspace-a")
		harness.settle(primary, supervision.DrainUnconfirmed{
			Residual: supervision.OwnedUndrained,
			ExecutionData: supervision.ExecutionData{
				Deadline: 20 * time.Second, CommandDuration: time.Second,
			},
		}, 0)
		settled := harness.applyCut(processruntime.SettleEmergencyCut([]processruntime.Resolution{
			processruntime.TransferCustody(primary.generation),
		})).Settlement()

		harness.advance(campaign.RuntimeEmergencySettled(settled))

		assert.True(t, harness.machine.Projection().Failed())
		assert.NotEmpty(t, harness.machine.Projection().Obligations())
	})
}

func runtimeRequestForOperation(
	t *testing.T,
	harness *campaignMachineHarness,
	effects []campaign.Effect,
	operation processruntime.Operation,
) campaign.RuntimeRequest {
	t.Helper()
	for _, effect := range effects {
		request, ok := harness.runtimeBinding.RuntimeRequest(effect, harness.definition)
		if ok && request.Cut().Operation() == operation {
			return request
		}
	}
	require.FailNowf(t, "runtime request not found", "operation=%d", operation)
	return campaign.RuntimeRequest{}
}

func applyRuntimeRequest(
	t *testing.T,
	harness *campaignMachineHarness,
	request campaign.RuntimeRequest,
) processruntime.ReplayResult {
	t.Helper()
	return harness.applyCut(request.Cut())
}
