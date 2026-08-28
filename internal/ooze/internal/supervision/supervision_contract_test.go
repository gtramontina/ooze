//go:build darwin || linux || windows

package supervision_test

import (
	"os"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisionPublicLifecycle(t *testing.T) {
	t.Run("launch and wait", func(t *testing.T) {
		runtime, driver, start, spec := supervisionContractAttempt(t, "exit")
		launched := driver.Launch(start, spec)
		owned, ok := launched.Result().(supervision.Owned)
		require.True(t, ok)
		require.NotNil(t, owned.Attempt)

		terminal := driver.Wait(start.Generation(), owned.Attempt)
		settled, ok := terminal.Terminal().(supervision.Settled)
		require.True(t, ok)
		assert.True(t, settled.Exit.Passed())
		assert.True(t, terminal.Receipt().SettlementAcknowledged())
		assert.False(t, runtime.EmergencySettlementRequired())
	})

	t.Run("stop before wait", func(t *testing.T) {
		_, driver, start, spec := supervisionContractAttempt(t, "block")
		launched := driver.Launch(start, spec)
		owned, ok := launched.Result().(supervision.Owned)
		require.True(t, ok)
		require.NotNil(t, owned.Attempt)

		driver.Stop(owned.Attempt)
		terminal := driver.Wait(start.Generation(), owned.Attempt)
		_, stopped := terminal.Terminal().(supervision.Stopped)
		assert.True(t, stopped)
	})

	t.Run("emergency without waiter", func(t *testing.T) {
		runtime, driver, start, spec := supervisionContractAttempt(t, "block")
		launched := driver.Launch(start, spec)
		owned, ok := launched.Result().(supervision.Owned)
		require.True(t, ok)
		require.NotNil(t, owned.Attempt)
		closure := runtime.Close("supervision contract emergency")
		require.NotZero(t, closure.Epoch())

		at := time.Now()
		sweep, settlement := driver.EmergencyDrain(supervision.EmergencyRequest{
			At: at, DrainBy: at.Add(3 * time.Second),
		})
		_, drained := sweep.(supervision.SweepDrained)
		assert.True(t, drained)
		assert.NotZero(t, settlement.Epoch())
		_, stopped := owned.Attempt.Wait().(supervision.Stopped)
		assert.True(t, stopped)
	})
}

func supervisionContractAttempt(
	t *testing.T,
	mode string,
) (*processruntime.Runtime, *supervision.Driver, processruntime.PreparedStart, supervision.Spec) {
	t.Helper()
	runtime := processruntime.New(1)
	registration := runtime.RegisterCampaign(1)
	require.Equal(t, processruntime.CampaignRegistered, registration.Decision())
	request := runtime.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "contract-" + mode,
		Class: processruntime.SerialPrimaryAdmission, Profile: processruntime.SerialProfile,
		Deadline: 5 * time.Second,
	})
	grant, received := request.Receive()
	require.True(t, received)
	driver, err := supervision.NewNativeDriver(runtime, 2*time.Second, 3*time.Second)
	require.NoError(t, err)
	spec := supervision.Spec{
		Attempt: request.Request().Attempt,
		Command: []string{os.Args[0], "-test.run=^TestSupervisionContractProcess$"},
		Dir:     t.TempDir(), Env: append(os.Environ(), "OOZE_SUPERVISION_CONTRACT="+mode),
		Profile: supervision.SerialProfile, Deadline: 5 * time.Second,
	}
	cell := processruntime.NewStartCell()
	driver.ReserveLaunch(cell, spec)
	start := runtime.CommitStart(grant, cell)
	require.Equal(t, processruntime.StartAccepted, start.Decision())

	return runtime, driver, start, spec
}

func TestSupervisionContractProcess(t *testing.T) {
	switch os.Getenv("OOZE_SUPERVISION_CONTRACT") {
	case "exit":
		return
	case "block":
		time.Sleep(time.Hour)
	}
}
