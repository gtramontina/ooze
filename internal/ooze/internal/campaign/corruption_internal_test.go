package campaign

import (
	"strings"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
)

type panickingCampaignFact struct{}

func (panickingCampaignFact) campaignEventPayload() {}

func (panickingCampaignFact) campaignEventName() string { panic("reducer defect") }

func TestCampaignRejectsInjectedBaselineDeadline(t *testing.T) {
	definition := campaignDefinition{
		identity: "campaign-a", lineage: 11, command: []string{"test"}, profile: SerialProfile, peers: 8,
		baselineDeadline: time.Second,
	}
	assert.Panics(t, func() { _, _ = beginCampaign(definition) })
}

func TestCampaignInvariantProjectionOmitsPrivateCustodyAndFilesystemFacts(t *testing.T) {
	_, registration := processruntime.NewReplay(1).Apply(processruntime.RegisterCampaignCut(8888))
	state := campaignState{
		definition:   campaignDefinition{identity: "campaign-a"},
		snapshot:     "/private/snapshot",
		runtimeToken: registration.Registration().Campaign(),
		drain:        campaignDrainIntent{epoch: 9999},
		attempts: []campaignAttempt{{
			identity: "campaign-a:2", generation: 7777, workspace: "/private/workspace",
		}},
		obligations: []campaignObligation{{
			kind: campaignResourceWorkspace, identity: "/private/workspace",
			attempt: "campaign-a:2", generation: 7777,
		}},
	}
	event := campaignEvent{id: 4, payload: resourceSettlementFailedEvent{
		kind: campaignResourceWorkspace, identity: "/private/workspace", cause: "private cause",
	}}
	projected := strings.Join(append(
		[]string{campaignEventSummary(event)},
		append(state.stableIdentitySnapshot(event), state.obligationSnapshot()...)...,
	), "\n")

	for _, public := range []string{
		"event=4 kind=resource settlement failed resource=workspace",
		"campaign=campaign-a", "attempt=campaign-a:2", "workspace/attempt=campaign-a:2",
	} {
		assert.Contains(t, projected, public)
	}
	for _, private := range []string{
		"/private", "7777", "8888", "9999", "private cause", "generation", "token",
	} {
		assert.NotContains(t, projected, private)
	}
}

func TestMachineAcceptsPropagatesUnexpectedReducerPanics(t *testing.T) {
	machine, _ := NewMachine(Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"test"}, Profile: SerialProfile, Peers: 1,
	})

	assert.PanicsWithValue(t, "reducer defect", func() {
		machine.Accepts(Fact{payload: panickingCampaignFact{}})
	})
}

func TestCanonicalProjectionOmitsRuntimeAuthority(t *testing.T) {
	first := Projection{state: campaignState{runtimeToken: campaignToken{}}}
	_, registered := processruntime.NewReplay(1).Apply(processruntime.RegisterCampaignCut(11))
	second := Projection{state: campaignState{runtimeToken: registered.Registration().Campaign()}}

	assert.True(t, first.Canonical().Equal(second.Canonical()))
}
