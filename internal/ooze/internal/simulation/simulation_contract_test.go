package simulation_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/simulation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministicSimulationFourOperationContract(t *testing.T) {
	definition := simulation.NewDefinition(campaign.Definition{
		Identity: "campaign-a", Lineage: 1, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: 1,
	}, 1, nil)

	t.Run("explore and replay legal facts", func(t *testing.T) {
		explored := simulation.Explore(definition, nil)
		require.NoError(t, explored.Failure())

		replayed := simulation.ReplayLegal(explored.Trace())
		assert.NoError(t, replayed.Failure())
		assert.True(t, explored.SameWorld(replayed))
	})

	t.Run("replay a malformed fact", func(t *testing.T) {
		explored := simulation.Explore(definition, nil)
		require.NoError(t, explored.Failure())

		violation := simulation.ReplayViolation(
			explored.Trace().Prefix(0),
			simulation.MalformedRuntime(processruntime.RequestAdmissionCut(processruntime.Admission{})),
		)
		assert.NoError(t, violation.Failure())
		assert.NotZero(t, violation.FailureKey())
	})

	t.Run("shrink a trace by semantic failure identity", func(t *testing.T) {
		explored := simulation.Explore(definition, nil)
		require.NoError(t, explored.Failure())
		violation := simulation.ReplayViolation(
			explored.Trace().Prefix(0),
			simulation.MalformedRuntime(processruntime.RequestAdmissionCut(processruntime.Admission{})),
		)

		shrunk := simulation.Shrink(violation.Trace(), violation.FailureKey())
		replayed := simulation.ReplayViolation(shrunk.LegalPrefix(), shrunk.Malformed())
		assert.NoError(t, replayed.Failure())
		assert.Equal(t, violation.FailureKey(), replayed.FailureKey())
	})
}
