package processruntime_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
)

func TestEventRetainsImmutableAcceptedTransition(t *testing.T) {
	admission := processruntime.Admission{
		Attempt: "mutant-a", Class: processruntime.SharedAdmission, Profile: processruntime.AutomaticProfile,
	}
	result := processruntime.AdmissionResult{
		Decision: processruntime.AdmissionAccepted,
		Deliveries: []processruntime.Admission{{
			Attempt: "mutant-a", Class: processruntime.SharedAdmission, Profile: processruntime.AutomaticProfile,
		}},
	}
	event, err := processruntime.NewAdmissionRequested(admission, result)
	assert.NoError(t, err)

	admission.Attempt = "changed"
	result.Deliveries[0].Attempt = "changed"
	observed := event.Result()
	observed.Deliveries[0].Attempt = "changed-again"

	assert.Equal(t, "mutant-a", event.Admission().Attempt)
	assert.Equal(t, "mutant-a", event.Result().Deliveries[0].Attempt)
}

func TestEventRejectsMismatchedDomainValues(t *testing.T) {
	_, err := processruntime.NewAdmissionRequested(
		processruntime.Admission{Class: processruntime.AdmissionClass(255)},
		processruntime.AdmissionResult{Decision: processruntime.AdmissionAccepted},
	)

	assert.Error(t, err)
}
