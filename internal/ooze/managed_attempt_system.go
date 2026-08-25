package ooze

import (
	"sync"
	"time"
)

const (
	managedLaunchProgress = time.Second
	managedDrainEpoch     = 5 * time.Second
)

type nativeManagedAttemptSystem struct {
	driver          *supervisorDriver
	emergencyOnce   sync.Once
	emergencyResult managedObservedEmergency
}

func newNativeManagedAttemptSystem(runtime *processRuntimeShell) (*nativeManagedAttemptSystem, error) {
	driver, err := newNativeSupervisorDriver(runtime, managedLaunchProgress, managedDrainEpoch)
	if err != nil {
		return nil, err
	}

	return &nativeManagedAttemptSystem{driver: driver}, nil
}

func (system *nativeManagedAttemptSystem) launch(
	start installedStart,
	spec Spec,
) managedObservedLaunch {
	return system.driver.launchManaged(start, spec)
}

func (system *nativeManagedAttemptSystem) reserveLaunch(cell *pendingStartCell, spec Spec) {
	system.driver.reserveLaunch(cell, spec)
}

func (system *nativeManagedAttemptSystem) discardLaunch(cell *pendingStartCell) {
	system.driver.discardLaunch(cell)
}

func (system *nativeManagedAttemptSystem) wait(
	generation attemptGeneration,
	owned *OwnedAttempt,
) managedObservedTerminal {
	return system.driver.waitManaged(generation, owned)
}

func (system *nativeManagedAttemptSystem) stop(owned *OwnedAttempt) {
	at := system.driver.now()
	owned.Stop(StopRequest{At: at, DrainBy: at.Add(system.driver.drainEpoch)})
}

func (system *nativeManagedAttemptSystem) emergency(epoch fatalEpochID) managedObservedEmergency {
	system.emergencyOnce.Do(func() {
		at := system.driver.now()
		system.driver.emergencyDrain(EmergencyRequest{At: at, DrainBy: at.Add(system.driver.drainEpoch)})
		system.driver.mutex.Lock()
		defer system.driver.mutex.Unlock()
		if !system.driver.emergencyReady {
			invariant(supervisorDriverOperation, "managed emergency lacks exact runtime receipt")
		}
		system.emergencyResult = managedObservedEmergency{
			epoch: system.driver.emergencyReceipt.epoch, settlement: system.driver.emergencyReceipt,
		}
	})
	if system.emergencyResult.epoch != epoch {
		invariant(supervisorDriverOperation, "managed emergency epoch is stale or wrong")
	}

	return system.emergencyResult
}
