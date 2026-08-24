package ooze

import (
	"sync"
	"time"
)

const supervisorDriverOperation = "drive supervisor"

const nominalSupervisorFuseCadence = 50 * time.Millisecond

type supervisorDriverConstruction struct {
	runtime        *processRuntimeShell
	now            func() time.Time
	launchProgress time.Duration
	drainEpoch     time.Duration
	prepare        func(attemptGeneration, Spec)
	execute        func(supervisorAction) *supervisorEvent
	recheckRoot    func(attemptGeneration) (ExitStatus, bool, error)
	sampleRunning  func(attemptGeneration) (bool, uint64, error)
	readOutput     func(supervisorOutputRef) string
}

type supervisorDrivenAttempt struct {
	spec          Spec
	owned         *OwnedAttempt
	launchResult  LaunchResult
	waitAction    supervisorAction
	sampleAction  supervisorAction
	waitStarted   bool
	terminalReady bool
	terminal      chan Terminal
}

type supervisorDriver struct {
	mutex            sync.Mutex
	state            supervisorState
	runtime          *processRuntimeShell
	now              func() time.Time
	launchProgress   time.Duration
	drainEpoch       time.Duration
	prepare          func(attemptGeneration, Spec)
	execute          func(supervisorAction) *supervisorEvent
	recheckRoot      func(attemptGeneration) (ExitStatus, bool, error)
	sampleRunning    func(attemptGeneration) (bool, uint64, error)
	readOutput       func(supervisorOutputRef) string
	attempts         map[attemptGeneration]*supervisorDrivenAttempt
	emergency        chan SweepResult
	emergencyStarted bool
}

func newSupervisorDriver(construction supervisorDriverConstruction) *supervisorDriver {
	if construction.runtime == nil || construction.now == nil || construction.launchProgress <= 0 ||
		construction.drainEpoch <= 0 || construction.execute == nil || construction.readOutput == nil {
		panic("supervisor driver construction is incomplete")
	}

	return &supervisorDriver{
		runtime: construction.runtime, now: construction.now,
		launchProgress: construction.launchProgress, drainEpoch: construction.drainEpoch,
		prepare: construction.prepare, execute: construction.execute,
		recheckRoot: construction.recheckRoot, sampleRunning: construction.sampleRunning,
		readOutput: construction.readOutput,
		attempts:   make(map[attemptGeneration]*supervisorDrivenAttempt),
		emergency:  make(chan SweepResult, 1),
	}
}

func newDrivenSupervisorForTest(
	installStart func(attemptIdentity, *pendingStartCell) installedStart,
	driver *supervisorDriver,
) *Supervisor {
	if driver == nil {
		panic("driven supervisor requires a driver")
	}
	supervisor := newSupervisorForTest(installStart, driver.launch)
	supervisor.driveLaunch = driver.launchInstalled
	supervisor.emergencyDrain = driver.emergencyDrain

	return supervisor
}

func (driver *supervisorDriver) launchInstalled(start installedStart, spec Spec) LaunchResult {
	var actions []supervisorAction
	var launchObservation attemptObservation
	observed := start.launch(func(generation attemptGeneration) attemptObservation {
		launchObservation, actions = driver.stageLaunch(generation, spec)

		return launchObservation
	})
	start.shell.observeAttempt(start.generation, observed)
	for _, action := range actions {
		driver.run(action)
	}
	driver.mutex.Lock()
	published := driver.requireAttempt(start.generation).launchResult
	driver.mutex.Unlock()
	if launchObservation == nil || published == nil {
		invariant(supervisorDriverOperation, "launch returned before publication")
	}

	return published
}

