package processruntime_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeOwnsAnAttemptLifecycle(t *testing.T) {
	runtime := processruntime.New(1)
	registration := runtime.RegisterCampaign(41)
	require.Equal(t, processruntime.CampaignRegistered, registration.Decision())

	await := runtime.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(),
		Attempt:  "mutant-a",
		Class:    processruntime.SharedAdmission,
		Profile:  processruntime.AutomaticProfile,
	})
	require.Equal(t, processruntime.AdmissionAccepted, await.Decision())
	grant, received := await.Receive()
	require.True(t, received)

	start := runtime.CommitStart(grant, processruntime.NewStartCell())
	require.Equal(t, processruntime.StartAccepted, start.Decision())
	owned := start.Launch(func(processruntime.Generation) processruntime.Observation {
		return processruntime.Owned()
	})
	ownedReceipt := runtime.Observe(start.Generation(), owned)
	assert.False(t, ownedReceipt.SettlementAcknowledged())

	settled := runtime.Observe(start.Generation(), processruntime.Settled(processruntime.AutomaticProfile, 0))
	assert.True(t, settled.SettlementAcknowledged())
	assert.Equal(t, processruntime.TerminalCommitted, runtime.CommitTerminal(registration.Campaign()).Decision())
}
