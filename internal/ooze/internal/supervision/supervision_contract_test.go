package supervision_test

import (
	"errors"
	"sync"
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

	t.Run("launch boundary closes a late not-released launch", func(t *testing.T) {
		runtime, driver, boundary, start, spec := supervisionContractAttempt(t)
		boundary.mutex.Lock()
		boundary.launchGate = make(chan struct{})
		boundary.launchBoundary = make(chan time.Time, 1)
		boundary.launchNotReleased = true
		boundary.mutex.Unlock()
		launched := make(chan supervision.ObservedLaunch, 1)
		go func() { launched <- driver.Launch(start, spec) }()

		launchBy := <-boundary.awaitedLaunch
		boundary.setNow(launchBy.Add(time.Nanosecond))
		boundary.launchBoundary <- launchBy
		observed := <-launched
		unconfirmed, ok := observed.Result().(supervision.LaunchUnconfirmed)
		require.True(t, ok)
		assert.Equal(t, supervision.ProspectiveUnresolved, unconfirmed.Residual)
		_, beforeCompletion := driver.Snapshot()
		close(boundary.launchGate)
		require.Eventually(t, func() bool {
			_, afterCompletion := driver.Snapshot()

			return boundary.completedLaunch() && !afterCompletion.Equal(beforeCompletion)
		}, time.Second, time.Millisecond)
		assert.True(t, runtime.EmergencySettlementRequired())
		at := boundary.Now().Add(time.Nanosecond)
		sweep, settlement := driver.EmergencyDrain(supervision.EmergencyRequest{
			At: at, DrainBy: at.Add(time.Second),
		})
		_, drained := sweep.(supervision.SweepDrained)
		assert.True(t, drained)
		assert.NotZero(t, settlement.Epoch())
	})

	t.Run("residual custody reaches wait and emergency sweep", func(t *testing.T) {
		runtime, driver, boundary, start, spec := supervisionContractAttempt(t)
		defer close(boundary.wait)
		boundary.mutex.Lock()
		boundary.residual = true
		boundary.mutex.Unlock()
		launched := driver.Launch(start, spec)
		owned, ok := launched.Result().(supervision.Owned)
		require.True(t, ok)

		driver.Stop(owned.Attempt)
		terminal := driver.Wait(start.Generation(), owned.Attempt)
		unconfirmed, ok := terminal.Terminal().(supervision.DrainUnconfirmed)
		require.True(t, ok)
		assert.Equal(t, supervision.OwnedUndrained, unconfirmed.Residual)
		assert.True(t, runtime.EmergencySettlementRequired())
		at := boundary.Now().Add(time.Nanosecond)
		sweep, settlement := driver.EmergencyDrain(supervision.EmergencyRequest{
			At: at, DrainBy: at.Add(time.Second),
		})
		residuals, ok := sweep.(supervision.SweepUnconfirmed)
		require.True(t, ok)
		assert.Equal(t, []supervision.ResidualRef{{
			Attempt: spec.Attempt, Kind: supervision.OwnedUndrained,
		}}, residuals.Residuals())
		assert.NotZero(t, settlement.Epoch())
	})

	for _, test := range []struct {
		name       string
		cause      supervision.Cause
		diagnostic func(supervision.FailureDiagnostics) string
	}{
		{name: "wait", cause: supervision.WaitFailed, diagnostic: func(value supervision.FailureDiagnostics) string { return value.Wait }},
		{name: "drain census", cause: supervision.CensusFailed, diagnostic: func(value supervision.FailureDiagnostics) string { return value.DrainCensus }},
		{name: "termination", cause: supervision.TerminationControlFailed, diagnostic: func(value supervision.FailureDiagnostics) string { return value.Termination }},
		{name: "output", cause: supervision.OutputCaptureFailed, diagnostic: func(value supervision.FailureDiagnostics) string { return value.Output }},
		{name: "release", cause: supervision.ReleaseFailed, diagnostic: func(value supervision.FailureDiagnostics) string { return value.Release }},
	} {
		t.Run(test.name+" failure remains public", func(t *testing.T) {
			_, driver, boundary, start, spec := supervisionContractAttempt(t)
			boundary.mutex.Lock()
			boundary.failure = test.cause
			if test.cause == supervision.TerminationControlFailed {
				boundary.residual = true
			}
			boundary.mutex.Unlock()
			launched := driver.Launch(start, spec)
			owned, ok := launched.Result().(supervision.Owned)
			require.True(t, ok)
			close(boundary.wait)

			terminal := driver.Wait(start.Generation(), owned.Attempt)
			failure, ok := terminal.Terminal().(supervision.Infrastructure)
			require.True(t, ok)
			assert.Equal(t, test.cause, failure.Cause)
			assert.EqualError(t, failure.Err, test.name+" failure")
			assert.Equal(t, test.name+" failure", test.diagnostic(failure.Failures))
		})
	}
}

type supervisionBoundary struct {
	wait              chan struct{}
	launchGate        chan struct{}
	launchBoundary    chan time.Time
	launchNotReleased bool
	awaitedLaunch     chan time.Time
	mutex             sync.Mutex
	now               time.Time
	launchCompleted   bool
	residual          bool
	failure           supervision.Cause
	drainFailures     int
	drainResiduals    int
	diagnostics       map[uint64]error
	nextDiagnostic    uint64
}

func (boundary *supervisionBoundary) Now() time.Time {
	boundary.mutex.Lock()
	defer boundary.mutex.Unlock()
	if boundary.now.IsZero() {
		return time.Now()
	}

	return boundary.now
}