func (driver *supervisorDriver) stageLaunch(
	generation attemptGeneration,
	spec Spec,
) (attemptObservation, []supervisorAction) {
	registeredAt := driver.now()
	driver.mutex.Lock()
	if generation == 0 || driver.attempts[generation] != nil {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "launch generation is zero or duplicated")
	}
	driver.attempts[generation] = &supervisorDrivenAttempt{
		spec: spec, terminal: make(chan Terminal, 1),
	}
	driver.mutex.Unlock()
	if driver.prepare != nil {
		driver.prepare(generation, spec)
	}
	actions := driver.reduce(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: generation,
		attempt: attemptIdentity(spec.Attempt), at: registeredAt,
		launchBy: registeredAt.Add(driver.launchProgress), profile: spec.Profile,
		commandDeadline: spec.Deadline,
	})
	if len(actions) != 1 || actions[0].kind != supervisorLaunchNative {
		invariant(supervisorDriverOperation, "prospective registration did not issue native launch")
	}
	event := driver.execute(actions[0])
	if event == nil {
		invariant(supervisorDriverOperation, "native launch returned no completion")
	}
	publication := driver.reduce(*event)
	completion := event.completion
	if completion == nil {
		invariant(supervisorDriverOperation, "native launch completion is absent")
	}
	var observation attemptObservation
	switch completion.kind {
	case supervisorLaunchReleased:
		observation = launchOwned{}
	case supervisorLaunchProvenNotReleased:
		switch completion.failure {
		case LaunchFailed:
			observation = launchNotReleased{reason: launchFailed}
		case LaunchResourceExhausted:
			observation = launchNotReleased{reason: launchResourceExhausted}
		default:
			invariant(supervisorDriverOperation, "not-released launch classification is invalid")
		}
	default:
		invariant(supervisorDriverOperation, "native launch completion kind is invalid")
	}

	return observation, publication
}

func (driver *supervisorDriver) launch(generation attemptGeneration, spec Spec) LaunchResult {
	registeredAt := driver.now()
	driver.mutex.Lock()
	if generation == 0 || driver.attempts[generation] != nil {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "launch generation is zero or duplicated")
	}
	driver.attempts[generation] = &supervisorDrivenAttempt{
		spec: spec, terminal: make(chan Terminal, 1),
	}
	driver.mutex.Unlock()
	driver.apply(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: generation,
		attempt: attemptIdentity(spec.Attempt), at: registeredAt,
		launchBy: registeredAt.Add(driver.launchProgress), profile: spec.Profile,
		commandDeadline: spec.Deadline,
	})

	driver.mutex.Lock()
	result := driver.attempts[generation].launchResult
	driver.mutex.Unlock()
	if result == nil {
		invariant(supervisorDriverOperation, "launch returned before publication")
	}

	return result
}

func (driver *supervisorDriver) reduce(event supervisorEvent) []supervisorAction {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	next, actions := reduceSupervisor(driver.state, event)
	driver.state = next

	return actions
}

func (driver *supervisorDriver) apply(event supervisorEvent) {
	actions := driver.reduce(event)
	for _, action := range actions {
		driver.run(action)
	}
}

func (driver *supervisorDriver) run(action supervisorAction) {
	switch action.kind {
	case supervisorPublishOwned:
		driver.publishOwned(action)
	case supervisorPublishNotReleased:
		driver.publishNotReleased(action)
	case supervisorWaitRoot:
		driver.rememberMonitor(action, false)
	case supervisorSampleRunning:
		driver.rememberMonitor(action, true)
	case supervisorSealStopAdmission:
		driver.sealStopAdmission(action)
	case supervisorSettleRuntime:
		driver.settleRuntime(action)
	case supervisorDeliverTerminal:
		driver.deliverTerminal(action)
	case supervisorSettleEmergency, supervisorDeliverEmergencySettlement:
		driver.executeEmergency(action)
	case supervisorLaunchNative, supervisorObserveEmptiness, supervisorForceOwned,
		supervisorCaptureOutput, supervisorReleaseDomain:
		driver.executeAction(action)
	default:
		invariant(supervisorDriverOperation, "action kind is not implemented by the driver")
	}
}

func (driver *supervisorDriver) executeAction(action supervisorAction) {
	event := driver.execute(action)
	if event == nil {
		invariant(supervisorDriverOperation, "native action returned no completion")
	}
	driver.apply(*event)
}

func (driver *supervisorDriver) publishOwned(action supervisorAction) {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(action.generation)
	if attempt.owned != nil || attempt.launchResult != nil {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "owned launch was published twice")
	}
	attempt.owned = newOwnedAttempt(
		func(request StopRequest) { driver.stop(action.generation, request) },
		func() Terminal { return driver.wait(action.generation) },
	)
	attempt.launchResult = Owned{Attempt: attempt.owned}
	driver.mutex.Unlock()
}

