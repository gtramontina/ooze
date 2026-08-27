package processruntime_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeOwnsACompleteAttemptLifecycle(t *testing.T) {
	t.Run("registered campaign settles an owned attempt and commits terminal", func(t *testing.T) {
		runtime := processruntime.New(1)
		registration := runtime.RegisterCampaign(processruntime.Lineage(11))
		require.Equal(t, processruntime.CampaignRegistered, registration.Decision())

		await := runtime.RequestAdmission(processruntime.Admission{
			Campaign: registration.Campaign(), Attempt: "baseline", Class: processruntime.SharedAdmission,
		})
		require.Equal(t, processruntime.AdmissionAccepted, await.Decision())
		grant, ok := await.Receive()
		require.True(t, ok)

		prepared := runtime.CommitStart(grant, processruntime.NewStartCell())
		require.Equal(t, processruntime.StartAccepted, prepared.Decision())
		generation := prepared.Generation()
		require.NotZero(t, generation)

		owned := prepared.Launch(func(processruntime.Generation) processruntime.Observation {
			return processruntime.Owned()
		})
		assert.True(t, owned.SettlementPending())

		settled := runtime.Observe(generation, processruntime.Settled())
		assert.True(t, settled.SettlementAcknowledged())

		terminal := runtime.CommitTerminal(registration.Campaign())
		assert.Equal(t, processruntime.TerminalCommitted, terminal.Decision())
	})
}
