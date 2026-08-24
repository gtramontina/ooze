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
