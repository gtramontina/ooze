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
		Decision: processruntime.AdmissionAccepted, Request: admission,
		Deliveries: []processruntime.Admission{{
			Attempt: "mutant-a", Class: processruntime.SharedAdmission, Profile: processruntime.AutomaticProfile,
		}},
	}
	event, err := processruntime.NewAdmissionRequestProcessed(admission, result)
	assert.NoError(t, err)

	admission.Attempt = "changed"
	result.Deliveries[0].Attempt = "changed"
	requested := event.Variant().(processruntime.AdmissionRequestProcessed)
	observed := requested.Result()
	observed.Deliveries[0].Attempt = "changed-again"

	assert.Equal(t, "mutant-a", requested.Admission().Attempt)
	assert.Equal(t, "mutant-a", requested.Result().Deliveries[0].Attempt)
}

func TestEventRejectsMismatchedDomainValues(t *testing.T) {
	t.Run("invalid admission class", func(t *testing.T) {
		_, err := processruntime.NewAdmissionRequestProcessed(
			processruntime.Admission{Class: processruntime.AdmissionClass(255)},
			processruntime.AdmissionResult{Decision: processruntime.AdmissionAccepted},
		)

		assert.Error(t, err)
	})

	t.Run("result belongs to another request", func(t *testing.T) {
		admission := processruntime.Admission{
			Campaign: processruntime.Campaign{ID: 1, Lineage: 2}, Attempt: "mutant-a",
			Class: processruntime.SharedAdmission, Profile: processruntime.AutomaticProfile,
		}
		result := processruntime.AdmissionResult{
			Decision: processruntime.AdmissionAccepted,
			Request: processruntime.Admission{
				Campaign: admission.Campaign, Attempt: "mutant-b",
				Class: processruntime.SharedAdmission, Profile: processruntime.AutomaticProfile,
			},
		}

		_, err := processruntime.NewAdmissionRequestProcessed(admission, result)

		assert.Error(t, err)
	})

	t.Run("cancellation reports a request decision", func(t *testing.T) {
		admission := processruntime.Admission{
			Campaign: processruntime.Campaign{ID: 1, Lineage: 2}, Attempt: "mutant-a",
			Class: processruntime.SharedAdmission, Profile: processruntime.AutomaticProfile,
		}

		_, err := processruntime.NewAdmissionCancellationProcessed(admission, processruntime.AdmissionResult{
			Decision: processruntime.AdmissionAccepted, Request: admission,
		})

		assert.Error(t, err)
	})
}

func TestEventVariantsCannotBePublishedWithoutValidation(t *testing.T) {
	_, published := any(processruntime.CampaignRegistrationProcessed{}).(processruntime.Event)

	assert.False(t, published)
}
