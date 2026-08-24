// Package simulationcontract prototypes the representation split chosen by issue #64.
package simulationcontract

import (
	"errors"
	"fmt"
)

// Definition identifies the catalogue in stable order.
type Definition struct {
	Mutants []string
}

// EventKind identifies one toy normalized fact family.
type EventKind uint8

// Toy normalized fact families.
const (
	BaselinePassed EventKind = iota + 1
	PrimarySettled
)

// Outcome is the observed result of a toy primary.
type Outcome uint8

// Toy primary outcomes.
const (
	Survived Outcome = iota + 1
	Killed
)

// Event is one normalized toy fact with a stable sequence.
type Event struct {
	Sequence uint64
	Kind     EventKind
	Mutant   string
	Outcome  Outcome
}

// Trace is the canonical toy replay input.
type Trace struct {
	Events []Event
}

// Terminal identifies the completed toy campaign result.
type Terminal uint8

// Toy terminal results.
const (
	Completed Terminal = iota + 1
	NoMutants
)

// MutantResult records one toy mutant's result.
type MutantResult struct {
	Mutant  string
	Outcome Outcome
}

// Result is the replayed toy campaign result.
type Result struct {
	Terminal Terminal
	Mutants  []MutantResult
}

// Counterexample combines the immutable definition and its replay trace.
type Counterexample struct {
	Definition Definition
	Trace      Trace
}

// FailureFingerprint is the toy semantic identity retained by shrinking.
type FailureFingerprint struct {
	Mutant  string
	Outcome Outcome
}

var (
	errBaselineMissing     = errors.New("trace does not begin with the baseline")
	errEventNotEnabled     = errors.New("event is not enabled")
	errSettlementMissing   = errors.New("trace stopped before every mutant settled")
	errFingerprintMismatch = errors.New("trace does not reproduce the failure fingerprint")
)

// Explore expands arbitrary choices only through currently enabled toy work.
func Explore(definition Definition, choices []byte) (Trace, Result) {
	if len(definition.Mutants) == 0 {
		return Trace{Events: nil}, Result{Terminal: NoMutants, Mutants: nil}
	}
	remaining := append([]string(nil), definition.Mutants...)
	events := []Event{{Sequence: 1, Kind: BaselinePassed, Mutant: "", Outcome: 0}}
	for _, choice := range choices {
		if len(remaining) == 0 {
			break
		}
		index := int(choice) % len(remaining)
		outcome := Survived
		if choice&1 == 1 {
			outcome = Killed
		}
		events = append(events, Event{
			Sequence: uint64(len(events) + 1),
			Kind:     PrimarySettled,
			Mutant:   remaining[index],
			Outcome:  outcome,
		})
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	for _, mutant := range remaining {
		events = append(events, Event{
			Sequence: uint64(len(events) + 1),
			Kind:     PrimarySettled,
			Mutant:   mutant,
			Outcome:  Survived,
		})
	}
	trace := Trace{Events: events}
	result, err := Replay(definition, trace)
	if err != nil {
		panic(err)
	}

	return trace, result
}

// Replay accepts only a complete legal toy trace.
func Replay(definition Definition, trace Trace) (Result, error) {
	if len(definition.Mutants) == 0 && len(trace.Events) == 0 {
		return Result{Terminal: NoMutants, Mutants: nil}, nil
	}
	baseline := Event{Sequence: 1, Kind: BaselinePassed, Mutant: "", Outcome: 0}
	if len(trace.Events) == 0 || trace.Events[0] != baseline {
		return Result{}, errBaselineMissing
	}
	known := make(map[string]bool, len(definition.Mutants))
	for _, mutant := range definition.Mutants {
		known[mutant] = true
	}
	results := make([]MutantResult, 0, len(definition.Mutants))
	for index, event := range trace.Events[1:] {
		if event.Sequence != uint64(index+2) || event.Kind != PrimarySettled ||
			!known[event.Mutant] || (event.Outcome != Survived && event.Outcome != Killed) {
			return Result{}, fmt.Errorf("%w: sequence %d", errEventNotEnabled, index+2)
		}
		known[event.Mutant] = false
		results = append(results, MutantResult{Mutant: event.Mutant, Outcome: event.Outcome})
	}
	if len(results) != len(definition.Mutants) {
		return Result{}, errSettlementMissing
	}

	return Result{Terminal: Completed, Mutants: results}, nil
}

// Shrink demonstrates a whole-record and definition-member semantic reduction.
func Shrink(counterexample Counterexample, fingerprint FailureFingerprint) (Counterexample, error) {
	result, err := Replay(counterexample.Definition, counterexample.Trace)
	if err != nil {
		return Counterexample{}, err
	}
	found := false
	for _, mutant := range result.Mutants {
		found = found || mutant == MutantResult(fingerprint)
	}
	if !found {
		return Counterexample{}, errFingerprintMismatch
	}

	return Counterexample{
		Definition: Definition{Mutants: []string{fingerprint.Mutant}},
		Trace: Trace{Events: []Event{
			{Sequence: 1, Kind: BaselinePassed, Mutant: "", Outcome: 0},
			{Sequence: 2, Kind: PrimarySettled, Mutant: fingerprint.Mutant, Outcome: fingerprint.Outcome},
		}},
	}, nil
}
