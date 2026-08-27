package processruntime_test

import (
	"errors"
	"sync"
	"testing"
	"time"

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

func TestRuntimePublishesAcceptedLifecycleEvents(t *testing.T) {
	var mutex sync.Mutex
	var events []processruntime.Event
	runtime := processruntime.NewObserved(1, processruntime.ObserverFunc(func(event processruntime.Event) error {
		mutex.Lock()
		events = append(events, event)
		mutex.Unlock()
		return nil
	}))

	registration := runtime.RegisterCampaign(41)
	await := runtime.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "mutant-a", Class: processruntime.SharedAdmission,
	})
	grant, received := await.Receive()
	require.True(t, received)
	start := runtime.CommitStart(grant, processruntime.NewStartCell())
	observed := start.Launch(func(processruntime.Generation) processruntime.Observation {
		return processruntime.NotReleased(false)
	})
	runtime.Observe(start.Generation(), observed)
	runtime.CommitTerminal(registration.Campaign())

	require.Len(t, events, 5)
	assert.IsType(t, processruntime.CampaignRegistrationProcessed{}, events[0])
	assert.IsType(t, processruntime.AdmissionRequestProcessed{}, events[1])
	assert.IsType(t, processruntime.StartCommitmentProcessed{}, events[2])
	assert.IsType(t, processruntime.AttemptObservationProcessed{}, events[3])
	assert.IsType(t, processruntime.TerminalCommitmentProcessed{}, events[4])
}

func TestRuntimeObserverFailuresDoNotChangeRuntimePolicy(t *testing.T) {
	tests := map[string]processruntime.Observer{
		"error": processruntime.ObserverFunc(func(processruntime.Event) error { return errors.New("observer failed") }),
		"panic": processruntime.ObserverFunc(func(processruntime.Event) error { panic("observer failed") }),
	}
	for name, observer := range tests {
		t.Run(name, func(t *testing.T) {
			runtime := processruntime.NewObserved(1, observer)
			assert.Equal(t, processruntime.CampaignRegistered, runtime.RegisterCampaign(42).Decision())
			assert.Equal(t, processruntime.CampaignRegistered, runtime.RegisterCampaign(43).Decision())
		})
	}
}

func TestRuntimeObserverMayReenterRuntime(t *testing.T) {
	var runtime *processruntime.Runtime
	reentered := make(chan processruntime.Registration, 1)
	runtime = processruntime.NewObserved(1, processruntime.ObserverFunc(func(event processruntime.Event) error {
		registered, ok := event.(processruntime.CampaignRegistrationProcessed)
		if ok && registered.Lineage() == 44 {
			reentered <- runtime.RegisterCampaign(45)
		}
		return nil
	}))

	registered := make(chan processruntime.Registration, 1)
	go func() { registered <- runtime.RegisterCampaign(44) }()
	select {
	case registration := <-registered:
		assert.Equal(t, processruntime.CampaignRegistered, registration.Decision())
	case <-time.After(time.Second):
		require.FailNow(t, "runtime registration deadlocked in observer")
	}
	select {
	case registration := <-reentered:
		assert.Equal(t, processruntime.CampaignRegistered, registration.Decision())
	case <-time.After(time.Second):
		require.FailNow(t, "reentrant runtime registration was not observed")
	}
}

