package supervision

import (
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorDriverEmergencyPreemptsCommittedStartBeforeRegistration(t *testing.T) {
	registeredAt := time.Unix(6_500, 0)
	emergencyAt := registeredAt.Add(500 * time.Millisecond)
	drainBy := emergencyAt.Add(5 * time.Second)
	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 87})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token, attempt: "committed-before-registration", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	nativeCalls := 0
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time { return registeredAt },
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			nativeCalls++
			assert.Fail(t, "pre-registered emergency executed native action", "action=%#v", action)

			return nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	spec := Spec{
		Attempt: "committed-before-registration", Command: []string{"blocked-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	}
	cell := processruntime.NewStartCell()
	driver.reserveLaunch(cell, spec)
	prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})
	closeRuntimeForTest(shell, runtimeFatalCause("committed start registration race"))

	settlement := driver.emergencyDrain(EmergencyRequest{At: emergencyAt, DrainBy: drainBy})
	_, ok := settlement.(SweepDrained)
	require.True(t, ok)
	launch := driver.launchManaged(prepared.start, spec)
	assert.Equal(t, NotReleased{Kind: LaunchFailed}, launch.result)
	assert.Zero(t, nativeCalls)
}

func TestSupervisorDriverDiscardsReservationWhenEmergencyPrecedesStartCommitment(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 86})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token, attempt: "emergency-before-start", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: func() time.Time { return time.Unix(6_000, 0) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		execute: func(action supervisorAction) *supervisorEvent {
			assert.Fail(t, "rejected start executed native action", "action=%#v", action)

			return nil
		},
		readOutput: func(supervisorOutputRef) string { return "" },
	})
	closeRuntimeForTest(shell, runtimeFatalCause("emergency before start commitment"))
	driver.emergencyStarted = true
	cell := processruntime.NewStartCell()
	spec := Spec{
		Attempt: "emergency-before-start", Command: []string{"never-start"}, Dir: "/tmp",
		Profile: SerialProfile, Deadline: 10 * time.Second,
	}
	driver.reserveLaunch(cell, spec)
	prepared := startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell})
	assert.Equal(t, processruntime.StartRejectedClosed, prepared.result.decision)
	driver.discardLaunch(cell)
	assert.Empty(t, driver.reservations)
	assert.Zero(t, cell.InstalledGeneration())
}

func TestSupervisorDriverReleasesRecorderOwnerCutBeforeNativeAction(t *testing.T) {
	recorder := newSimulationRecorder()
	fixture := newRunningReducerFixture(t, SerialProfile)
	reentered := make(chan struct{})
	returned := make(chan struct{})
	driver := &Driver{
		machine: newMachineFrom(fixture.state), observer: recorder, ownerSequence: &recorder.next,
		execute: func(supervisorAction) *supervisorEvent {
			leaveRecorder := recorder.enter()
			leaveRecorder()
			close(reentered)

			return nil
		},
	}
	eventAt := fixture.startedAt.Add(time.Second)
	event := supervisorEvent{
		kind: supervisorRunningObserved, generation: fixture.generation,
		at: eventAt, drainBy: fixture.drainBy,
		running: &supervisorRunningBundle{
			generation: fixture.generation, waitAction: fixture.waitAction,
			facts: []supervisorRunningFact{{
				generation: fixture.generation, action: fixture.waitAction,
				kind: supervisorRunningRootExited, at: eventAt,
			}},
		},
	}
	go func() {
		defer func() {
			_ = recover()
			close(returned)
		}()
		driver.applyMonitorEvent(event)
	}()

	select {
	case <-reentered:
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "native action could not enter a recorder owner cut")
	}
	<-returned
}

func TestSupervisorDriverPublishesOwnerCutsOutsideItsLock(t *testing.T) {
	observer := &blockingSupervisionObserver{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	driver := &Driver{machine: NewMachine(), observer: observer}
	done := make(chan struct{})
	go func() {
		driver.reduce(supervisorEvent{
			kind: supervisorProspectiveRegistered, generation: 1, attempt: "attempt-a",
			at: time.Unix(100, 0), launchBy: time.Unix(101, 0),
			profile: AutomaticProfile, commandDeadline: time.Minute,
		})
		close(done)
	}()
	<-observer.started
	locked := make(chan struct{})
	go func() {
		driver.mutex.Lock()
		locked <- struct{}{}
		driver.mutex.Unlock()
	}()

	select {
	case <-locked:
	case <-time.After(100 * time.Millisecond):
		close(observer.release)
		<-done
		require.Fail(t, "owner-cut observer ran while the supervisor lock was held")
	}
	close(observer.release)
	<-done
}

type blockingSupervisionObserver struct {
	started chan struct{}
	release chan struct{}
}

func (*blockingSupervisionObserver) Enter() func() { return func() {} }

func (observer *blockingSupervisionObserver) Publish(
	OwnerCutReservation,
	Fact,
	Event,
	Projection,
	[]Effect,
) {
	close(observer.started)
	<-observer.release
}

func (*blockingSupervisionObserver) Complete(Effect) {}
