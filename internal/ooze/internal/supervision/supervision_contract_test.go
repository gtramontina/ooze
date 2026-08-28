package supervision_test

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisionPublicLifecycle(t *testing.T) {
	t.Run("launch and wait", func(t *testing.T) {
		runtime, driver, boundary, start, spec := supervisionContractAttempt(t)
		close(boundary.wait)
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
		_, driver, boundary, start, spec := supervisionContractAttempt(t)
		launched := driver.Launch(start, spec)
		owned, ok := launched.Result().(supervision.Owned)
		require.True(t, ok)
		require.NotNil(t, owned.Attempt)

		driver.Stop(owned.Attempt)
		close(boundary.wait)
		terminal := driver.Wait(start.Generation(), owned.Attempt)
		_, stopped := terminal.Terminal().(supervision.Stopped)
		assert.True(t, stopped)
	})

	t.Run("emergency without waiter", func(t *testing.T) {
		runtime, driver, boundary, start, spec := supervisionContractAttempt(t)
		defer close(boundary.wait)
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

type supervisionBoundary struct {
	wait chan struct{}
}

func (*supervisionBoundary) Prepare(supervision.Generation, supervision.Spec) {}

func (boundary *supervisionBoundary) Execute(
	effect supervision.Effect,
) (supervision.Fact, bool) {
	switch effect.Kind() {
	case supervision.LaunchNativeEffect:
		return effect.LaunchReleasedFact(time.Now())
	case supervision.RevokeLaunchReleaseEffect:
		return supervision.Fact{}, true
	case supervision.WaitRootEffect:
		<-boundary.wait

		return effect.RootExitFact(time.Now(), supervision.ExitStatus{})
	case supervision.ForceOwnedEffect, supervision.ObserveEmptinessEffect,
		supervision.CaptureOutputEffect, supervision.ReleaseDomainEffect:
		return effect.SystemCompletionFact(time.Now())
	default:
		return supervision.Fact{}, false
	}
}

func (*supervisionBoundary) RecheckRoot(supervision.Generation) (supervision.ExitStatus, time.Time, bool, error) {
	return supervision.ExitStatus{}, time.Now(), false, nil
}

func (*supervisionBoundary) SampleRunning(supervision.Generation) (bool, uint64, error) {
	return false, 0, nil
}

func (*supervisionBoundary) ReadOutput(uint64) string { return "contract output" }

func (*supervisionBoundary) ReadDiagnostic(uint64) error { return nil }

func (*supervisionBoundary) RecordDiagnostic(error) uint64 { return 1 }

func supervisionContractAttempt(
	t *testing.T,
) (*processruntime.Runtime, *supervision.Driver, *supervisionBoundary, processruntime.PreparedStart, supervision.Spec) {
	t.Helper()
	runtime := processruntime.New(1)
	registration := runtime.RegisterCampaign(1)
	require.Equal(t, processruntime.CampaignRegistered, registration.Decision())
	request := runtime.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "contract",
		Class: processruntime.SerialPrimaryAdmission, Profile: processruntime.SerialProfile,
		Deadline: 5 * time.Second,
	})
	grant, received := request.Receive()
	require.True(t, received)
	boundary := &supervisionBoundary{wait: make(chan struct{})}
	driver, err := supervision.NewDriver(runtime, 2*time.Second, 3*time.Second, boundary)
	require.NoError(t, err)
	spec := supervision.Spec{
		Attempt: request.Request().Attempt, Command: []string{"contract"},
		Dir: t.TempDir(), Profile: supervision.SerialProfile, Deadline: 5 * time.Second,
	}
	cell := processruntime.NewStartCell()
	driver.ReserveLaunch(cell, spec)
	start := runtime.CommitStart(grant, cell)
	require.Equal(t, processruntime.StartAccepted, start.Decision())

	return runtime, driver, boundary, start, spec
}
