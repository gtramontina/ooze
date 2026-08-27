package ooze

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

type simulationAdmission struct {
	campaign campaignToken
	attempt  attemptIdentity
	class    processruntime.AdmissionClass
	profile  Profile
	deadline simulationDuration
}

func simulationTraceAdmission(value admissionAuthority) simulationAdmission {
	return simulationAdmission{
		campaign: value.campaign, attempt: value.attempt, class: value.class,
		profile: value.profile, deadline: simulationTraceDuration(value.deadline),
	}
}

func (admission simulationAdmission) production() admissionAuthority {
	return admissionAuthority{
		campaign: admission.campaign, attempt: admission.attempt, class: admission.class,
		profile: admission.profile, deadline: admission.deadline.production(),
	}
}

func simulationTraceAdmissions[Values ~[]admissionAuthority](values Values) []simulationAdmission {
	result := make([]simulationAdmission, len(values))
	for index, value := range values {
		result[index] = simulationTraceAdmission(value)
	}
	return result
}

type simulationRuntimeState = processruntime.Projection

func simulationTraceRuntimeState(value processruntime.Replay) simulationRuntimeState {
	return value.Projection()
}
