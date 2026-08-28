package supervision

import "github.com/gtramontina/ooze/internal/ooze/internal/processruntime"

type supervisor struct {
	installStart   func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart
	launchNative   func(attemptGeneration, Spec) LaunchResult
	reserveLaunch  func(*processruntime.StartCell, Spec)
	discardLaunch  func(*processruntime.StartCell)
	driveLaunch    func(processruntime.PreparedStart, Spec) LaunchResult
	emergencyDrain func(EmergencyRequest) SweepResult
}

func newSupervisorForTest(
	installStart func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart,
	launchNative func(attemptGeneration, Spec) LaunchResult,
) *supervisor {
	if installStart == nil || launchNative == nil {
		panic("supervisor fixture requires start and launch plumbing")
	}

	return &supervisor{installStart: installStart, launchNative: launchNative}
}

func newDrivenSupervisorForTest(
	installStart func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart,
	driver *driver,
) *supervisor {
	if driver == nil {
		panic("driven supervisor fixture requires a driver")
	}
	fixture := newSupervisorForTest(installStart, driver.launch)
	fixture.reserveLaunch = driver.reserveLaunch
	fixture.discardLaunch = driver.discardLaunch
	fixture.driveLaunch = driver.launchInstalled
	fixture.emergencyDrain = driver.emergencyDrain

	return fixture
}

func (fixture *supervisor) Launch(spec Spec) LaunchResult {
	if err := spec.validate(); err != nil {
		panic(err)
	}
	snapshot := spec.snapshot()
	cell := processruntime.NewStartCell()
	if fixture.reserveLaunch != nil {
		fixture.reserveLaunch(cell, snapshot)
	}
	start := fixture.installStart(attemptIdentity(snapshot.Attempt), cell)
	if start.Generation() == 0 && fixture.discardLaunch != nil {
		fixture.discardLaunch(cell)
	}
	if fixture.driveLaunch != nil {
		return fixture.driveLaunch(start, snapshot)
	}
	var result LaunchResult
	observed := start.Launch(func(generation processruntime.Generation) processruntime.Observation {
		result = fixture.launchNative(generation, snapshot)

		return brokerLaunchObservation(result)
	})
	start.Observe(observed)

	return result
}

func (fixture *supervisor) EmergencyDrain(request EmergencyRequest) SweepResult {
	if err := request.validate(); err != nil {
		panic(err)
	}
	if fixture.emergencyDrain == nil {
		panic("supervisor fixture emergency drain plumbing is absent")
	}

	return fixture.emergencyDrain(request)
}