func (driver *supervisorDriver) publishNotReleased(action supervisorAction) {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	attempt := driver.requireAttempt(action.generation)
	if attempt.launchResult != nil {
		invariant(supervisorDriverOperation, "not-released launch was published twice")
	}
	attempt.launchResult = NotReleased{Kind: action.launchFailure}
}

func (driver *supervisorDriver) rememberMonitor(action supervisorAction, sample bool) {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	attempt := driver.requireAttempt(action.generation)
	if sample {
		if attempt.sampleAction.token != 0 {
			invariant(supervisorDriverOperation, "running sampler action was duplicated")
		}
		attempt.sampleAction = action

		return
	}
	if attempt.waitAction.token != 0 {
		invariant(supervisorDriverOperation, "root waiter action was duplicated")
	}
	attempt.waitAction = action
}

func (driver *supervisorDriver) wait(generation attemptGeneration) Terminal {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(generation)
	if attempt.waitStarted || attempt.waitAction.token == 0 {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "owned wait action is absent or duplicated")
	}
	attempt.waitStarted = true
	waitAction := attempt.waitAction
	terminal := attempt.terminal
	terminalReady := attempt.terminalReady
	var deadlineAt time.Time
	monitorRequired := false
	if !terminalReady {
		state := driver.state.attempts[driver.state.requireAttempt(generation)]
		deadlineAt = state.deadlineAt
		monitorRequired = state.phase == supervisorRunning
	}
	sampleAction := attempt.sampleAction
	driver.mutex.Unlock()
	if !terminalReady && monitorRequired {
		if driver.recheckRoot == nil {
			driver.executeAction(waitAction)
		} else {
			driver.waitThroughDeadline(waitAction, sampleAction, deadlineAt)
		}
	}

	return <-terminal
}

func (driver *supervisorDriver) waitThroughDeadline(
	waitAction supervisorAction,
	sampleAction supervisorAction,
	deadlineAt time.Time,
) {
	waited := make(chan *supervisorEvent, 1)
	go func() { waited <- driver.execute(waitAction) }()
	timer := time.NewTimer(time.Until(deadlineAt))
	defer timer.Stop()
	var samples <-chan time.Time
	var ticker *time.Ticker
	if sampleAction.token != 0 {
		if driver.sampleRunning == nil {
			invariant(supervisorDriverOperation, "automatic wait lacks a running sampler")
		}
		ticker = time.NewTicker(nominalSupervisorFuseCadence)
		defer ticker.Stop()
		samples = ticker.C
	}
	for {
		select {
		case event := <-waited:
			if event == nil {
				invariant(supervisorDriverOperation, "root wait returned no completion")
			}
			driver.apply(*event)

			return
		case <-timer.C:
			status, observed, err := driver.recheckRoot(waitAction.generation)
			if err != nil {
				invariant(supervisorDriverOperation, "deadline root recheck failed")
			}
			recheck := supervisorExitRecheck{performed: true, observed: observed, at: deadlineAt}
			if observed {
				recheck.code = status.Code
				recheck.signal = status.Signal
			}
			driver.apply(supervisorEvent{
				kind: supervisorRunningObserved, generation: waitAction.generation,
				at: deadlineAt, drainBy: deadlineAt.Add(driver.drainEpoch),
				running: &supervisorRunningBundle{
					generation: waitAction.generation,
					waitAction: waitAction.token, sampleAction: sampleAction.token,
					exitRecheck: recheck,
				},
			})

			return
		case at := <-samples:
			if !at.Before(deadlineAt) {
				continue
			}
			rootLive, live, err := driver.sampleRunning(waitAction.generation)
			var facts []supervisorRunningFact
			if err != nil {
				facts = []supervisorRunningFact{{
					generation: waitAction.generation, action: sampleAction.token,
					kind: supervisorRunningObservationFailed, at: at,
					source: supervisorObservationRunning, diagnostic: 1,
				}}
			} else if rootLive && live != 0 {
				facts = []supervisorRunningFact{{
					generation: waitAction.generation, action: sampleAction.token,
					kind: supervisorRunningFuseObserved, at: at,
					rootLive: true, live: live,
				}}
			}
			if len(facts) == 0 {
				continue
			}
			driver.apply(supervisorEvent{
				kind: supervisorRunningObserved, generation: waitAction.generation,
				at: at, drainBy: at.Add(driver.drainEpoch),
				running: &supervisorRunningBundle{
					generation: waitAction.generation,
					waitAction: waitAction.token, sampleAction: sampleAction.token,
					facts: facts,
				},
			})
			driver.mutex.Lock()
			terminalReady := driver.requireAttempt(waitAction.generation).terminalReady
			driver.mutex.Unlock()
			if terminalReady {
				return
			}
		}
	}
}

