package ooze

import (
	"sync"
	"time"
)

const supervisorDriverOperation = "drive supervisor"

const nominalSupervisorFuseCadence = 50 * time.Millisecond

type supervisorDriverConstruction struct {
	runtime          *processRuntimeShell
	now              func() time.Time
	launchBoundary   func(time.Time) <-chan time.Time
	launchProgress   time.Duration
	drainEpoch       time.Duration
	prepare          func(attemptGeneration, Spec)
	execute          func(supervisorAction) *supervisorEvent
	recheckRoot      func(attemptGeneration) (ExitStatus, time.Time, bool, error)
	sampleRunning    func(attemptGeneration) (bool, uint64, error)
	readOutput       func(supervisorOutputRef) string
	readDiagnostic   func(supervisorDiagnosticRef) error
	recordDiagnostic func(error) supervisorDiagnosticRef
}

type supervisorDrivenAttempt struct {
	spec            Spec
	owned           *OwnedAttempt
	launchResult    LaunchResult
	waitAction      supervisorAction
	sampleAction    supervisorAction
	waitStarted     bool
	terminalReady   bool
	terminal        chan Terminal
	launchBy        time.Time
	launchAction    supervisorAction
	launchEvent     *supervisorEvent
	launchWake      chan struct{}
	launchPublished chan struct{}
	launchResolved  bool
	launchConsumed  bool
	launchReturn    []supervisorAction
	emergencyReturn bool
}

type supervisorDriver struct {
	mutex             sync.Mutex
	state             supervisorState
	runtime           *processRuntimeShell
	now               func() time.Time
	launchBoundary    func(time.Time) <-chan time.Time
	launchProgress    time.Duration
	drainEpoch        time.Duration
	prepare           func(attemptGeneration, Spec)
	execute           func(supervisorAction) *supervisorEvent
	recheckRoot       func(attemptGeneration) (ExitStatus, time.Time, bool, error)
	sampleRunning     func(attemptGeneration) (bool, uint64, error)
	readOutput        func(supervisorOutputRef) string
	readDiagnostic    func(supervisorDiagnosticRef) error
	recordDiagnostic  func(error) supervisorDiagnosticRef
	attempts          map[attemptGeneration]*supervisorDrivenAttempt
	emergency         chan SweepResult
	emergencyStarted  bool
	emergencyReturns  int
	emergencyDeferred []supervisorAction
}

