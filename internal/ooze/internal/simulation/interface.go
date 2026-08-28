package simulation

import (
	"reflect"
	"slices"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

// Definition fixes one deterministic simulation scenario.
type Definition struct{ value simulationDefinition }

// Choices select from the legal moves enabled during exploration.
type Choices []byte

// Trace is the immutable replay authority produced by exploration.
type Trace struct{ value simulationTrace }

// MalformedFact is one deliberately invalid input for violation replay.
type MalformedFact struct{ value simulationMalformedFact }

// NewDefinition constructs a deterministic simulation definition.
func NewDefinition(definition campaign.Definition, capacity int, catalogue []string) Definition {
	definition.Command = slices.Clone(definition.Command)
	definition.Env = slices.Clone(definition.Env)
	return Definition{value: simulationDefinition{
		campaign: definition, capacity: capacity, catalogue: slices.Clone(catalogue),
	}}
}

// Explore evaluates one definition using the supplied legal choices.
func Explore(definition Definition, choices Choices) SimulationResult {
	return explore(definition.value, simulationChoiceBytes(slices.Clone(choices)))
}

// ReplayLegal replays every accepted fact in a trace.
func ReplayLegal(trace Trace) SimulationResult {
	return replayLegal(simulationCloneTrace(trace.value))
}

// ReplayViolation applies one malformed fact after a legal prefix.
func ReplayViolation(prefix Trace, malformed MalformedFact) ViolationResult {
	result := replayViolation(simulationCloneTrace(prefix.value), malformed.value)
	result.trace = simulationCloneTrace(prefix.value)
	result.trace.malformed = &malformed.value
	return result
}

// Shrink minimizes a trace while preserving its semantic failure identity.
func Shrink(trace Trace, key FailureKey) Trace {
	return Trace{value: shrink(simulationCloneTrace(trace.value), key)}
}

// MalformedCampaign constructs an invalid campaign input.
func MalformedCampaign(fact campaign.Fact) MalformedFact {
	return MalformedFact{value: simulationMalformedFact{
		authority: simulationCampaignAuthority, campaign: fact,
	}}
}

// MalformedRuntime constructs an invalid process-runtime input.
func MalformedRuntime(cut processruntime.Cut) MalformedFact {
	return MalformedFact{value: simulationMalformedFact{
		authority: simulationRuntimeAuthority, runtimeCut: cut,
	}}
}

// MalformedSupervision constructs an invalid supervision input.
func MalformedSupervision(fact supervision.Fact) MalformedFact {
	return MalformedFact{value: simulationMalformedFact{
		authority: supervisionAuthority, supervisor: fact,
	}}
}

// Trace returns the explored or replayed trace.
func (result SimulationResult) Trace() Trace {
	return Trace{value: simulationCloneTrace(result.trace)}
}

// Failure returns an exploration or replay failure.
func (result SimulationResult) Failure() error { return result.failure }

// FailureKey returns the semantic identity of a simulation failure.
func (result SimulationResult) FailureKey() FailureKey { return result.key }

// SameWorld reports whether two results reached the same canonical world.
func (result SimulationResult) SameWorld(other SimulationResult) bool {
	return reflect.DeepEqual(result.world, other.world)
}

// LegalPrefix removes the malformed input from a violation trace.
func (trace Trace) LegalPrefix() Trace {
	cloned := simulationCloneTrace(trace.value)
	cloned.malformed = nil
	return Trace{value: cloned}
}

// Malformed returns the malformed input carried by a violation trace.
func (trace Trace) Malformed() MalformedFact {
	if trace.value.malformed == nil {
		return MalformedFact{}
	}
	return MalformedFact{value: *trace.value.malformed}
}

// Trace returns the violated trace.
func (result ViolationResult) Trace() Trace {
	return Trace{value: simulationCloneTrace(result.trace)}
}

// Failure returns a violation replay failure.
func (result ViolationResult) Failure() error { return result.failure }

// FailureKey returns the invariant's semantic identity.
func (result ViolationResult) FailureKey() FailureKey { return result.key }

// SameWorld reports whether two violations reached the same canonical world.
func (result ViolationResult) SameWorld(other ViolationResult) bool {
	return reflect.DeepEqual(result.world, other.world)
}
