package campaign_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCampaignEffectsDeclareTheirOwningBoundary(t *testing.T) {
	t.Run("registration belongs to the process runtime", func(t *testing.T) {
		_, transition := campaign.NewMachine(campaign.Definition{
			Identity: "campaign-a", Lineage: 11, Command: []string{"test"},
			Profile: processruntime.AutomaticProfile, Peers: 1,
		})
		effects := transition.Effects()
		require.Len(t, effects, 1)

		assert.Equal(t, campaign.RuntimeOwner, effects[0].Owner())
	})

	t.Run("snapshot work belongs to the campaign artifact boundary", func(t *testing.T) {
		harness := newCampaignMachineHarness(t, nil, processruntime.AutomaticProfile, 1)
		require.Len(t, harness.effects, 1)

		assert.Equal(t, campaign.ArtifactOwner, harness.effects[0].Owner())
	})
}

func TestCampaignTranslatesRuntimeResultsWithoutExposingReducerVocabulary(t *testing.T) {
	definition := campaign.Definition{
		Identity: "campaign-a", Lineage: 11, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
	}
	machine, transition := campaign.NewMachine(definition)
	effects := transition.Effects()
	require.Len(t, effects, 1)
	runtime := processruntime.NewReplay(1)
	request, ok := (campaign.RuntimeBinding{}).RuntimeRequest(effects[0], definition)
	require.True(t, ok)
	nextRuntime, result := runtime.Apply(request.Cut())

	facts := request.Complete(result.RecordedCut())
	require.Len(t, facts, 1)
	nextMachine, nextTransition := machine.Apply(facts[0])

	assert.Equal(t, campaign.Registered(result.Registration()), facts[0])
	assert.False(t, nextMachine.Projection().Settled())
	require.Len(t, nextTransition.Effects(), 1)
	assert.Equal(t, campaign.ArtifactOwner, nextTransition.Effects()[0].Owner())
	assert.EqualValues(t, 1, nextRuntime.Projection().CampaignCount())
}
