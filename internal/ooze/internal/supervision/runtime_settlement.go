package supervision

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

type admissionStage uint8

const (
	admissionWaiting admissionStage = iota + 1
	admissionGranted
	admissionProspective
	admissionOwned
)

type residualCustody struct {
	generation  attemptGeneration
	attempt     attemptIdentity
	stage       admissionStage
	transferred bool
}

type emergencyDisposition uint8

const (
	emergencyConfirmedDrained emergencyDisposition = iota + 1
	emergencyCustodyTransferred
)

type emergencyResolution struct {
	generation  attemptGeneration
	disposition emergencyDisposition
}

type emergencySweep struct{ resolutions []emergencyResolution }

type emergencySettlement struct {
	epoch        uint64
	owner        processruntime.Campaign
	acknowledged []attemptGeneration
	residual     []residualCustody
}

func processRuntimeResolutions(sweep emergencySweep) []processruntime.Resolution {
	result := make([]processruntime.Resolution, len(sweep.resolutions))
	for index, resolution := range sweep.resolutions {
		result[index] = processruntime.ConfirmedDrained(resolution.generation)
		if resolution.disposition == emergencyCustodyTransferred {
			result[index] = processruntime.TransferCustody(resolution.generation)
		}
	}
	return result
}

func runtimeEmergencySettlement(settlement processruntime.EmergencySettlement) emergencySettlement {
	residuals := make([]residualCustody, len(settlement.Residual()))
	for index, residual := range settlement.Residual() {
		stage := admissionOwned
		if residual.Prospective() {
			stage = admissionProspective
		}
		residuals[index] = residualCustody{
			generation: residual.Generation(), attempt: attemptIdentity(residual.Attempt()),
			stage: stage, transferred: residual.Transferred(),
		}
	}
	return emergencySettlement{
		epoch: settlement.Epoch(), owner: settlement.Owner(),
		acknowledged: settlement.Acknowledged(), residual: residuals,
	}
}