func (driver *supervisorDriver) stop(generation attemptGeneration, request StopRequest) {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(generation)
	bundle := supervisorRunningBundle{
		generation: generation, waitAction: attempt.waitAction.token,
		sampleAction: attempt.sampleAction.token,
		facts: []supervisorRunningFact{{
			generation: generation, kind: supervisorRunningStopRequested,
			at: request.At, stop: request,
		}},
	}
	driver.mutex.Unlock()
	actions := driver.reduce(supervisorEvent{
		kind: supervisorRunningObserved, generation: generation,
		at: request.At, drainBy: request.DrainBy, running: &bundle,
	})
	if len(actions) != 0 {
		go func() {
			for _, action := range actions {
				driver.run(action)
			}
		}()
	}
}

func (driver *supervisorDriver) sealStopAdmission(action supervisorAction) {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(action.generation)
	owned := attempt.owned
	driver.mutex.Unlock()
	if owned == nil {
		invariant(supervisorDriverOperation, "stop seal has no owned capability")
	}
	owned.sealStopAdmission()
	at := driver.now()
	completion := supervisorStopSealCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at,
	}
	driver.apply(supervisorEvent{
		kind: supervisorStopAdmissionSealed, generation: action.generation,
		at: at, seal: &completion,
	})
}

func (driver *supervisorDriver) settleRuntime(action supervisorAction) {
	receipt := driver.runtime.observeAttempt(action.generation, terminalObservation(action.terminal))
	kind := supervisorRuntimeClosurePending
	if receipt.settlementAcknowledged {
		kind = supervisorRuntimeAcknowledged
		if receipt.confirmationProvisional {
			kind = supervisorRuntimeProvisionalDeadline
		}
	} else if !receipt.runtimeClosureInProgress {
		invariant(supervisorDriverOperation, "runtime returned no terminal disposition")
	}
	completion := supervisorRuntimeCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		kind:       kind,
	}
	driver.apply(supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: action.generation,
		runtime: &completion,
	})
}

func terminalObservation(evidence supervisorTerminalEvidence) attemptObservation {
	switch evidence.kind {
	case supervisorTerminalSettled:
		return attemptSettled{}
	case supervisorTerminalFuseTrip:
		return attemptTripped{kind: fuseTrip}
	case supervisorTerminalAutomaticDeadlineTrip, supervisorTerminalSerialDeadlineTrip:
		return attemptTripped{kind: deadlineTrip}
	case supervisorTerminalStopped:
		return attemptStopped{}
	case supervisorTerminalInfrastructureWait, supervisorTerminalInfrastructureRunning,
		supervisorTerminalInfrastructureRelease, supervisorTerminalInfrastructureOutput,
		supervisorTerminalInfrastructureControl:
		return attemptInfrastructure{cause: "managed attempt supervision failed"}
	default:
		invariant(supervisorDriverOperation, "terminal cannot settle runtime")

		return nil
	}
}

func (driver *supervisorDriver) deliverTerminal(action supervisorAction) {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(action.generation)
	terminal := publicTerminal(action.terminal, driver.readOutput)
	delivery := attempt.terminal
	if attempt.terminalReady {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "terminal delivery was duplicated")
	}
	attempt.terminalReady = true
	driver.mutex.Unlock()
	delivery <- terminal
}

func (driver *supervisorDriver) emergencyDrain(request EmergencyRequest) SweepResult {
	driver.mutex.Lock()
	if driver.emergencyStarted {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "emergency drain was duplicated")
	}
	driver.emergencyStarted = true
	snapshots := make([]supervisorEmergencySnapshot, 0, len(driver.state.attempts))
	for _, state := range driver.state.attempts {
		if state.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		snapshot := supervisorEmergencySnapshot{generation: state.generation}
		switch state.phase {
		case supervisorRunning:
			snapshot.running = &supervisorRunningBundle{
				generation:   state.generation,
				waitAction:   state.waitAction,
				sampleAction: state.sampleAction,
			}
		default:
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "attempt phase has no emergency snapshot")
		}
		snapshots = append(snapshots, snapshot)
	}
	driver.mutex.Unlock()
	driver.apply(supervisorEvent{
		kind: supervisorEmergencyStarted, at: request.At, drainBy: request.DrainBy,
		emergencySnapshots: snapshots,
	})

	return <-driver.emergency
}