func newSupervisorDriver(construction supervisorDriverConstruction) *supervisorDriver {
	if construction.runtime == nil || construction.now == nil || construction.launchProgress <= 0 ||
		construction.drainEpoch <= 0 || construction.execute == nil || construction.readOutput == nil {
		panic("supervisor driver construction is incomplete")
	}

	launchBoundary := construction.launchBoundary
	if launchBoundary == nil {
		launchBoundary = func(time.Time) <-chan time.Time {
			return time.After(construction.launchProgress)
		}
	}

	return &supervisorDriver{
		runtime: construction.runtime, now: construction.now,
		launchBoundary: launchBoundary,
		launchProgress: construction.launchProgress, drainEpoch: construction.drainEpoch,
		prepare: construction.prepare, execute: construction.execute,
		recheckRoot: construction.recheckRoot, sampleRunning: construction.sampleRunning,
		readOutput: construction.readOutput, readDiagnostic: construction.readDiagnostic,
		recordDiagnostic: construction.recordDiagnostic,
		attempts:         make(map[attemptGeneration]*supervisorDrivenAttempt),
		emergency:        make(chan SweepResult, 1),
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
	published := driver.finishLaunchReturn(start.generation, actions)
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
	launchBy := registeredAt.Add(driver.launchProgress)
	driver.attempts[generation] = &supervisorDrivenAttempt{
		spec: spec, terminal: make(chan Terminal, 1), launchBy: launchBy,
		launchWake: make(chan struct{}, 1), launchPublished: make(chan struct{}),
	}
	driver.mutex.Unlock()
	if driver.prepare != nil {
		driver.prepare(generation, spec)
	}
	actions := driver.reduce(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: generation,
		attempt: attemptIdentity(spec.Attempt), at: registeredAt,
		launchBy: launchBy, profile: spec.Profile,
		commandDeadline: spec.Deadline,
	})
	if len(actions) != 1 || actions[0].kind != supervisorLaunchNative {
		invariant(supervisorDriverOperation, "prospective registration did not issue native launch")
	}
	driver.mutex.Lock()
	driver.requireAttempt(generation).launchAction = actions[0]
	driver.mutex.Unlock()
	go driver.executeLaunch(actions[0])
	boundary := driver.launchBoundary(launchBy)
	for {
		select {
		case <-driver.requireLaunchWake(generation):
			driver.mutex.Lock()
			attempt := driver.requireAttempt(generation)
			if attempt.launchResolved && len(attempt.launchReturn) != 0 {
				actions := append([]supervisorAction(nil), attempt.launchReturn...)
				attempt.launchReturn = nil
				completion := (*supervisorLaunchCompletion)(nil)
				if attempt.launchConsumed {
					completion = attempt.launchEvent.completion
				}
				observation := attemptObservation(launchUnconfirmed{})
				if completion != nil {
					observation = launchObservation(completion)
				}
				driver.mutex.Unlock()

				return observation, actions
			}
			event := attempt.launchEvent
			if event != nil && event.completion != nil && event.at.Before(launchBy) {
				next, publication := reduceSupervisor(driver.state, *event)
				driver.state = next
				attempt.launchConsumed = true
				attempt.launchResolved = true
				observation := launchObservation(event.completion)
				driver.mutex.Unlock()

				return observation, publication
			}
			driver.mutex.Unlock()
		case <-boundary:
			driver.mutex.Lock()
			attempt := driver.requireAttempt(generation)
			var completion *supervisorLaunchCompletion
			if attempt.launchEvent != nil && attempt.launchEvent.completion != nil &&
				!attempt.launchEvent.at.After(launchBy) {
				completion = attempt.launchEvent.completion
				attempt.launchConsumed = true
			}
			next, publication := reduceSupervisor(driver.state, supervisorEvent{
				kind: supervisorLaunchBoundary, generation: generation, at: launchBy,
				completion: completion,
			})
			driver.state = next
			attempt.launchResolved = true
			observation := attemptObservation(launchUnconfirmed{})
			if completion != nil {
				observation = launchObservation(completion)
			}
			driver.mutex.Unlock()

			return observation, publication
		}
	}
}

func (driver *supervisorDriver) finishLaunchReturn(
	generation attemptGeneration,
	actions []supervisorAction,
) LaunchResult {
	for _, action := range actions {
		driver.run(action)
	}
	driver.mutex.Lock()
	attempt := driver.requireAttempt(generation)
	published := attempt.launchResult
	var deferred []supervisorAction
	if attempt.emergencyReturn {
		attempt.emergencyReturn = false
		driver.emergencyReturns--
		if driver.emergencyReturns < 0 {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "emergency launch return count became negative")
		}
		if driver.emergencyReturns == 0 {
			deferred = driver.emergencyDeferred
			driver.emergencyDeferred = nil
		}
	}
	close(attempt.launchPublished)
	driver.mutex.Unlock()
	for _, action := range deferred {
		driver.run(action)
	}

	return published
}

func (driver *supervisorDriver) requireLaunchWake(generation attemptGeneration) <-chan struct{} {
	driver.mutex.Lock()
	wake := driver.requireAttempt(generation).launchWake
	driver.mutex.Unlock()

	return wake
}

