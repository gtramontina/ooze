package ooze

import (
	"reflect"
	"testing"
	"time"
)

func TestCampaignEmptyCatalogueRunsNoCommandAndCommitsNoMutants(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a",
		lineage:  11,
		command:  []string{"go", "test", "./..."},
		profile:  AutomaticProfile,
		peers:    4,
	}
	state, effects := beginCampaign(definition)
	assertCampaignEffects(t, effects, campaignEffectRegister)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: campaignRegistration{
			decision: campaignRegistered,
			token:    campaignToken{id: 1, lineage: 11},
		}},
	})
	assertCampaignEffects(t, effects, campaignEffectEstablishSnapshot)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 2, payload: snapshotEstablishedEvent{snapshot: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectDiscoverCatalogue)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 3, payload: catalogueDiscoveredEvent{snapshot: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	if got := state.commandCount(); got != 0 {
		t.Fatalf("command count=%d, want 0", got)
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 4, payload: resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"},
	})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)

	state, effects = advanceCampaign(state, campaignEvent{
		id: 5, payload: terminalCommittedEvent{result: terminalResult{decision: terminalCommitted}},
	})
	if len(effects) != 0 || !reflect.DeepEqual(state.outcome, noMutantsOutcome{}) {
		t.Fatalf("terminal state/effects=%#v/%#v", state, effects)
	}
	if len(state.obligations) != 0 {
		t.Fatalf("normal terminal retained obligations: %#v", state.obligations)
	}
	if definition.command[0] != "go" || state.definition.command[0] != "go" {
		t.Fatalf("definition was not preserved: input=%#v state=%#v", definition, state.definition)
	}
}

func TestCampaignDefinitionOwnsPositiveTenMinuteBaselineBootstrap(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: SerialProfile, peers: 8,
	}
	state, _ := beginCampaign(definition)
	if state.definition.baselineDeadline != 10*time.Minute {
		t.Fatalf("baseline deadline=%s, want 10m", state.definition.baselineDeadline)
	}

	definition.baselineDeadline = time.Second
	defer func() {
		if recover() == nil {
			t.Fatal("caller-selected baseline deadline was accepted")
		}
	}()
	_, _ = beginCampaign(definition)
}

