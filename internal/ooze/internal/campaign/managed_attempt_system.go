package campaign

import (
	"sync"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

const (
	managedLaunchProgress = time.Second
	managedDrainEpoch     = 5 * time.Second
)

type nativeManagedAttemptSystem struct {
	driver          supervision.System
	emergencyOnce   sync.Once
	emergencyResult managedObservedEmergency
}

func newNativeManagedAttemptSystem(runtime *processruntime.Runtime) (*nativeManagedAttemptSystem, error) {
	driver, err := supervision.NewNative(runtime, managedLaunchProgress, managedDrainEpoch)
	if err != nil {
		return nil, err
	}

	return &nativeManagedAttemptSystem{driver: driver}, nil
}

func newManagedAttemptSystem(driver supervision.System) *nativeManagedAttemptSystem {
	if driver == nil {
		panic("managed attempt system is required")
	}
	return &nativeManagedAttemptSystem{driver: driver}
}

func (system *nativeManagedAttemptSystem) launch(
	start processruntime.PreparedStart,
	spec supervision.Spec,
) managedObservedLaunch {
	observed := system.driver.Launch(start, spec)
	return managedObservedLaunch{result: observed.Result(), receipt: observed.Receipt()}
}

func (system *nativeManagedAttemptSystem) reserveLaunch(cell *processruntime.StartCell, spec supervision.Spec) {
	system.driver.ReserveLaunch(cell, spec)
}

func (system *nativeManagedAttemptSystem) discardLaunch(cell *processruntime.StartCell) {
	system.driver.DiscardLaunch(cell)
}

func (system *nativeManagedAttemptSystem) wait(
	generation attemptGeneration,
	owned *supervision.OwnedAttempt,
) managedObservedTerminal {
	observed := system.driver.Wait(generation, owned)
	return managedObservedTerminal{terminal: observed.Terminal(), receipt: observed.Receipt()}
}

func (system *nativeManagedAttemptSystem) stop(owned *supervision.OwnedAttempt) {
	system.driver.Stop(owned)
}

func (system *nativeManagedAttemptSystem) emergency(epoch fatalEpochID) managedObservedEmergency {
	system.emergencyOnce.Do(func() {
		at := time.Now()
		_, settlement := system.driver.EmergencyDrain(supervision.EmergencyRequest{
			At: at, DrainBy: at.Add(managedDrainEpoch),
		})
		system.emergencyResult = managedObservedEmergency{
			epoch: fatalEpochID(settlement.Epoch()), settlement: settlement,
		}
	})
	if system.emergencyResult.epoch != epoch {
		invariant("manage attempts", "managed emergency epoch is stale or wrong")
	}

	return system.emergencyResult
}