func (driver *supervisorDriver) executeLaunch(action supervisorAction) {
	event := driver.execute(action)
	if event == nil || event.completion == nil {
		invariant(supervisorDriverOperation, "native launch returned no completion")
	}
	driver.mutex.Lock()
	attempt := driver.requireAttempt(action.generation)
	if attempt.launchEvent != nil || attempt.launchAction.token != action.token {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "native launch completion was duplicated or stale")
	}
	attempt.launchEvent = event
	published := attempt.launchPublished
	select {
	case attempt.launchWake <- struct{}{}:
	default:
	}
	driver.mutex.Unlock()

	<-published
	driver.mutex.Lock()
	attempt = driver.requireAttempt(action.generation)
	consumed := attempt.launchConsumed
	resolved := attempt.launchResolved
	driver.mutex.Unlock()
	if consumed {
		return
	}
	if !resolved {
		invariant(supervisorDriverOperation, "late launch completion preceded boundary resolution")
	}
	late := *event
	if event.completion.kind == supervisorLaunchReleased {
		late.drainBy = event.at.Add(driver.drainEpoch)
	}
	driver.apply(late)
}

func launchObservation(completion *supervisorLaunchCompletion) attemptObservation {
	if completion == nil {
		invariant(supervisorDriverOperation, "launch observation lacks completion")
	}
	switch completion.kind {
	case supervisorLaunchReleased:
		return launchOwned{}
	case supervisorLaunchProvenNotReleased:
		switch completion.failure {
		case LaunchFailed:
			return launchNotReleased{reason: launchFailed}
		case LaunchResourceExhausted:
			return launchNotReleased{reason: launchResourceExhausted}
		}
	}
	invariant(supervisorDriverOperation, "native launch completion classification is invalid")

	return nil
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
	case supervisorRevokeLaunchRelease:
		if event := driver.execute(action); event != nil {
			invariant(supervisorDriverOperation, "launch release revocation returned a completion")
		}
	case supervisorPublishLaunchUnconfirmed:
		driver.publishLaunchUnconfirmed(action)
	case supervisorCloseProspective:
		driver.closeProspective(action)
	case supervisorAdoptOwned:
		driver.adoptOwned(action)
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
	case supervisorTransferResidualCustody:
		driver.transferResidualCustody(action)
	case supervisorSettleRuntime:
		driver.settleRuntime(action)
	case supervisorDeliverTerminal:
		driver.deliverTerminal(action)
	case supervisorSettleEmergency:
		driver.mutex.Lock()
		if driver.emergencyReturns != 0 {
			driver.emergencyDeferred = append(driver.emergencyDeferred, action)
			driver.mutex.Unlock()

			return
		}
		driver.mutex.Unlock()
		driver.executeEmergency(action)
	case supervisorDeliverEmergencySettlement:
		driver.executeEmergency(action)
	case supervisorLaunchNative, supervisorObserveEmptiness, supervisorForceOwned,
		supervisorCaptureOutput, supervisorReleaseDomain:
		driver.executeAction(action)
	default:
		invariant(supervisorDriverOperation, "action kind is not implemented by the driver")
	}
}

func (driver *supervisorDriver) publishLaunchUnconfirmed(action supervisorAction) {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	attempt := driver.requireAttempt(action.generation)
	if attempt.launchResult != nil {
		invariant(supervisorDriverOperation, "unconfirmed launch was published twice")
	}
	attempt.launchResult = LaunchUnconfirmed{Residual: ProspectiveUnresolved}
}

func (driver *supervisorDriver) closeProspective(action supervisorAction) {
	driver.runtime.observeAttempt(action.generation, launchObservationFromAction(action))
}

func (driver *supervisorDriver) adoptOwned(action supervisorAction) {
	driver.runtime.observeAttempt(action.generation, launchOwned{})
}

