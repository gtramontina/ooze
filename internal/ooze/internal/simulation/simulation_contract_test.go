package simulation_test

import (
	"fmt"
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/simulation"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSimulationFourOperationContract(t *testing.T) {
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
			explored.Trace(),
			simulation.MalformedRuntime(processruntime.RequestAdmissionCut(processruntime.Admission{})),
		)
		assert.NoError(t, violation.Failure())
		assert.NotZero(t, violation.FailureKey())
	})

	t.Run("shrink a trace by semantic failure identity", func(t *testing.T) {
		explored := simulation.Explore(definition, nil)
		require.NoError(t, explored.Failure())
		violation := simulation.ReplayViolation(
			explored.Trace(),
			simulation.MalformedRuntime(processruntime.RequestAdmissionCut(processruntime.Admission{})),
		)

		shrunk := simulation.Shrink(violation.Trace(), violation.FailureKey())
		replayed := simulation.ReplayViolation(shrunk.LegalPrefix(), shrunk.Malformed())
		assert.NoError(t, replayed.Failure())
		assert.Equal(t, violation.FailureKey(), replayed.FailureKey())
	})
}

func TestSimulationRetainsLateTerminalNeededForCampaignCleanup(t *testing.T) {
	definition, choices := simulationInput([]byte("22000000110000010AX12"))

	result := simulation.Explore(definition, choices)

	assert.NoError(t, result.Failure())
}

func TestSimulationQueuesOnlyOnePendingEmergencyEpoch(t *testing.T) {
	definition, choices := simulationInput([]byte("X12002"))

	result := simulation.Explore(definition, choices)

	assert.NoError(t, result.Failure())
}

func FuzzSimulationLegalReplayAndViolationRemainDeterministic(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 7, 9})
	f.Add([]byte{2})
	f.Fuzz(func(t *testing.T, source []byte) {
		definition, choices := simulationInput(source)

		explored := simulation.Explore(definition, choices)
		require.NoError(t, explored.Failure())
		replayed := simulation.ReplayLegal(explored.Trace())
		require.NoError(t, replayed.Failure())
		assert.True(t, explored.SameWorld(replayed))

		malformed := simulation.MalformedCampaign(campaign.SnapshotEstablished(""))
		if len(source) != 0 {
			switch source[0] % 3 {
			case 1:
				malformed = simulation.MalformedRuntime(
					processruntime.RequestAdmissionCut(processruntime.Admission{}),
				)
			case 2:
				malformed = simulation.MalformedSupervision(
					supervision.CorrelatedMalformedFact(supervision.ProspectiveRegisteredFact, 0),
				)
			}
		}
		first := simulation.ReplayViolation(explored.Trace(), malformed)
		second := simulation.ReplayViolation(explored.Trace(), malformed)
		require.NoError(t, first.Failure())
		require.NoError(t, second.Failure())
		assert.Equal(t, first.FailureKey(), second.FailureKey())
		assert.True(t, first.SameWorld(second))
	})
}

func simulationInput(source []byte) (simulation.Definition, simulation.Choices) {
	capacity := 1
	mutants := 0
	if len(source) != 0 {
		mutants = 1 + int(source[0]%3)
	}
	if len(source) > 1 {
		capacity = 1 + int(source[1]%3)
	}
	catalogue := make([]string, mutants)
	for index := range catalogue {
		catalogue[index] = fmt.Sprintf("mutant-%d", index+1)
	}
	definition := simulation.NewDefinition(campaign.Definition{
		Identity: "campaign-fuzz", Lineage: 61, Command: []string{"test"},
		Profile: processruntime.AutomaticProfile, Peers: capacity,
	}, capacity, catalogue)
	var choices simulation.Choices
	if len(source) > 2 {
		choices = source[2:]
	}

	return definition, choices
}
