package simulationcontract_test

import (
	"testing"

	contract "deterministic-simulation-contract"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChoiceBytesExpandToReplayableLegalTrace(t *testing.T) {
	t.Parallel()

	definition := contract.Definition{Mutants: []string{"first", "second"}}
	trace, explored := contract.Explore(definition, []byte{2, 1, 0, 2})
	replayed, err := contract.Replay(definition, trace)

	require.NoError(t, err, "replay rejected generated trace: %v", err)
	assert.Equal(t, explored, replayed, "replay = %#v, explored = %#v", replayed, explored)
	assert.Equal(t, contract.Completed, explored.Terminal, "terminal = %v, want Completed", explored.Terminal)
}

func TestSemanticShrinkRemovesUnrelatedWholeEventsAndPreservesFailure(t *testing.T) {
	t.Parallel()

	counterexample := contract.Counterexample{
		Definition: contract.Definition{Mutants: []string{"setup", "failing", "noise"}},
		Trace: contract.Trace{Events: []contract.Event{
			{Sequence: 1, Kind: contract.BaselinePassed, Mutant: "", Outcome: 0},
			{Sequence: 2, Kind: contract.PrimarySettled, Mutant: "setup", Outcome: contract.Survived},
			{Sequence: 3, Kind: contract.PrimarySettled, Mutant: "failing", Outcome: contract.Killed},
			{Sequence: 4, Kind: contract.PrimarySettled, Mutant: "noise", Outcome: contract.Survived},
		}},
	}
	fingerprint := contract.FailureFingerprint{Mutant: "failing", Outcome: contract.Killed}

	shrunk, err := contract.Shrink(counterexample, fingerprint)
	require.NoError(t, err, "shrink failed: %v", err)
	want := contract.Counterexample{
		Definition: contract.Definition{Mutants: []string{"failing"}},
		Trace: contract.Trace{Events: []contract.Event{
			{Sequence: 1, Kind: contract.BaselinePassed, Mutant: "", Outcome: 0},
			{Sequence: 2, Kind: contract.PrimarySettled, Mutant: "failing", Outcome: contract.Killed},
		}},
	}
	assert.Equal(t, want, shrunk, "shrunk = %#v, want %#v", shrunk, want)
	{
		_, err := contract.Replay(shrunk.Definition, shrunk.Trace)
		assert.NoError(t, err, "shrunk trace is not legal: %v", err)
	}
}

func TestEmptyCatalogueExplorationRunsNoCommand(t *testing.T) {
	t.Parallel()

	trace, result := contract.Explore(contract.Definition{Mutants: nil}, []byte{1, 2, 3})

	assert.EqualValues(t, 0, len(trace.Events), "empty catalogue trace = %#v, want no events", trace)
	assert.Equal(t, contract.NoMutants, result.Terminal, "terminal = %v, want NoMutants", result.Terminal)
}