func launchObservationFromAction(action supervisorAction) attemptObservation {
	completion := supervisorLaunchCompletion{kind: action.launchKind, failure: action.launchFailure}

	return launchObservation(&completion)
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
			status, completedAt, observed, err := driver.recheckRoot(waitAction.generation)
			if err != nil {
				invariant(supervisorDriverOperation, "deadline root recheck failed")
			}
			recheck := supervisorExitRecheck{performed: true, observed: observed, at: deadlineAt}
			if observed {
				recheck.at = completedAt
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
				if driver.recordDiagnostic == nil {
					invariant(supervisorDriverOperation, "running sampler diagnostic registry is absent")
				}
				facts = []supervisorRunningFact{{
					generation: waitAction.generation, action: sampleAction.token,
					kind: supervisorRunningObservationFailed, at: at,
					source: supervisorObservationRunning, diagnostic: driver.recordDiagnostic(err),
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
	_, unconfirmed := attempt.launchResult.(LaunchUnconfirmed)
	driver.mutex.Unlock()
	if owned != nil {
		owned.sealStopAdmission()
	} else if !unconfirmed {
		invariant(supervisorDriverOperation, "stop seal has no owned capability")
	}
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

func (driver *supervisorDriver) transferResidualCustody(action supervisorAction) {
	receipt := driver.runtime.observeAttempt(action.generation, drainUnconfirmed{})
	if receipt.settlementAcknowledged || receipt.confirmationProvisional ||
		!receipt.runtimeClosureInProgress {
		invariant(supervisorDriverOperation, "runtime rejected residual custody transfer")
	}
	completion := supervisorRuntimeCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		kind:       supervisorRuntimeClosurePending,
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
	terminal := publicTerminal(action.terminal, driver.readOutput, driver.readDiagnostic)
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
	returning := make(map[attemptGeneration]struct{})
	for _, state := range driver.state.attempts {
		if state.phase == supervisorLaunchClosedNotReleased {
			continue
		}
		snapshot := supervisorEmergencySnapshot{generation: state.generation}
		switch state.phase {
		case supervisorLaunchEstablishing:
			attempt := driver.requireAttempt(state.generation)
			if attempt.launchEvent != nil && attempt.launchEvent.completion != nil &&
				!attempt.launchEvent.at.After(request.At) {
				if attempt.launchEvent.completion.kind == supervisorLaunchReleased {
					if driver.recheckRoot == nil {
						driver.mutex.Unlock()
						invariant(supervisorDriverOperation, "released prospective emergency lacks a root completion cell")
					}
					status, completedAt, observed, err := driver.recheckRoot(state.generation)
					if err != nil {
						driver.mutex.Unlock()
						invariant(supervisorDriverOperation, "released prospective root snapshot failed")
					}
					recheckAt := request.At
					if observed {
						recheckAt = completedAt
					}
					snapshot.running = &supervisorRunningBundle{
						generation: state.generation,
						exitRecheck: supervisorExitRecheck{
							performed: true, observed: observed, at: recheckAt,
							action: state.launchAction, code: status.Code, signal: status.Signal,
						},
					}
					deadlineAt := attempt.launchEvent.completion.at.Add(state.commandDeadline)
					selectedAt := time.Time{}
					if observed && !completedAt.After(deadlineAt) {
						selectedAt = completedAt
					} else if !request.At.Before(deadlineAt) {
						selectedAt = deadlineAt
					}
					if !selectedAt.IsZero() {
						snapshot.running.drainBy = selectedAt.Add(driver.drainEpoch)
					}
				}
				snapshot.completion = attempt.launchEvent.completion
			}
			returning[state.generation] = struct{}{}
		case supervisorLaunchReportedUnconfirmed:
			attempt := driver.requireAttempt(state.generation)
			if attempt.launchEvent != nil && attempt.launchEvent.completion != nil &&
				!attempt.launchEvent.at.After(request.At) {
				snapshot.completion = attempt.launchEvent.completion
			}
		case supervisorRunning:
			snapshot.running = &supervisorRunningBundle{
				generation:   state.generation,
				waitAction:   state.waitAction,
				sampleAction: state.sampleAction,
			}
		case supervisorAwaitingEmergencySettlement:
		default:
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "attempt phase has no emergency snapshot")
		}
		snapshots = append(snapshots, snapshot)
	}
	next, actions := reduceSupervisor(driver.state, supervisorEvent{
		kind: supervisorEmergencyStarted, at: request.At, drainBy: request.DrainBy,
		emergencySnapshots: snapshots,
	})
	driver.state = next
	remaining := make([]supervisorAction, 0, len(actions))
	for _, action := range actions {
		if _, ok := returning[action.generation]; ok {
			attempt := driver.requireAttempt(action.generation)
			attempt.launchReturn = append(attempt.launchReturn, action)

			continue
		}
		if action.kind == supervisorSettleEmergency && len(returning) != 0 {
			driver.emergencyDeferred = append(driver.emergencyDeferred, action)

			continue
		}
		remaining = append(remaining, action)
	}
	for generation := range returning {
		attempt := driver.requireAttempt(generation)
		if len(attempt.launchReturn) == 0 || attempt.launchResolved || attempt.emergencyReturn {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "emergency launch return is absent or duplicated")
		}
		attempt.launchResolved = true
		attempt.launchConsumed = snapshotsCompletion(snapshots, generation) != nil
		attempt.emergencyReturn = true
		driver.emergencyReturns++
		select {
		case attempt.launchWake <- struct{}{}:
		default:
		}
	}
	driver.mutex.Unlock()
	for _, action := range remaining {
		driver.run(action)
	}

	return <-driver.emergency
}

func snapshotsCompletion(
	snapshots []supervisorEmergencySnapshot,
	generation attemptGeneration,
) *supervisorLaunchCompletion {
	for _, snapshot := range snapshots {
		if snapshot.generation == generation {
			return snapshot.completion
		}
	}

	return nil
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
	readDiagnostic func(supervisorDiagnosticRef) error,
) Terminal {
	diagnostics := resolvePublicDiagnostics(evidence, readDiagnostic)
	data := ExecutionData{
		Deadline: evidence.commandDeadline, LaunchDuration: evidence.launchDuration,
		CommandDuration: evidence.commandDuration,
		Output: OutputSnapshot{
			Bytes: readOutput(evidence.output.ref), Cutoff: evidence.output.cutoff,
			CompleteThroughCutoff: evidence.output.completeThroughCutoff,
			Final:                 evidence.output.final,
		},
		Failures: diagnostics.failures,
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
		return Infrastructure{
			Cause: infrastructureCause(evidence.kind), Err: diagnostics.primary(evidence.kind),
			ExecutionData: data,
		}
	default:
		invariant(supervisorDriverOperation, "terminal evidence kind is invalid")

		return nil
	}
}

type publicSupervisorDiagnostics struct {
	failures FailureDiagnostics
	wait     error
	running  error
	drain    error
	control  error
	output   error
	release  error
}

func resolvePublicDiagnostics(
	evidence supervisorTerminalEvidence,
	read func(supervisorDiagnosticRef) error,
) publicSupervisorDiagnostics {
	resolve := func(ref supervisorDiagnosticRef) error {
		if ref == 0 {
			return nil
		}
		if read == nil {
			invariant(supervisorDriverOperation, "terminal diagnostic registry is absent")
		}
		err := read(ref)
		if err == nil {
			invariant(supervisorDriverOperation, "terminal diagnostic reference resolved nil")
		}

		return err
	}
	result := publicSupervisorDiagnostics{
		wait: resolve(evidence.diagnostics.wait), running: resolve(evidence.diagnostics.running),
		drain: resolve(evidence.diagnostics.drain), control: resolve(evidence.diagnostics.control),
		output: resolve(evidence.output.diagnostic), release: resolve(evidence.diagnostics.release),
	}
	message := func(err error) string {
		if err == nil {
			return ""
		}

		return err.Error()
	}
	result.failures = FailureDiagnostics{
		Wait: message(result.wait), RunningCensus: message(result.running),
		DrainCensus: message(result.drain), Termination: message(result.control),
		Output: message(result.output), Release: message(result.release),
	}

	return result
}

func (diagnostics publicSupervisorDiagnostics) primary(kind supervisorTerminalKind) error {
	var err error
	switch kind {
	case supervisorTerminalInfrastructureWait:
		err = diagnostics.wait
	case supervisorTerminalInfrastructureRunning:
		err = diagnostics.running
	case supervisorTerminalInfrastructureControl:
		err = diagnostics.control
	case supervisorTerminalInfrastructureOutput:
		err = diagnostics.output
	case supervisorTerminalInfrastructureRelease:
		err = diagnostics.release
	}
	if err == nil {
		invariant(supervisorDriverOperation, "infrastructure terminal lacks its primary diagnostic")
	}

	return err
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
