package ooze

import (
	"reflect"
	"testing"
)

func TestSimulationExploresAndReplaysEmptyCatalogueThroughProductionOwners(t *testing.T) {
	definition := simulationDefinition{
		campaign: campaignDefinition{
			identity: "campaign-a",
			lineage:  11,
			command:  []string{"go", "test", "./..."},
			profile:  AutomaticProfile,
			peers:    2,
		},
		capacity: 2,
	}

	explored := Explore(definition, simulationChoiceBytes{0, 1, 2})
	if explored.failure != nil {
		t.Fatalf("exploration failure=%v", explored.failure)
	}
	if got, want := explored.world.campaign.outcome, (campaignOutcome)(noMutantsOutcome{}); !reflect.DeepEqual(got, want) {
		t.Fatalf("explored outcome=%#v, want %#v", got, want)
	}
	if got, want := simulationAuthorities(explored.trace), []simulationAuthority{
		simulationRuntimeAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationCampaignAuthority,
		simulationRuntimeAuthority,
		simulationCampaignAuthority,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trace authorities=%v, want %v", got, want)
	}
	for index, record := range explored.trace.records {
		if got, want := record.sequence, uint64(index+1); got != want {
			t.Fatalf("record %d sequence=%d, want %d", index, got, want)
		}
	}

	replayed := ReplayLegal(explored.trace)
	if replayed.failure != nil {
		t.Fatalf("replay failure=%v", replayed.failure)
	}
	if !reflect.DeepEqual(replayed.world, explored.world) {
		t.Fatalf("replayed world diverged:\n got: %#v\nwant: %#v", replayed.world, explored.world)
	}
}

func simulationAuthorities(trace simulationTrace) []simulationAuthority {
	authorities := make([]simulationAuthority, 0, len(trace.records))
	for _, record := range trace.records {
		authorities = append(authorities, record.authority)
	}

	return authorities
}