func (boundary *supervisionBoundary) setNow(at time.Time) {
	boundary.mutex.Lock()
	boundary.now = at
	boundary.mutex.Unlock()
}

func (boundary *supervisionBoundary) AwaitLaunch(at time.Time) <-chan time.Time {
	boundary.mutex.Lock()
	controlled := boundary.launchBoundary
	boundary.mutex.Unlock()
	if controlled != nil {
		boundary.awaitedLaunch <- at

		return controlled
	}

	return time.After(time.Until(at))
}

func (*supervisionBoundary) AwaitCommand(at time.Time) <-chan time.Time {
	return time.After(time.Until(at))
}

func (*supervisionBoundary) SampleTicks() (<-chan time.Time, func()) {
	ticker := time.NewTicker(time.Hour)

	return ticker.C, ticker.Stop
}

func (*supervisionBoundary) Prepare(supervision.Generation, supervision.Spec) {}

func (boundary *supervisionBoundary) Execute(
	effect supervision.Effect,
) (supervision.Fact, bool) {
	switch effect.Kind() {
	case supervision.LaunchNativeEffect:
		boundary.mutex.Lock()
		gate := boundary.launchGate
		notReleased := boundary.launchNotReleased
		boundary.mutex.Unlock()
		if gate != nil {
			<-gate
		}
		if notReleased {
			fact, ready := effect.LaunchNotReleasedFact(boundary.Now(), supervision.LaunchFailed, 0)
			boundary.mutex.Lock()
			boundary.launchCompleted = true
			boundary.mutex.Unlock()

			return fact, ready
		}
		return effect.LaunchReleasedFact(boundary.Now())
	case supervision.RevokeLaunchReleaseEffect:
		return supervision.Fact{}, true
	case supervision.WaitRootEffect:
		<-boundary.wait
		boundary.mutex.Lock()
		failure := boundary.failure
		boundary.mutex.Unlock()
		if failure == supervision.WaitFailed {
			return effect.WaitFailureFact(boundary.Now(), boundary.record(errors.New("wait failure")))
		}

		return effect.RootExitFact(boundary.Now(), supervision.ExitStatus{})
	case supervision.ForceOwnedEffect, supervision.ObserveEmptinessEffect,
		supervision.CaptureOutputEffect, supervision.ReleaseDomainEffect:
		boundary.mutex.Lock()
		residual := boundary.residual
		failure := boundary.failure
		drainFailures := boundary.drainFailures
		drainResiduals := boundary.drainResiduals
		if effect.Kind() == supervision.ObserveEmptinessEffect && failure == supervision.CensusFailed {
			boundary.drainFailures++
		}
		if effect.Kind() == supervision.ObserveEmptinessEffect && residual {
			boundary.drainResiduals++
		}
		boundary.mutex.Unlock()
		if effect.Kind() == supervision.ForceOwnedEffect && failure == supervision.TerminationControlFailed {
			return effect.DrainFailureFact(
				boundary.Now(), 0, boundary.record(errors.New("termination failure")),
			)
		}
		if effect.Kind() == supervision.ObserveEmptinessEffect &&
			failure == supervision.CensusFailed && drainFailures == 0 {
			return effect.DrainFailureFact(
				boundary.Now(), 0, boundary.record(errors.New("drain census failure")),
			)
		}
		if effect.Kind() == supervision.CaptureOutputEffect && failure == supervision.OutputCaptureFailed {
			return effect.OutputFailureFact(
				boundary.Now(), 1, 1, 0, 0, boundary.record(errors.New("output failure")),
			)
		}
		if effect.Kind() == supervision.ReleaseDomainEffect && failure == supervision.ReleaseFailed {
			return effect.ReleaseFailureFact(boundary.Now(), boundary.record(errors.New("release failure")))
		}
		if residual && effect.Kind() == supervision.ObserveEmptinessEffect {
			if failure == supervision.TerminationControlFailed && drainResiduals == 0 {
				return effect.DrainResidualFact(boundary.Now())
			}
			if failure == supervision.TerminationControlFailed {
				return effect.SystemCompletionFact(boundary.Now())
			}
			boundary.setNow(effect.DrainBy())

			return effect.DrainResidualFact(effect.DrainBy())
		}
		return effect.SystemCompletionFact(boundary.Now())
	default:
		return supervision.Fact{}, false
	}
}

func (boundary *supervisionBoundary) completedLaunch() bool {
	boundary.mutex.Lock()
	defer boundary.mutex.Unlock()

	return boundary.launchCompleted
}

func (*supervisionBoundary) RecheckRoot(supervision.Generation) (supervision.ExitStatus, time.Time, bool, error) {
	return supervision.ExitStatus{}, time.Now(), false, nil
}

func (*supervisionBoundary) SampleRunning(supervision.Generation) (bool, uint64, error) {
	return false, 0, nil
}

func (*supervisionBoundary) ReadOutput(uint64) string { return "contract output" }

func (boundary *supervisionBoundary) ReadDiagnostic(ref uint64) error {
	boundary.mutex.Lock()
	defer boundary.mutex.Unlock()

	return boundary.diagnostics[ref]
}

func (boundary *supervisionBoundary) RecordDiagnostic(err error) uint64 { return boundary.record(err) }

func (boundary *supervisionBoundary) record(err error) uint64 {
	boundary.mutex.Lock()
	defer boundary.mutex.Unlock()
	boundary.nextDiagnostic++
	boundary.diagnostics[boundary.nextDiagnostic] = err

	return boundary.nextDiagnostic
}

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
	boundary := &supervisionBoundary{
		wait: make(chan struct{}), awaitedLaunch: make(chan time.Time, 1),
		diagnostics: make(map[uint64]error),
	}
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
