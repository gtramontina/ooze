package simulationcontract_test

import (
	"reflect"
	"testing"

	contract "deterministic-simulation-contract"
)

func TestChoiceBytesExpandToReplayableLegalTrace(t *testing.T) {
	t.Parallel()

	definition := contract.Definition{Mutants: []string{"first", "second"}}
	trace, explored := contract.Explore(definition, []byte{2, 1, 0, 2})
	replayed, err := contract.Replay(definition, trace)

	if err != nil {
		t.Fatalf("replay rejected generated trace: %v", err)
	}
	if !reflect.DeepEqual(replayed, explored) {
		t.Fatalf("replay = %#v, explored = %#v", replayed, explored)
	}
	if explored.Terminal != contract.Completed {
		t.Fatalf("terminal = %v, want Completed", explored.Terminal)
	}
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
	if err != nil {
		t.Fatalf("shrink failed: %v", err)
	}
	want := contract.Counterexample{
		Definition: contract.Definition{Mutants: []string{"failing"}},
		Trace: contract.Trace{Events: []contract.Event{
			{Sequence: 1, Kind: contract.BaselinePassed, Mutant: "", Outcome: 0},
			{Sequence: 2, Kind: contract.PrimarySettled, Mutant: "failing", Outcome: contract.Killed},
		}},
	}
	if !reflect.DeepEqual(shrunk, want) {
		t.Fatalf("shrunk = %#v, want %#v", shrunk, want)
	}
	if _, err := contract.Replay(shrunk.Definition, shrunk.Trace); err != nil {
		t.Fatalf("shrunk trace is not legal: %v", err)
	}
}

func TestEmptyCatalogueExplorationRunsNoCommand(t *testing.T) {
	t.Parallel()

	trace, result := contract.Explore(contract.Definition{Mutants: nil}, []byte{1, 2, 3})

	if len(trace.Events) != 0 {
		t.Fatalf("empty catalogue trace = %#v, want no events", trace)
	}
	if result.Terminal != contract.NoMutants {
		t.Fatalf("terminal = %v, want NoMutants", result.Terminal)
	}
}
