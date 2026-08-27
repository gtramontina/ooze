package ooze

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessRuntimePublishesAcceptedLifecycleEvents(t *testing.T) {
	var mutex sync.Mutex
	var events []processRuntimeEvent
	observer := processRuntimeObserverFunc(func(event processRuntimeEvent) error {
		mutex.Lock()
		events = append(events, event)
		mutex.Unlock()

		return nil
	})
	runtime := newProcessRuntimeShellWithObserver(1, observer)

	registration := runtime.registerCampaign(campaignProvenance{lineage: 41})
	await := runtime.requestAdmission(admissionRequest{
		campaign: registration.token,
		attempt:  "mutant-a",
		class:    sharedAdmission,
	})
	grant := <-await.delivery
	start := runtime.startCommitted(grant, startInstallation{grant: grant, cell: &pendingStartCell{}})
	runtime.observeAttempt(start.result.generation, launchNotReleased{reason: launchFailed})
	terminal := runtime.commitTerminal(registration.token)

	require.EqualValues(t, 5, len(events))
	assert.Equal(t, []string{
		"campaign registered", "admission requested", "attempt start committed", "attempt observed", "terminal committed",
	}, eventNames(events))
	registered := events[0].(runtimeCampaignRegistrationProcessed)
	requested := events[1].(runtimeAdmissionRequestProcessed)
	committed := events[2].(runtimeStartCommitmentProcessed)
	observed := events[3].(runtimeAttemptObservationProcessed)
	committedTerminal := events[4].(runtimeTerminalCommitmentProcessed)
	assert.EqualValues(t, 41, registered.provenance.lineage)
	assert.Equal(t, registration.token, registered.result.token)
	assert.EqualValues(t, "mutant-a", requested.request.attempt)
	assert.Equal(t, start.result, committed.result)
	assert.Equal(t, launchFailed, observed.observation.(launchNotReleased).reason)
	assert.Equal(t, terminal, committedTerminal.result)
}

func TestProcessRuntimeObserverFailureDoesNotCloseRuntime(t *testing.T) {
	tests := map[string]processRuntimeObserver{
		"panic": processRuntimeObserverFunc(func(processRuntimeEvent) error { panic("observer failed") }),
		"error": processRuntimeObserverFunc(func(processRuntimeEvent) error { return errors.New("observer failed") }),
	}
	for name, observer := range tests {
		t.Run(name, func(t *testing.T) {
			runtime := newProcessRuntimeShellWithObserver(1, observer)

			first := runtime.registerCampaign(campaignProvenance{lineage: 42})
			registration := runtime.registerCampaign(campaignProvenance{lineage: 43})

			assert.Equal(t, campaignRegistered, first.decision)
			assert.Equal(t, campaignRegistered, registration.decision)
		})
	}
}

func TestProcessRuntimeObserverMayReenterRuntime(t *testing.T) {
	var runtime *processRuntimeShell
	reentered := make(chan campaignRegistration, 1)
	var names []string
	observer := processRuntimeObserverFunc(func(event processRuntimeEvent) error {
		names = append(names, eventName(event))
		registered, ok := event.(runtimeCampaignRegistrationProcessed)
		if ok && registered.provenance.lineage == 44 {
			reentered <- runtime.registerCampaign(campaignProvenance{lineage: 45})
		}

		return nil
	})
	runtime = newProcessRuntimeShellWithObserver(1, observer)

	registered := make(chan campaignRegistration, 1)
	go func() {
		registered <- runtime.registerCampaign(campaignProvenance{lineage: 44})
	}()

	select {
	case registration := <-registered:
		assert.Equal(t, campaignRegistered, registration.decision)
	case <-time.After(time.Second):
		t.Fatal("runtime registration deadlocked in observer")
	}
	select {
	case registration := <-reentered:
		assert.Equal(t, campaignRegistered, registration.decision)
	case <-time.After(time.Second):
		t.Fatal("reentrant runtime registration was not observed")
	}
	assert.Equal(t, []string{"campaign registered", "campaign registered"}, names)
}

func TestProcessRuntimePublishesAcceptedEventBeforeCausalNotification(t *testing.T) {
	observing := make(chan struct{})
	release := make(chan struct{})
	observer := processRuntimeObserverFunc(func(event processRuntimeEvent) error {
		observed, ok := event.(runtimeAttemptObservationProcessed)
		_, settled := observed.observation.(attemptSettled)
		if ok && settled {
			close(observing)
			<-release
		}

		return nil
	})
	runtime := newProcessRuntimeShellWithObserver(1, observer)
	activeCampaign := runtime.registerCampaign(campaignProvenance{lineage: 46})
	waitingCampaign := runtime.registerCampaign(campaignProvenance{lineage: 47})
	active := runtime.requestAdmission(admissionRequest{
		campaign: activeCampaign.token, attempt: "active", class: sharedAdmission,
	})
	waiting := runtime.requestAdmission(admissionRequest{
		campaign: waitingCampaign.token, attempt: "waiting", class: sharedAdmission,
	})
	require.Equal(t, admissionAccepted, waiting.decision)
	started := startOwned(runtime, <-active.delivery)

	settled := make(chan observationResult, 1)
	go func() {
		settled <- runtime.observeAttempt(started.generation, attemptSettled{})
	}()
	<-observing
	select {
	case grant, open := <-waiting.delivery:
		t.Fatalf("admission notification arrived before its accepted runtime event: %#v/%t", grant, open)
	default:
	}
	close(release)
	<-settled
	grant, open := <-waiting.delivery
	assert.True(t, open)
	assert.EqualValues(t, "waiting", grant.attempt)
}

func TestSimulationRuntimeObserverReleasesRecorderGateAfterDivergence(t *testing.T) {
	recorder := newSimulationRecorder()
	observer := newSimulationRuntimeObserver(recorder, 1)
	admission := admissionRequest{
		campaign: campaignToken{id: 1, lineage: 48}, attempt: "unknown-campaign", class: sharedAdmission,
	}
	event := runtimeAdmissionRequestProcessed{
		request: admission,
		result:  admissionResult{decision: admissionAccepted, request: admission},
	}

	assert.Error(t, observer.Observe(event))
	acquired := make(chan struct{})
	go func() {
		recorder.gate.Lock()
		close(acquired)
		recorder.gate.Unlock()
	}()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("simulation observer leaked its recorder gate after divergence")
	}
}

func eventNames(events []processRuntimeEvent) []string {
	names := make([]string, len(events))
	for index, event := range events {
		names[index] = eventName(event)
	}
	return names
}

func eventName(event processRuntimeEvent) string {
	switch event.(type) {
	case runtimeCampaignRegistrationProcessed:
		return "campaign registered"
	case runtimeAdmissionRequestProcessed:
		return "admission requested"
	case runtimeStartCommitmentProcessed:
		return "attempt start committed"
	case runtimeAttemptObservationProcessed:
		return "attempt observed"
	case runtimeTerminalCommitmentProcessed:
		return "terminal committed"
	default:
		return "unknown"
	}
}
