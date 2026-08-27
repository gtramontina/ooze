package ooze

import (
	"sync"
	"testing"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessRuntimePublishesAcceptedLifecycleEvents(t *testing.T) {
	var mutex sync.Mutex
	var events []processruntime.Event
	observer := processruntime.ObserverFunc(func() func(processruntime.Event) {
		return func(event processruntime.Event) {
			mutex.Lock()
			events = append(events, event)
			mutex.Unlock()
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

func eventKinds(events []processruntime.Event) []processruntime.EventKind {
	kinds := make([]processruntime.EventKind, len(events))
	for index, event := range events {
		kinds[index] = event.Kind()
	}
	return kinds
}
