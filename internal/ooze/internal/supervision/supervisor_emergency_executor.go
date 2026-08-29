package supervision

import "slices"

const supervisorEmergencyExecutorOperation = "execute supervisor emergency action"

type supervisorEmergencyExecutor struct {
	settleEmergency            func(emergencySweep) emergencySettlement
	deliverEmergencySettlement func([]supervisorEmergencyResidual)
}

func (executor supervisorEmergencyExecutor) execute(
	machine *Machine,
	effect Effect,
) *Fact {
	switch effect.kind {
	case supervisorSettleEmergency:
		return executor.settle(machine, effect)
	case supervisorDeliverEmergencySettlement:
		executor.deliver(machine, effect)

		return nil
	default:
		invariant(supervisorEmergencyExecutorOperation, "action kind is not an emergency effect")

		return nil
	}
}

func (executor supervisorEmergencyExecutor) settle(
	machine *Machine,
	effect Effect,
) *Fact {
	resolutions, ready := machine.emergencySettlementRequest(effect)
	if !ready || executor.settleEmergency == nil {
		invariant(supervisorEmergencyExecutorOperation, "settlement effect is stale, wrong, or inexecutable")
	}
	runtimeResolutions, acknowledged, residuals := normalizeSupervisorEmergencyResolutions(resolutions)
	settled := executor.settleEmergency(emergencySweep{resolutions: runtimeResolutions})
	validateSupervisorRuntimeSettlement(settled, acknowledged, residuals)
	fact, ready := machine.emergencySettlementFact(effect, acknowledged, residuals)
	if !ready {
		invariant(supervisorEmergencyExecutorOperation, "settlement effect cannot produce a completion fact")
	}

	return &fact
}

func normalizeSupervisorEmergencyResolutions(
	resolutions []supervisorEmergencyResolution,
) ([]emergencyResolution, []attemptGeneration, []supervisorEmergencyResolution) {
	var runtimeResolutions []emergencyResolution
	var acknowledged []attemptGeneration
	var residualOwned []supervisorEmergencyResolution
	var previous attemptGeneration
	for _, resolution := range resolutions {
		if resolution.generation == 0 || resolution.generation <= previous {
			invariant(supervisorEmergencyExecutorOperation, "settlement resolutions are not canonical")
		}
		previous = resolution.generation
		disposition := emergencyConfirmedDrained
		switch resolution.kind {
		case supervisorEmergencyConfirmedDrained:
		case supervisorEmergencyResidualOwned:
			disposition = emergencyCustodyTransferred
			residualOwned = append(residualOwned, resolution)
		default:
			invariant(supervisorEmergencyExecutorOperation, "settlement resolution is unauthorized")
		}
		runtimeResolutions = append(runtimeResolutions, emergencyResolution{
			generation: resolution.generation, disposition: disposition,
		})
		acknowledged = append(acknowledged, resolution.generation)
	}

	return runtimeResolutions, acknowledged, residualOwned
}

func (executor supervisorEmergencyExecutor) deliver(machine *Machine, effect Effect) {
	residuals, ready := machine.emergencyDelivery(effect)
	if !ready || executor.deliverEmergencySettlement == nil {
		invariant(supervisorEmergencyExecutorOperation, "delivery effect is stale, wrong, or inexecutable")
	}
	validateSupervisorDeliveryResiduals(residuals)
	executor.deliverEmergencySettlement(residuals)
}

func validateSupervisorDeliveryResiduals(residuals []supervisorEmergencyResidual) {
	var previous attemptGeneration
	for _, residual := range residuals {
		if residual.generation == 0 || residual.generation <= previous || residual.attempt == "" ||
			residual.kind != supervisorResidualOwned {
			invariant(supervisorEmergencyExecutorOperation, "delivery residual is unauthorized or noncanonical")
		}
		previous = residual.generation
	}
}

func validateSupervisorRuntimeSettlement(
	settled emergencySettlement,
	acknowledged []attemptGeneration,
	residuals []supervisorEmergencyResolution,
) {
	if !slices.Equal(settled.acknowledged, acknowledged) || len(settled.residual) != len(residuals) {
		invariant(supervisorEmergencyExecutorOperation, "runtime settlement inventory is mismatched")
	}
	seen := make([]attemptGeneration, 0, len(settled.residual))
	for _, residual := range settled.residual {
		if residual.generation == 0 || residual.stage != admissionOwned || !residual.transferred ||
			slices.Contains(seen, residual.generation) ||
			!slices.ContainsFunc(residuals, func(authorized supervisorEmergencyResolution) bool {
				return authorized.generation == residual.generation
			}) {
			invariant(supervisorEmergencyExecutorOperation, "runtime residual custody is invalid")
		}
		seen = append(seen, residual.generation)
	}
}
