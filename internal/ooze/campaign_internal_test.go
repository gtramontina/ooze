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