func TestRuntimePublishesAcceptedEventBeforeCausalGrant(t *testing.T) {
	observing := make(chan struct{})
	release := make(chan struct{})
	runtime := processruntime.NewObserved(1, processruntime.ObserverFunc(func(event processruntime.Event) error {
		observed, ok := event.(processruntime.AttemptObservationProcessed)
		if ok && observed.Observation().Kind() == processruntime.AttemptSettled {
			close(observing)
			<-release
		}
		return nil
	}))
	activeCampaign := runtime.RegisterCampaign(46).Campaign()
	waitingCampaign := runtime.RegisterCampaign(47).Campaign()
	active := runtime.RequestAdmission(processruntime.Admission{
		Campaign: activeCampaign, Attempt: "active", Class: processruntime.SharedAdmission,
	})
	waiting := runtime.RequestAdmission(processruntime.Admission{
		Campaign: waitingCampaign, Attempt: "waiting", Class: processruntime.SharedAdmission,
	})
	activeGrant, received := active.Receive()
	require.True(t, received)
	start := runtime.CommitStart(activeGrant, processruntime.NewStartCell())
	owned := start.Launch(func(processruntime.Generation) processruntime.Observation { return processruntime.Owned() })
	runtime.Observe(start.Generation(), owned)

	settled := make(chan processruntime.Receipt, 1)
	go func() { settled <- runtime.Observe(start.Generation(), processruntime.Settled(0, 0)) }()
	<-observing
	receivedWaiting := make(chan struct{})
	go func() {
		waiting.Receive()
		close(receivedWaiting)
	}()
	select {
	case <-receivedWaiting:
		require.FailNow(t, "admission grant arrived before its accepted runtime event")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-settled
	select {
	case <-receivedWaiting:
	case <-time.After(time.Second):
		require.FailNow(t, "admission grant was not delivered after event publication")
	}
}

func TestReplayFoldsProductionEventsIntoCapabilityFreeProjections(t *testing.T) {
	replay := processruntime.NewReplay(1)
	runtime := processruntime.NewObserved(1, processruntime.ObserverFunc(func(event processruntime.Event) error {
		var matches bool
		replay, matches = replay.ApplyEvent(event)
		require.True(t, matches)
		return nil
	}))

	registration := runtime.RegisterCampaign(48)
	await := runtime.RequestAdmission(processruntime.Admission{
		Campaign: registration.Campaign(), Attempt: "mutant-a", Class: processruntime.SharedAdmission,
	})
	grant, received := await.Receive()
	require.True(t, received)
	start := runtime.CommitStart(grant, processruntime.NewStartCell())
	runtime.Observe(start.Generation(), start.Launch(func(processruntime.Generation) processruntime.Observation {
		return processruntime.Owned()
	}))

	t.Run("owned generation", func(t *testing.T) {
		assert.True(t, replay.Projection().Owned(start.Generation()))
		assert.True(t, replay.CanObserveOwnedTerminal(start.Generation()))
		assert.False(t, replay.CanObserveNotReleased(start.Generation()))
	})

	runtime.Observe(start.Generation(), processruntime.Settled(processruntime.AutomaticProfile, 0))
	runtime.CommitTerminal(registration.Campaign())

	t.Run("terminal state", func(t *testing.T) {
		assert.True(t, replay.Projection().Open())
		assert.Empty(t, replay.Residual())
		assert.Zero(t, replay.Projection().CampaignCount())
	})
}

func TestRuntimeFatalClosureProgressesWhileLaunchIsBlocked(t *testing.T) {
	runtime, start := preparedRuntimeStart(t, 49)
	entered := make(chan struct{})
	release := make(chan struct{})
	observed := make(chan processruntime.Observation, 1)
	go func() {
		observed <- start.Launch(func(processruntime.Generation) processruntime.Observation {
			close(entered)
			<-release
			return processruntime.NotReleased(false)
		})
	}()
	<-entered
	closed := make(chan processruntime.Closure, 1)
	go func() { closed <- runtime.Close("fatal while launch blocked") }()

	select {
	case closure := <-closed:
		assert.NotZero(t, closure.Epoch())
	case <-time.After(time.Second):
		require.FailNow(t, "fatal closure waited for dormant launch")
	}
	close(release)
	runtime.Observe(start.Generation(), <-observed)
	assert.True(t, runtime.Projection().Closing())
}

func TestRuntimeLaunchFailureRetainsOneFatalCause(t *testing.T) {
	runtime, start := preparedRuntimeStart(t, 50)

	assert.Panics(t, func() {
		start.Launch(func(processruntime.Generation) processruntime.Observation {
			panic("native launch failed")
		})
	})
	projection := runtime.Projection()
	assert.NotZero(t, projection.FatalEpoch())
	assert.EqualValues(t, 1, projection.FatalCauseCount())
}

func TestRuntimeDeliversEachGrantOnce(t *testing.T) {
	runtime, start := preparedRuntimeStart(t, 51)
	waitingCampaign := runtime.RegisterCampaign(52).Campaign()
	waiting := runtime.RequestAdmission(processruntime.Admission{
		Campaign: waitingCampaign, Attempt: "waiting", Class: processruntime.SharedAdmission,
	})
	runtime.Observe(start.Generation(), start.Launch(func(processruntime.Generation) processruntime.Observation {
		return processruntime.Owned()
	}))
	runtime.Observe(start.Generation(), processruntime.Settled(processruntime.AutomaticProfile, 0))

	_, received := waiting.Receive()
	assert.True(t, received)
	_, received = waiting.Receive()
	assert.False(t, received)
}

func TestRuntimeSerializesOwnedSettlementAgainstFatalClosure(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		runtime, start := preparedRuntimeStart(t, processruntime.Lineage(100+iteration))
		runtime.Observe(start.Generation(), start.Launch(func(processruntime.Generation) processruntime.Observation {
			return processruntime.Owned()
		}))
		begin := make(chan struct{})
		panics := make(chan any, 2)
		var wait sync.WaitGroup
		wait.Add(2)
		go runRuntimeRace(&wait, begin, panics, func() {
			runtime.Observe(start.Generation(), processruntime.Settled(processruntime.AutomaticProfile, 0))
		})
		go runRuntimeRace(&wait, begin, panics, func() { runtime.Close("concurrent fatal") })
		close(begin)
		wait.Wait()
		close(panics)
		assert.Empty(t, panics)
		projection := runtime.Projection()
		assert.True(t, projection.Closing() || projection.Drained())
	}
}

func preparedRuntimeStart(t *testing.T, lineage processruntime.Lineage) (*processruntime.Runtime, processruntime.PreparedStart) {
	t.Helper()
	runtime := processruntime.New(1)
	campaign := runtime.RegisterCampaign(lineage).Campaign()
	await := runtime.RequestAdmission(processruntime.Admission{
		Campaign: campaign, Attempt: "active", Class: processruntime.SharedAdmission,
	})
	grant, received := await.Receive()
	require.True(t, received)
	return runtime, runtime.CommitStart(grant, processruntime.NewStartCell())
}

func runRuntimeRace(wait *sync.WaitGroup, begin <-chan struct{}, panics chan<- any, action func()) {
	defer wait.Done()
	defer func() {
		if recovered := recover(); recovered != nil {
			panics <- recovered
		}
	}()
	<-begin
	action()
}
