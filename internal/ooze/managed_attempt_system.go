package ooze

import "time"

const (
	managedLaunchProgress = time.Second
	managedDrainEpoch     = 5 * time.Second
)

type nativeManagedAttemptSystem struct{ driver *supervisorDriver }

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

func (system *nativeManagedAttemptSystem) wait(
	generation attemptGeneration,
	owned *OwnedAttempt,
) managedObservedTerminal {
	return system.driver.waitManaged(generation, owned)
}

func (system *nativeManagedAttemptSystem) stop(generation attemptGeneration) {
	at := system.driver.now()
	system.driver.stop(generation, StopRequest{At: at, DrainBy: at.Add(system.driver.drainEpoch)})
}

func (system *nativeManagedAttemptSystem) emergency(epoch fatalEpochID) managedObservedEmergency {
	at := system.driver.now()
	system.driver.emergencyDrain(EmergencyRequest{At: at, DrainBy: at.Add(system.driver.drainEpoch)})
	system.driver.mutex.Lock()
	defer system.driver.mutex.Unlock()
	if !system.driver.emergencyReady || system.driver.emergencyReceipt.epoch != epoch {
		invariant(supervisorDriverOperation, "managed emergency lacks exact runtime receipt")
	}

	return managedObservedEmergency{epoch: epoch, settlement: system.driver.emergencyReceipt}
}
