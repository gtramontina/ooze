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
	observer := processruntime.ObserverFunc(func() func(processruntime.Event) error {
		return func(event processruntime.Event) error {
			mutex.Lock()
			events = append(events, event)
			mutex.Unlock()

			return nil
		}
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
	assert.Equal(t, []processruntime.EventKind{
		processruntime.RegisterCampaign,
		processruntime.RequestAdmission,
		processruntime.StartCommitted,
		processruntime.ObserveAttempt,
		processruntime.CommitTerminal,
	}, eventKinds(events))
	assert.EqualValues(t, 41, events[0].Command().Lineage)
	assert.EqualValues(t, registration.token.id, events[0].Result().Registration.Campaign.ID)
	assert.Equal(t, "mutant-a", events[1].Command().Admission.Attempt)
	assert.EqualValues(t, start.result.generation, events[2].Result().Start.Generation)
	assert.EqualValues(t, launchFailed, events[3].Command().Observation.Reason)
	assert.EqualValues(t, terminal.decision, events[4].Result().Terminal.Decision)
}

func TestProcessRuntimeObserverFailureDoesNotCloseRuntime(t *testing.T) {
	tests := map[string]processruntime.Observer{
		"reservation panic": processruntime.ObserverFunc(func() func(processruntime.Event) error {
			panic("observer reservation failed")
		}),
		"delivery panic": processruntime.ObserverFunc(func() func(processruntime.Event) error {
			return func(processruntime.Event) error { panic("observer delivery failed") }
		}),
		"delivery error": processruntime.ObserverFunc(func() func(processruntime.Event) error {
			return func(processruntime.Event) error { return errors.New("observer delivery failed") }
		}),
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
	var kinds []processruntime.EventKind
	observer := processruntime.ObserverFunc(func() func(processruntime.Event) error {
		return func(event processruntime.Event) error {
			kinds = append(kinds, event.Kind())
			if event.Kind() == processruntime.RegisterCampaign && event.Command().Lineage == 44 {
				reentered <- runtime.registerCampaign(campaignProvenance{lineage: 45})
			}

			return nil
		}
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
	assert.Equal(t, []processruntime.EventKind{
		processruntime.RegisterCampaign,
		processruntime.RegisterCampaign,
	}, kinds)
}

func TestProcessRuntimePublishesAcceptedEventBeforeCausalNotification(t *testing.T) {
	observing := make(chan struct{})
	release := make(chan struct{})
	observer := processruntime.ObserverFunc(func() func(processruntime.Event) error {
		return func(event processruntime.Event) error {
			if event.Kind() == processruntime.ObserveAttempt &&
				event.Command().Observation.Kind == processruntime.AttemptSettled {
				close(observing)
				<-release
			}

			return nil
		}
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

func eventKinds(events []processruntime.Event) []processruntime.EventKind {
	kinds := make([]processruntime.EventKind, len(events))
	for index, event := range events {
		kinds[index] = event.Kind()
	}
	return kinds
}