func TestCampaignNonEmptyCatalogueRunsOneSnapshotBoundBaselineBeforePrimaries(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"go", "test"},
		env: []string{"A=1"}, profile: AutomaticProfile, peers: 4,
	}
	runtime := newProcessRuntime(4)
	state, _ := beginCampaign(definition)
	runtime, registered := runtime.registerCampaign(campaignProvenance{lineage: definition.lineage})
	state, _ = advanceCampaign(state, campaignEvent{
		id: 1, payload: campaignRegisteredEvent{registration: registered},
	})
	state, _ = advanceCampaign(state, campaignEvent{
		id: 2, payload: snapshotEstablishedEvent{snapshot: "snapshot-a"},
	})
	mutants := []mutantIdentity{"mutant-a", "mutant-b"}
	state, effects := advanceCampaign(state, campaignEvent{
		id: 3, payload: catalogueDiscoveredEvent{snapshot: "snapshot-a", mutants: mutants},
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	baseline := effects[0]
	if baseline.snapshot != "snapshot-a" || baseline.attempt == "" {
		t.Fatalf("baseline materialization=%#v", baseline)
	}
	mutants[0] = "mutated-by-caller"
	if !reflect.DeepEqual(state.catalogue, []mutantIdentity{"mutant-a", "mutant-b"}) {
		t.Fatalf("catalogue aliases caller input: %#v", state.catalogue)
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 4, payload: workspaceMaterializedEvent{
			attempt: baseline.attempt, workspace: "workspace-baseline", snapshot: baseline.snapshot,
		},
	})
	assertCampaignEffects(t, effects, campaignEffectRequestAdmission)
	if effects[0].request.class != exclusiveAdmission {
		t.Fatalf("baseline admission=%#v, want exclusive", effects[0].request)
	}
	runtime, admitted := runtime.requestAdmission(effects[0].request)
	state, effects = advanceCampaign(state, campaignEvent{
		id: 5, payload: admissionGrantedEvent{attempt: baseline.attempt, grant: admitted.deliveries[0]},
	})
	assertCampaignEffects(t, effects, campaignEffectRequestStartCommitment)
	runtime, started := runtime.startCommitted(effects[0].grant)
	state, effects = advanceCampaign(state, campaignEvent{
		id: 6, payload: startCommittedEvent{attempt: baseline.attempt, grant: effects[0].grant, result: started},
	})
	assertCampaignEffects(t, effects, campaignEffectLaunchAttempt)
	if effects[0].deadline != 10*time.Minute || effects[0].profile != AutomaticProfile ||
		effects[0].snapshot != "snapshot-a" || effects[0].workspace != "workspace-baseline" {
		t.Fatalf("baseline launch=%#v", effects[0])
	}
	runtime, launchReceipt := runtime.observeAttempt(started.generation, launchOwned{})
	state, effects = advanceCampaign(state, campaignEvent{
		id: 7, payload: attemptLaunchEvent{
			attempt: baseline.attempt, generation: started.generation, result: Owned{}, receipt: launchReceipt,
		},
	})
	if len(effects) != 0 {
		t.Fatalf("owned baseline emitted effects=%#v", effects)
	}

	terminal := Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second},
	}
	runtime, terminalReceipt := runtime.observeAttempt(started.generation, attemptSettled{})
	resolved := resolveMutationDeadline(terminal.CommandDuration, definition.peers)
	state, effects = advanceCampaign(state, campaignEvent{
		id: 8, payload: attemptTerminalEvent{
			attempt: baseline.attempt, generation: started.generation, terminal: terminal,
			receipt: terminalReceipt, resolvedMutationDeadline: resolved,
		},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	if resolved != 20*time.Second || state.mutationDeadline != resolved || state.commandCount() != 1 {
		t.Fatalf("baseline resolution/count=%s/%s/%d", resolved, state.mutationDeadline, state.commandCount())
	}

	state, effects = advanceCampaign(state, campaignEvent{
		id: 9, payload: resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"},
	})
	if state.phase != campaignRunning {
		t.Fatalf("phase=%v, want running", state.phase)
	}
	if len(effects) != 2 {
		t.Fatalf("primary demand effects=%#v, want one per mutant up to peers", effects)
	}
	for _, effect := range effects {
		if effect.kind != campaignEffectMaterializeWorkspace || effect.snapshot != "snapshot-a" {
			t.Fatalf("primary materialization escaped snapshot: %#v", effect)
		}
	}
}

func TestCampaignCompletedRunsExactlyOneBaselineAndOnePrimaryPerMutant(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	if len(primaryEffects) != 2 {
		t.Fatalf("primary effects=%#v", primaryEffects)
	}

	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	firstEffects := harness.settleAttempt(t, first, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	secondEffects := harness.settleAttempt(t, second, Settled{
		Exit:          ExitStatus{Code: 1},
		ExecutionData: ExecutionData{Deadline: harness.state.mutationDeadline, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, firstEffects, campaignEffectReleaseWorkspace)
	assertCampaignEffects(t, secondEffects, campaignEffectReleaseWorkspace)

	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	if len(effects) != 0 {
		t.Fatalf("first primary cleanup effects=%#v", effects)
	}
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	if harness.state.phase != campaignDraining || harness.state.drain.kind != campaignDrainComplete {
		t.Fatalf("completion drain=%#v", harness.state.drain)
	}
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	harness.runtime, _ = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: terminalResult{decision: terminalCommitted}})

	completed, ok := harness.state.outcome.(completedOutcome)
	want := []mutantResult{{mutant: "mutant-a", kind: mutantSurvived}, {mutant: "mutant-b", kind: mutantKilled}}
	if !ok || !reflect.DeepEqual(completed.mutants, want) {
		t.Fatalf("completed outcome=%#v, want %#v", harness.state.outcome, want)
	}
	if harness.state.commandCount() != 3 || len(harness.state.obligations) != 0 {
		t.Fatalf("commands/obligations=%d/%#v", harness.state.commandCount(), harness.state.obligations)
	}
}

func TestCampaignFailedBaselineAbortsUnscoredAfterSettlement(t *testing.T) {
	harness := newCampaignHarness(t, []mutantIdentity{"mutant-a"}, AutomaticProfile, 2)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit:          ExitStatus{Code: 1},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: time.Second},
	}, 0)
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-baseline"})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	effects = harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	assertCampaignEffects(t, effects, campaignEffectProposeTerminal)
	var committed terminalResult
	harness.runtime, committed = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: committed})
	if _, ok := harness.state.outcome.(abortedOutcome); !ok || harness.state.commandCount() != 1 {
		t.Fatalf("baseline failure outcome/count=%#v/%d", harness.state.outcome, harness.state.commandCount())
	}
}

