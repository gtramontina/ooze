package processruntime

type Observer interface{ Observe(RecordedCut) }

type ObserverFunc func(RecordedCut)

func (observe ObserverFunc) Observe(event RecordedCut) { observe(event) }

func runtimeEventAdmission(authority admissionAuthority) admissionAuthority {
	authority.delivery = nil
	return authority
}

func runtimeEventAdmissions[Values ~[]admissionAuthority](values Values) []admissionAuthority {
	result := make([]admissionAuthority, len(values))
	for index, value := range values {
		result[index] = runtimeEventAdmission(value)
	}
	return result
}

func runtimeEventAdmissionResult(result admissionResult) admissionResult {
	result.request = runtimeEventAdmission(result.request)
	result.deliveries = runtimeEventAdmissions(result.deliveries)
	return result
}

func runtimeEventBarrierResult(result barrierResult) barrierResult {
	result.request = runtimeEventAdmission(result.request)
	result.deliveries = runtimeEventAdmissions(result.deliveries)
	return result
}

func runtimeEventObservationResult(result observationResult) observationResult {
	result.deliveries = runtimeEventAdmissions(result.deliveries)
	result.cancelledWaiting = runtimeEventAdmissions(result.cancelledWaiting)
	result.compensatedGrants = runtimeEventAdmissions(result.compensatedGrants)
	return result
}

func runtimeEventClosure(result runtimeClosure) runtimeClosure {
	result.cancelledWaiting = runtimeEventAdmissions(result.cancelledWaiting)
	result.compensatedGrants = runtimeEventAdmissions(result.compensatedGrants)
	result.residual = append([]residualCustody(nil), result.residual...)
	return result
}

func runtimeEventEmergency(result emergencySettlement) emergencySettlement {
	result.acknowledged = append([]attemptGeneration(nil), result.acknowledged...)
	result.residual = append([]residualCustody(nil), result.residual...)
	return result
}
