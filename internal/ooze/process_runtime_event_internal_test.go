package ooze

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessRuntimePublishesAcceptedLifecycleEvents(t *testing.T) {
	var mutex sync.Mutex
	var events []processruntime.Event
	observer := processruntime.ObserverFunc(func(event processruntime.Event) error {
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
	registered := events[0].Variant().(processruntime.CampaignRegistrationProcessed)
	requested := events[1].Variant().(processruntime.AdmissionRequestProcessed)
	committed := events[2].Variant().(processruntime.StartCommitmentProcessed)
	observed := events[3].Variant().(processruntime.AttemptObservationProcessed)
	committedTerminal := events[4].Variant().(processruntime.TerminalCommitmentProcessed)
	assert.EqualValues(t, 41, registered.Lineage())
	assert.EqualValues(t, registration.token.id, registered.Registration().Campaign.ID)
	assert.Equal(t, "mutant-a", requested.Admission().Attempt)
	assert.EqualValues(t, start.result.generation, committed.Result().Generation)
	assert.EqualValues(t, launchFailed, observed.Observation().Reason)
	assert.EqualValues(t, terminal.decision, committedTerminal.Result().Decision)
}

func TestProcessRuntimeObserverFailureDoesNotCloseRuntime(t *testing.T) {
	tests := map[string]processruntime.Observer{
		"panic": processruntime.ObserverFunc(func(processruntime.Event) error { panic("observer failed") }),
		"error": processruntime.ObserverFunc(func(processruntime.Event) error { return errors.New("observer failed") }),
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
	observer := processruntime.ObserverFunc(func(event processruntime.Event) error {
		names = append(names, eventName(event))
		registered, ok := event.Variant().(processruntime.CampaignRegistrationProcessed)
		if ok && registered.Lineage() == 44 {
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
	observer := processruntime.ObserverFunc(func(event processruntime.Event) error {
		observed, ok := event.Variant().(processruntime.AttemptObservationProcessed)
		if ok && observed.Observation().Kind == processruntime.AttemptSettled {
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
	admission := processruntime.Admission{
		Campaign: processruntime.Campaign{ID: 1, Lineage: 48}, Attempt: "unknown-campaign",
		Class: processruntime.SharedAdmission, Profile: processruntime.UnspecifiedProfile,
	}
	event, err := processruntime.NewAdmissionRequestProcessed(admission, processruntime.AdmissionResult{
		Decision: processruntime.AdmissionAccepted, Request: admission,
	})
	require.NoError(t, err)

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

func eventNames(events []processruntime.Event) []string {
	names := make([]string, len(events))
	for index, event := range events {
		names[index] = eventName(event)
	}
	return names
}

func eventName(event processruntime.Event) string {
	switch event.Variant().(type) {
	case processruntime.CampaignRegistrationProcessed:
		return "campaign registered"
	case processruntime.AdmissionRequestProcessed:
		return "admission requested"
	case processruntime.StartCommitmentProcessed:
		return "attempt start committed"
	case processruntime.AttemptObservationProcessed:
		return "attempt observed"
	case processruntime.TerminalCommitmentProcessed:
		return "terminal committed"
	default:
		return "unknown"
	}
}