func TestCampaignOverlapProvisionalsDrainInCatalogueOrderAndConfirmOnce(t *testing.T) {
	harness, primaryEffects := newRunningCampaignHarness(t, []mutantIdentity{"mutant-a", "mutant-b"}, 2)
	first := harness.launchMaterialized(t, primaryEffects[0], "workspace-a")
	second := harness.launchMaterialized(t, primaryEffects[1], "workspace-b")
	deadline := harness.state.mutationDeadline
	trip := func() Tripped {
		return Tripped{
			Trip:          AutomaticDeadlineTrip{},
			ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
		}
	}
	firstEffects := harness.settleAttempt(t, first, trip(), 0)
	secondEffects := harness.settleAttempt(t, second, trip(), 0)
	assertCampaignEffects(t, firstEffects, campaignEffectReleaseWorkspace)
	assertCampaignEffects(t, secondEffects, campaignEffectReleaseWorkspace)
	if harness.state.phase != campaignDraining || harness.state.drain.kind != campaignDrainConfirm ||
		!reflect.DeepEqual(harness.state.drain.provisionals, []mutantIdentity{"mutant-a", "mutant-b"}) {
		t.Fatalf("confirmation drain=%#v", harness.state.drain)
	}

	harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-a"})
	effects := harness.advance(resourceSettledEvent{kind: campaignResourceWorkspace, identity: "workspace-b"})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	if effects[0].mutant != "mutant-a" || harness.state.phase != campaignConfirming {
		t.Fatalf("first confirmation=%#v phase=%v", effects[0], harness.state.phase)
	}
	confirmationA := harness.launchConfirmation(t, effects[0], "workspace-confirm-a")
	effects = harness.settleConfirmation(t, confirmationA, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: time.Second},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-confirm-a",
	})
	assertCampaignEffects(t, effects, campaignEffectMaterializeWorkspace)
	if effects[0].mutant != "mutant-b" {
		t.Fatalf("second confirmation=%#v", effects[0])
	}
	confirmationB := harness.launchConfirmation(t, effects[0], "workspace-confirm-b")
	effects = harness.settleConfirmation(t, confirmationB, Tripped{
		Trip:          AutomaticDeadlineTrip{},
		ExecutionData: ExecutionData{Deadline: deadline, CommandDuration: deadline, BoundFired: CommandDeadlineFired},
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)
	effects = harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-confirm-b",
	})
	assertCampaignEffects(t, effects, campaignEffectReleaseSnapshot)
	if harness.state.commandCount() != 5 || harness.runtime.mode != singleAdmission {
		t.Fatalf("command count/mode=%d/%v", harness.state.commandCount(), harness.runtime.mode)
	}

	harness.advance(resourceSettledEvent{kind: campaignResourceSnapshot, identity: "snapshot-a"})
	var committed terminalResult
	harness.runtime, committed = harness.runtime.commitTerminal(harness.state.runtimeToken)
	harness.advance(terminalCommittedEvent{result: committed})
	completed := harness.state.outcome.(completedOutcome)
	want := []mutantResult{{mutant: "mutant-a", kind: mutantSurvived}, {mutant: "mutant-b", kind: mutantTimedOut}}
	if !reflect.DeepEqual(completed.mutants, want) {
		t.Fatalf("confirmation outcome=%#v, want %#v", completed.mutants, want)
	}
}

type campaignHarness struct {
	state     campaignState
	runtime   processRuntime
	nextEvent campaignEventID
	effects   []campaignEffect
}

type launchedCampaignAttempt struct {
	attempt    attemptIdentity
	generation attemptGeneration
}

func newCampaignHarness(
	t *testing.T,
	mutants []mutantIdentity,
	profile Profile,
	peers int,
) *campaignHarness {
	t.Helper()
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"go", "test"}, profile: profile, peers: peers,
	}
	state, _ := beginCampaign(definition)
	runtime := newProcessRuntime(peers)
	runtime, registered := runtime.registerCampaign(campaignProvenance{lineage: definition.lineage})
	harness := &campaignHarness{state: state, runtime: runtime}
	harness.advance(campaignRegisteredEvent{registration: registered})
	harness.advance(snapshotEstablishedEvent{snapshot: "snapshot-a"})
	harness.effects = harness.advance(catalogueDiscoveredEvent{snapshot: "snapshot-a", mutants: mutants})

	return harness
}

func newRunningCampaignHarness(
	t *testing.T,
	mutants []mutantIdentity,
	peers int,
) (*campaignHarness, []campaignEffect) {
	t.Helper()
	harness := newCampaignHarness(t, mutants, AutomaticProfile, peers)
	baseline := harness.launchMaterialized(t, harness.effects[0], "workspace-baseline")
	effects := harness.settleAttempt(t, baseline, Settled{
		Exit:          ExitStatus{},
		ExecutionData: ExecutionData{Deadline: 10 * time.Minute, CommandDuration: 2 * time.Second},
	}, resolveMutationDeadline(2*time.Second, peers))
	assertCampaignEffects(t, effects, campaignEffectReleaseWorkspace)

	return harness, harness.advance(resourceSettledEvent{
		kind: campaignResourceWorkspace, identity: "workspace-baseline",
	})
}

func (harness *campaignHarness) advance(payload campaignEventPayload) []campaignEffect {
	harness.nextEvent++
	var effects []campaignEffect
	harness.state, effects = advanceCampaign(harness.state, campaignEvent{id: harness.nextEvent, payload: payload})

	return effects
}

