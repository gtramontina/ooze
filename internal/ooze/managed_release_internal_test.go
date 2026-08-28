package ooze

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/stretchr/testify/assert"
)

func TestManagedCleanupFailureRetainsOrderedResidualEvidence(t *testing.T) {
	result := presentManagedRelease(campaign.Result{
		Outcome: campaign.ManagedCleanupUnconfirmed,
		Residual: []campaign.ResidualCustody{
			{Attempt: "campaign-1:2", Generation: 7, Transferred: true},
			{Attempt: "campaign-1:3", Generation: 9, Prospective: true},
		},
	})

	assert.Equal(t, ManagedCleanupUnconfirmed, result.Outcome)
	assert.Equal(t, []ManagedResidualCustody{
		{Attempt: "campaign-1:2", Generation: 7, Stage: ManagedResidualOwned, Transferred: true},
		{Attempt: "campaign-1:3", Generation: 9, Stage: ManagedResidualProspective},
	}, result.Residual)
}
