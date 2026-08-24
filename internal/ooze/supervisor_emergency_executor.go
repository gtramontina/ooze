package ooze

import "slices"

const supervisorEmergencyExecutorOperation = "execute supervisor emergency action"

type supervisorEmergencyExecutor struct {
	settleEmergency            func(emergencySweep) emergencySettlement
	deliverEmergencySettlement func([]supervisorEmergencyResidual)
}

func (executor supervisorEmergencyExecutor) execute(
	state supervisorState,
	action supervisorAction,
) *supervisorEvent {
	switch action.kind {
	case supervisorSettleEmergency:
		return executor.settle(state, action)
	case supervisorDeliverEmergencySettlement:
		executor.deliver(state, action)

		return nil
	default:
		invariant(supervisorEmergencyExecutorOperation, "action kind is not an emergency effect")

		return nil
	}
}

func (executor supervisorEmergencyExecutor) settle(
	state supervisorState,
	action supervisorAction,
) *supervisorEvent {
	requireSupervisorSettlementAction(executor, state, action)
	runtimeResolutions, acknowledged, residuals := normalizeSupervisorEmergencyResolutions(action.resolutions)
	settled := executor.settleEmergency(emergencySweep{resolutions: runtimeResolutions})
	validateSupervisorRuntimeSettlement(settled, acknowledged, residuals)
	completion := supervisorEmergencySettlementCompletion{
		action:       state.emergency.pendingAction,
		acknowledged: acknowledged,
		residuals:    residuals,
	}

	return &supervisorEvent{
		kind:                supervisorEmergencySettlementCompleted,
		emergencySettlement: &completion,
	}
}

func requireSupervisorSettlementAction(
	executor supervisorEmergencyExecutor,
	state supervisorState,
	action supervisorAction,
) {
	if action.token == 0 || len(action.residuals) != 0 || executor.settleEmergency == nil ||
		!state.emergency.active ||
		state.nextAction != action.token ||
		state.emergency.pendingAction != (supervisorPendingAction{kind: action.kind, token: action.token}) {
		invariant(supervisorEmergencyExecutorOperation, "settlement action is stale, wrong, or inexecutable")
	}
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

func (executor supervisorEmergencyExecutor) deliver(state supervisorState, action supervisorAction) {
	requireSupervisorDeliveryAction(executor, state, action)
	residuals := append([]supervisorEmergencyResidual(nil), action.residuals...)
	validateSupervisorDeliveryResiduals(residuals)
	executor.deliverEmergencySettlement(residuals)
}

func requireSupervisorDeliveryAction(
	executor supervisorEmergencyExecutor,
	state supervisorState,
	action supervisorAction,
) {
	if action.token == 0 || len(action.resolutions) != 0 || executor.deliverEmergencySettlement == nil ||
		!state.emergency.active ||
		state.emergency.pendingAction != (supervisorPendingAction{}) || state.nextAction != action.token {
		invariant(supervisorEmergencyExecutorOperation, "delivery action is stale, wrong, or inexecutable")
	}
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