func (driver *supervisorDriver) executeEmergency(action supervisorAction) {
	driver.mutex.Lock()
	state := cloneSupervisorState(driver.state)
	driver.mutex.Unlock()
	executor := supervisorEmergencyExecutor{
		settleEmergency:            driver.runtime.settleEmergency,
		deliverEmergencySettlement: driver.deliverEmergencySettlement,
	}
	event := executor.execute(state, action)
	if event != nil {
		driver.apply(*event)
	}
}

func (driver *supervisorDriver) deliverEmergencySettlement(residuals []supervisorEmergencyResidual) {
	if len(residuals) == 0 {
		driver.emergency <- SweepDrained{}

		return
	}
	public := make([]ResidualRef, len(residuals))
	for index, residual := range residuals {
		public[index] = ResidualRef{Attempt: string(residual.attempt), Kind: OwnedUndrained}
	}
	driver.emergency <- SweepUnconfirmed{residuals: public}
}

func publicTerminal(
	evidence supervisorTerminalEvidence,
	readOutput func(supervisorOutputRef) string,
) Terminal {
	data := ExecutionData{
		Deadline: evidence.commandDeadline, LaunchDuration: evidence.launchDuration,
		CommandDuration: evidence.commandDuration,
		Output: OutputSnapshot{
			Bytes: readOutput(evidence.output.ref), Cutoff: evidence.output.cutoff,
			CompleteThroughCutoff: evidence.output.completeThroughCutoff,
			Final:                 evidence.output.final,
		},
	}
	if evidence.firedBound == supervisorCommandDeadlineFired {
		data.BoundFired = CommandDeadlineFired
	}
	switch evidence.kind {
	case supervisorTerminalSettled:
		return Settled{Exit: ExitStatus{Code: evidence.exitCode, Signal: evidence.exitSignal}, ExecutionData: data}
	case supervisorTerminalStopped:
		return Stopped{ExecutionData: data}
	case supervisorTerminalFuseTrip:
		return Tripped{Trip: FuseTrip{Live: evidence.count.value}, ExecutionData: data}
	case supervisorTerminalAutomaticDeadlineTrip:
		return Tripped{Trip: AutomaticDeadlineTrip{Peak: ObservedCount{
			Value: evidence.count.value, Present: evidence.count.present,
		}}, ExecutionData: data}
	case supervisorTerminalSerialDeadlineTrip:
		return Tripped{Trip: SerialDeadlineTrip{}, ExecutionData: data}
	case supervisorTerminalDrainUnconfirmed:
		return DrainUnconfirmed{Residual: OwnedUndrained, ExecutionData: data}
	case supervisorTerminalInfrastructureWait, supervisorTerminalInfrastructureRunning,
		supervisorTerminalInfrastructureRelease, supervisorTerminalInfrastructureOutput,
		supervisorTerminalInfrastructureControl:
		return Infrastructure{Cause: infrastructureCause(evidence.kind), ExecutionData: data}
	default:
		invariant(supervisorDriverOperation, "terminal evidence kind is invalid")

		return nil
	}
}

func infrastructureCause(kind supervisorTerminalKind) Cause {
	switch kind {
	case supervisorTerminalInfrastructureWait:
		return WaitFailed
	case supervisorTerminalInfrastructureRunning:
		return CensusFailed
	case supervisorTerminalInfrastructureRelease:
		return ReleaseFailed
	case supervisorTerminalInfrastructureOutput:
		return OutputCaptureFailed
	case supervisorTerminalInfrastructureControl:
		return TerminationControlFailed
	default:
		invariant(supervisorDriverOperation, "terminal kind has no infrastructure cause")

		return 0
	}
}

func (driver *supervisorDriver) requireAttempt(generation attemptGeneration) *supervisorDrivenAttempt {
	attempt := driver.attempts[generation]
	if generation == 0 || attempt == nil {
		invariant(supervisorDriverOperation, "attempt generation is stale or unknown")
	}

	return attempt
}
