package campaign

func (runner *managedCampaignRunner) needsEmergencySettlement() bool {
	_, requested := runner.state.runtimeEmergencySettlementRequest()
	return !runner.emergency && requested
}

func (runner *managedCampaignRunner) settleEmergency() []campaignEffect {
	epoch, requested := runner.state.runtimeEmergencySettlementRequest()
	if !requested {
		panic("managed emergency settlement was not requested")
	}
	observed := runner.attempts.emergency(epoch)
	if observed.epoch != epoch || observed.settlement.Epoch() != uint64(epoch) {
		panic("managed emergency settlement has the wrong epoch")
	}
	runner.emergency = true
	runner.pending = 0

	return runner.advance(runtimeEmergencySettledEvent{epoch: epoch, settlement: campaignSettlement(observed.settlement)})
}
