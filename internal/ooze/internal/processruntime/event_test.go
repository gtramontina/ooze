package processruntime_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
)

func TestEventRetainsImmutableAcceptedTransition(t *testing.T) {
	command := processruntime.Command{
		Admission: processruntime.Admission{Attempt: "mutant-a"},
	}
	result := processruntime.Result{
		Admission: processruntime.AdmissionResult{
			Deliveries: []processruntime.Admission{{Attempt: "mutant-a"}},
		},
	}
	event := processruntime.Accepted(processruntime.RequestAdmission, command, result)

	command.Admission.Attempt = "changed"
	result.Admission.Deliveries[0].Attempt = "changed"
	observed := event.Result()
	observed.Admission.Deliveries[0].Attempt = "changed-again"

	assert.Equal(t, processruntime.RequestAdmission, event.Kind())
	assert.Equal(t, "mutant-a", event.Command().Admission.Attempt)
	assert.Equal(t, "mutant-a", event.Result().Admission.Deliveries[0].Attempt)
}
