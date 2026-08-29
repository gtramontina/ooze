package simulation

import (
	"reflect"
	"slices"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

type Definition struct{ value simulationDefinition }

type Choices []byte

type Trace struct{ value simulationTrace }

type MalformedFact struct{ value simulationMalformedFact }

func NewDefinition(definition campaign.Definition, capacity int, catalogue []string) Definition {
	definition.Command = slices.Clone(definition.Command)
	definition.Env = slices.Clone(definition.Env)
	return Definition{value: simulationDefinition{
		campaign: definition, capacity: capacity, catalogue: slices.Clone(catalogue),
	}}
}

func Explore(definition Definition, choices Choices) SimulationResult {
	return explore(definition.value, simulationChoiceBytes(slices.Clone(choices)))
}

func ReplayLegal(trace Trace) SimulationResult {
	return replayLegal(simulationCloneTrace(trace.value))
}

func ReplayViolation(prefix Trace, malformed MalformedFact) ViolationResult {
	selected, ok := simulationViolationPrefix(prefix.value)
	if !ok {
		return ViolationResult{failure: errNoActiveViolationPrefix}
	}
	result := replayViolation(selected, malformed.value)
	result.trace = simulationCloneTrace(selected)
	result.trace.malformed = &malformed.value
	return result
}

func Shrink(trace Trace, key FailureKey) Trace {
	return Trace{value: shrink(simulationCloneTrace(trace.value), key)}
}

func MalformedCampaign(fact campaign.Fact) MalformedFact {
	return MalformedFact{value: simulationMalformedFact{
		authority: simulationCampaignAuthority, campaign: fact,
	}}
}

func MalformedRuntime(cut processruntime.Cut) MalformedFact {
	return MalformedFact{value: simulationMalformedFact{
		authority: simulationRuntimeAuthority, runtimeCut: cut,
	}}
}

func MalformedSupervision(fact supervision.Fact) MalformedFact {
	return MalformedFact{value: simulationMalformedFact{
		authority: supervisionAuthority, supervisor: fact,
	}}
}

func (result SimulationResult) Trace() Trace {
	return Trace{value: simulationCloneTrace(result.trace)}
}

func (result SimulationResult) Failure() error { return result.failure }

func (result SimulationResult) FailureKey() FailureKey { return result.key }

func (result SimulationResult) SameWorld(other SimulationResult) bool {
	return reflect.DeepEqual(result.world, other.world)
}

func (trace Trace) LegalPrefix() Trace {
	cloned := simulationCloneTrace(trace.value)
	cloned.malformed = nil
	return Trace{value: cloned}
}

func (trace Trace) Malformed() MalformedFact {
	if trace.value.malformed == nil {
		return MalformedFact{}
	}
	return MalformedFact{value: *trace.value.malformed}
}

func (result ViolationResult) Trace() Trace {
	return Trace{value: simulationCloneTrace(result.trace)}
}

func (result ViolationResult) Failure() error { return result.failure }

func (result ViolationResult) FailureKey() FailureKey { return result.key }

func (result ViolationResult) SameWorld(other ViolationResult) bool {
	return reflect.DeepEqual(result.world, other.world)
}