func (harness *campaignHarness) launchMaterialized(
	t *testing.T,
	effect campaignEffect,
	workspace string,
) launchedCampaignAttempt {
	t.Helper()
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: workspace, snapshot: effect.snapshot,
	})
	var admitted admissionResult
	harness.runtime, admitted = harness.runtime.requestAdmission(effects[0].request)
	effects = harness.advance(admissionGrantedEvent{attempt: effect.attempt, grant: admitted.deliveries[0]})
	var started startCommittedResult
	harness.runtime, started = harness.runtime.startCommitted(effects[0].grant)
	effects = harness.advance(startCommittedEvent{attempt: effect.attempt, grant: effects[0].grant, result: started})
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(started.generation, launchOwned{})
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation, result: Owned{}, receipt: receipt,
	})

	return launchedCampaignAttempt{attempt: effect.attempt, generation: started.generation}
}

func (harness *campaignHarness) settleAttempt(
	t *testing.T,
	attempt launchedCampaignAttempt,
	terminal Terminal,
	resolved time.Duration,
) []campaignEffect {
	t.Helper()
	var observation attemptObservation
	switch observed := terminal.(type) {
	case Settled:
		observation = attemptSettled{}
	case Tripped:
		switch observed.Trip.(type) {
		case FuseTrip:
			observation = attemptTripped{kind: fuseTrip}
		default:
			observation = attemptTripped{kind: deadlineTrip}
		}
	case Stopped:
		observation = attemptStopped{}
	case Infrastructure:
		observation = attemptInfrastructure{cause: "campaign fixture"}
	case DrainUnconfirmed:
		observation = drainUnconfirmed{}
	default:
		t.Fatalf("unsupported fixture terminal %#v", terminal)
	}
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(attempt.generation, observation)

	return harness.advance(attemptTerminalEvent{
		attempt: attempt.attempt, generation: attempt.generation, terminal: terminal,
		receipt: receipt, resolvedMutationDeadline: resolved,
	})
}

func (harness *campaignHarness) launchConfirmation(
	t *testing.T,
	effect campaignEffect,
	workspace string,
) launchedCampaignAttempt {
	t.Helper()
	effects := harness.advance(workspaceMaterializedEvent{
		attempt: effect.attempt, workspace: workspace, snapshot: effect.snapshot,
	})
	if effects[0].kind == campaignEffectBindConfirmationBarrier {
		var bound barrierResult
		harness.runtime, bound = harness.runtime.sealAndBindConfirmationBarrier(effects[0].binding)
		effects = harness.advance(confirmationBarrierBoundEvent{attempt: effect.attempt, result: bound})
	} else {
		var admitted admissionResult
		harness.runtime, admitted = harness.runtime.requestAdmission(effects[0].request)
		effects = harness.advance(admissionGrantedEvent{attempt: effect.attempt, grant: admitted.deliveries[0]})
	}
	var started startCommittedResult
	harness.runtime, started = harness.runtime.startCommitted(effects[0].grant)
	effects = harness.advance(startCommittedEvent{attempt: effect.attempt, grant: effects[0].grant, result: started})
	var receipt observationResult
	harness.runtime, receipt = harness.runtime.observeAttempt(started.generation, launchOwned{})
	harness.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: started.generation, result: Owned{}, receipt: receipt,
	})

	return launchedCampaignAttempt{attempt: effect.attempt, generation: started.generation}
}

func (harness *campaignHarness) settleConfirmation(
	t *testing.T,
	attempt launchedCampaignAttempt,
	terminal Terminal,
) []campaignEffect {
	t.Helper()
	queueDrained := len(harness.state.drain.provisionals) == 1
	outcome := confirmationPressureAccepted
	if _, repeated := terminal.(Tripped); repeated {
		outcome = confirmationRejected
	}
	var receipt observationResult
	if queueDrained {
		harness.runtime, receipt = harness.runtime.observeAttempt(attempt.generation, confirmationQueueDrained{
			outcome: outcome,
		})
	} else {
		harness.runtime, receipt = harness.runtime.observeAttempt(attempt.generation, confirmationContinues{
			outcome: outcome,
		})
	}

	return harness.advance(attemptTerminalEvent{
		attempt: attempt.attempt, generation: attempt.generation, terminal: terminal, receipt: receipt,
	})
}

func assertCampaignEffects(t *testing.T, effects []campaignEffect, kinds ...campaignEffectKind) {
	t.Helper()
	got := make([]campaignEffectKind, len(effects))
	for index := range effects {
		got[index] = effects[index].kind
	}
	if !reflect.DeepEqual(got, kinds) {
		t.Fatalf("effect kinds=%v, want %v; effects=%#v", got, kinds, effects)
	}
}
