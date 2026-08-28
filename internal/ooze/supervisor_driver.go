package ooze

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

const supervisorDriverOperation = "drive supervisor"

const nominalSupervisorFuseCadence = 50 * time.Millisecond

type supervisorDriverConstruction struct {
	runtime          *processruntime.Runtime
	observer         supervisionOwnerCutObserver
	ownerSequence    *supervisionOwnerCutSequence
	now              func() time.Time
	launchBoundary   func(time.Time) <-chan time.Time
	commandBoundary  func(time.Time) <-chan time.Time
	sampleTicks      func() (<-chan time.Time, func())
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
	monitorStarted  bool
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
	preempted       bool
	runtimeReceipt  processruntime.Receipt
	receiptReady    bool
}

type supervisorDriver struct {
	mutex             sync.Mutex
	machine           *supervisorMachine
	runtime           *processruntime.Runtime
	now               func() time.Time
	launchBoundary    func(time.Time) <-chan time.Time
	commandBoundary   func(time.Time) <-chan time.Time
	sampleTicks       func() (<-chan time.Time, func())
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
	reservations      map[*processruntime.StartCell]Spec
	emergency         chan SweepResult
	emergencyStarted  bool
	emergencyReturns  int
	emergencyDeferred []supervisorAction
	emergencyReceipt  processruntime.EmergencySettlement
	emergencyReady    bool
	observer          supervisionOwnerCutObserver
	ownerSequence     *supervisionOwnerCutSequence
	localSequence     supervisionOwnerCutSequence
	ownerCuts         []supervisionOwnerCut
}

func newSupervisorDriver(construction supervisorDriverConstruction) *supervisorDriver {
	if construction.runtime == nil || construction.now == nil || construction.launchProgress <= 0 ||
		construction.drainEpoch <= 0 || construction.execute == nil || construction.readOutput == nil {
		panic("supervisor driver construction is incomplete")
	}

	launchBoundary := construction.launchBoundary
	if launchBoundary == nil {
		launchBoundary = waitForSupervisorLaunchBoundary
	}
	commandBoundary := construction.commandBoundary
	if commandBoundary == nil {
		commandBoundary = waitForSupervisorLaunchBoundary
	}
	sampleTicks := construction.sampleTicks
	if sampleTicks == nil {
		sampleTicks = func() (<-chan time.Time, func()) {
			ticker := time.NewTicker(nominalSupervisorFuseCadence)
			return ticker.C, ticker.Stop
		}
	}

	observer := construction.observer
	if observer == nil {
		observer = supervisionNoopObserver{}
	}
	return &supervisorDriver{
		machine: newSupervisorMachine(),
		runtime: construction.runtime, now: construction.now,
		launchBoundary: launchBoundary, commandBoundary: commandBoundary, sampleTicks: sampleTicks,
		launchProgress: construction.launchProgress, drainEpoch: construction.drainEpoch,
		prepare: construction.prepare, execute: construction.execute,
		recheckRoot: construction.recheckRoot, sampleRunning: construction.sampleRunning,
		readOutput: construction.readOutput, readDiagnostic: construction.readDiagnostic,
		recordDiagnostic: construction.recordDiagnostic,
		attempts:         make(map[attemptGeneration]*supervisorDrivenAttempt),
		reservations:     make(map[*processruntime.StartCell]Spec),
		emergency:        make(chan SweepResult, 1),
		observer:         observer,
		ownerSequence:    construction.ownerSequence,
	}
}

func waitForSupervisorLaunchBoundary(launchBy time.Time) <-chan time.Time {
	return time.After(time.Until(launchBy))
}

func (driver *supervisorDriver) ownerObserver() supervisionOwnerCutObserver {
	if driver.observer == nil {
		return supervisionNoopObserver{}
	}

	return driver.observer
}

func (driver *supervisorDriver) reserveOwnerCut() supervisionOwnerCutReservation {
	sequence := driver.ownerSequence
	if sequence == nil {
		sequence = &driver.localSequence
	}

	return supervisionOwnerCutReservation(sequence.Add(1))
}

func (driver *supervisorDriver) completeOwnerEffect(action supervisorAction) {
	defer func() { _ = recover() }()
	driver.ownerObserver().Complete(supervisionEffectFromAction(action))
}

func newDrivenSupervisorForTest(
	installStart func(attemptIdentity, *processruntime.StartCell) processruntime.PreparedStart,
	driver *supervisorDriver,
) *Supervisor {
	if driver == nil {
		panic("driven supervisor requires a driver")
	}
	supervisor := newSupervisorForTest(installStart, driver.launch)
	supervisor.reserveLaunch = driver.reserveLaunch
	supervisor.discardLaunch = driver.discardLaunch
	supervisor.driveLaunch = driver.launchInstalled
	supervisor.emergencyDrain = driver.emergencyDrain

	return supervisor
}

func (driver *supervisorDriver) launchInstalled(start processruntime.PreparedStart, spec Spec) LaunchResult {
	return driver.launchManaged(start, spec).result
}

func (driver *supervisorDriver) launchManaged(start processruntime.PreparedStart, spec Spec) managedObservedLaunch {
	var actions []supervisorAction
	var launchObservation attemptObservation
	var receipt processruntime.Receipt
	var receiptReady bool
	observed := start.Launch(func(generation processruntime.Generation) processruntime.Observation {
		launchObservation, actions, receipt, receiptReady = driver.stageLaunch(start, spec)

		return processRuntimeObservation(launchObservation)
	})
	if !receiptReady {
		receipt = driver.runtime.Observe(start.Generation(), observed)
	}
	published := driver.finishLaunchReturn(start.Generation(), actions)
	if launchObservation == nil || published == nil {
		invariant(supervisorDriverOperation, "launch returned before publication")
	}

	return managedObservedLaunch{result: published, receipt: receipt}
}

func (driver *supervisorDriver) reserveLaunch(cell *processruntime.StartCell, spec Spec) {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	_, duplicated := driver.reservations[cell]
	if cell == nil || duplicated {
		invariant(supervisorDriverOperation, "launch reservation is invalid or duplicated")
	}
	driver.reservations[cell] = spec
}

func (driver *supervisorDriver) discardLaunch(cell *processruntime.StartCell) {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	if _, reserved := driver.reservations[cell]; !reserved || cell.InstalledGeneration() != 0 {
		invariant(supervisorDriverOperation, "discarded launch reservation is absent or installed")
	}
	delete(driver.reservations, cell)
}

func (driver *supervisorDriver) stageLaunch(
	start processruntime.PreparedStart,
	spec Spec,
) (attemptObservation, []supervisorAction, processruntime.Receipt, bool) {
	generation := start.Generation()
	registeredAt := driver.now()
	driver.mutex.Lock()
	if attempt := driver.attempts[generation]; attempt != nil {
		if _, reserved := driver.reservations[start.Cell()]; !attempt.preempted || reserved {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "launch generation is duplicated")
		}
		wake := attempt.launchWake
		driver.mutex.Unlock()
		<-wake
		driver.mutex.Lock()
		attempt = driver.requireAttempt(generation)
		if attempt.launchResult == nil || !attempt.receiptReady {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "preempted launch lacks publication or runtime receipt")
		}
		result := attempt.launchResult
		receipt := attempt.runtimeReceipt
		driver.mutex.Unlock()

		return brokerLaunchObservation(result), nil, receipt, true
	}
	if _, ok := driver.reservations[start.Cell()]; generation == 0 || !ok {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "launch generation is zero or unreserved")
	}
	delete(driver.reservations, start.Cell())
	launchBy := registeredAt.Add(driver.launchProgress)
	driver.attempts[generation] = &supervisorDrivenAttempt{
		spec: spec, terminal: make(chan Terminal, 1), launchBy: launchBy,
		launchWake: make(chan struct{}, 1), launchPublished: make(chan struct{}),
	}
	actions := driver.reduceLocked(supervisorEvent{
		kind: supervisorProspectiveRegistered, generation: generation,
		attempt: attemptIdentity(spec.Attempt), at: registeredAt,
		launchBy: launchBy, profile: spec.Profile,
		commandDeadline: spec.Deadline,
	})
	if len(actions) != 1 || actions[0].kind != supervisorLaunchNative {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "prospective registration did not issue native launch")
	}
	driver.requireAttempt(generation).launchAction = actions[0]
	driver.mutex.Unlock()
	driver.publishOwnerCuts()
	if driver.prepare != nil {
		driver.prepare(generation, spec)
	}
	driver.mutex.Lock()
	preempted := driver.requireAttempt(generation).emergencyReturn
	driver.mutex.Unlock()
	if !preempted {
		go driver.executeLaunch(actions[0])
	}
	boundary := driver.launchBoundary(launchBy)
	for {
		select {
		case <-driver.requireLaunchWake(generation):
			leaveRecorder := driver.ownerObserver().Enter()
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
				leaveRecorder()

				return observation, actions, processruntime.Receipt{}, false
			}
			event := attempt.launchEvent
			if event != nil && event.completion != nil && event.at.Before(launchBy) {
				publication := driver.reduceLocked(*event)
				attempt.launchConsumed = true
				attempt.launchResolved = true
				observation := launchObservation(event.completion)
				driver.mutex.Unlock()
				driver.publishOwnerCuts()
				leaveRecorder()

				return observation, publication, processruntime.Receipt{}, false
			}
			driver.mutex.Unlock()
			driver.publishOwnerCuts()
			leaveRecorder()
		case <-boundary:
			leaveRecorder := driver.ownerObserver().Enter()
			driver.mutex.Lock()
			attempt := driver.requireAttempt(generation)
			var completion *supervisorLaunchCompletion
			if attempt.launchEvent != nil && attempt.launchEvent.completion != nil &&
				!attempt.launchEvent.at.After(launchBy) {
				completion = attempt.launchEvent.completion
				attempt.launchConsumed = true
			}
			boundaryDrainBy := time.Time{}
			if completion != nil && completion.kind == supervisorLaunchReleaseUnconfirmed {
				boundaryDrainBy = attempt.launchEvent.drainBy
			}
			publication := driver.reduceLocked(supervisorEvent{
				kind: supervisorLaunchBoundary, generation: generation, at: launchBy,
				completion: completion, drainBy: boundaryDrainBy,
			})
			attempt.launchResolved = true
			observation := attemptObservation(launchUnconfirmed{})
			if completion != nil {
				observation = launchObservation(completion)
			}
			driver.mutex.Unlock()
			driver.publishOwnerCuts()
			leaveRecorder()

			return observation, publication, processruntime.Receipt{}, false
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
	driver.startEligibleMonitors()
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
	defer driver.completeOwnerEffect(action)
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
	if event.completion.kind == supervisorLaunchReleased ||
		event.completion.kind == supervisorLaunchReleaseUnconfirmed {
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
	case supervisorLaunchReleaseUnconfirmed:
		return launchUnconfirmed{}
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
	leaveRecorder := driver.ownerObserver().Enter()
	driver.mutex.Lock()
	actions := driver.reduceLocked(event)
	driver.mutex.Unlock()
	driver.publishOwnerCuts()
	leaveRecorder()

	return actions
}

func (driver *supervisorDriver) reduceLocked(event supervisorEvent) []supervisorAction {
	if driver.machine == nil {
		driver.machine = newSupervisorMachine()
	}
	reservation := driver.reserveOwnerCut()
	fact := supervisionFactFromEvent(event)
	var transition supervisorTransition
	driver.machine, transition = driver.machine.Apply(fact)
	accepted := fact.production()
	actions := transition.actions()
	driver.ownerCuts = append(driver.ownerCuts, supervisionOwnerCut{
		reservation: reservation, fact: supervisionFactFromEvent(accepted),
		event: transition.Event(), projection: driver.machine.Projection(),
		effects: supervisionEffectsFromActions(actions),
	})

	return actions
}

func (driver *supervisorDriver) publishOwnerCuts() {
	driver.mutex.Lock()
	cuts := append([]supervisionOwnerCut(nil), driver.ownerCuts...)
	driver.ownerCuts = nil
	driver.mutex.Unlock()
	for _, cut := range cuts {
		func() {
			defer func() { _ = recover() }()
			driver.ownerObserver().Publish(cut.reservation, cut.fact, cut.event, cut.projection, cut.effects)
		}()
	}
}

func (driver *supervisorDriver) supervisorState() supervisorState {
	if driver.machine == nil {
		return supervisorState{}
	}

	return driver.machine.snapshot()
}

func (driver *supervisorDriver) apply(event supervisorEvent) {
	actions := driver.reduce(event)
	for _, action := range actions {
		driver.run(action)
	}
	driver.startEligibleMonitors()
}

func (driver *supervisorDriver) run(action supervisorAction) {
	switch action.kind {
	case supervisorLaunchNative, supervisorWaitRoot, supervisorSampleRunning,
		supervisorDeliverTerminal, supervisorDeliverEmergencySettlement:
	default:
		defer driver.completeOwnerEffect(action)
	}
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
	receipt := driver.runtime.Observe(action.generation, processRuntimeObservation(launchObservationFromAction(action)))
	completion := supervisorRuntimeCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		kind:       normalizedSupervisorRuntimeReceipt(receipt),
	}
	driver.apply(supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: action.generation,
		runtime: &completion,
	})
}

func (driver *supervisorDriver) adoptOwned(action supervisorAction) {
	driver.runtime.Observe(action.generation, processruntime.Owned())
}

func launchObservationFromAction(action supervisorAction) attemptObservation {
	completion := supervisorLaunchCompletion{kind: action.launchKind, failure: action.launchFailure}

	return launchObservation(&completion)
}

func launchObservationFromEffect(effect supervisionEffect) attemptObservation {
	completion, ok := effect.LaunchCompletion()
	if !ok {
		invariant(supervisorDriverOperation, "effect cannot publish a launch observation")
	}

	return launchObservation(completion.production())
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
	attempt := driver.requireAttempt(action.generation)
	if attempt.launchResult != nil {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "not-released launch was published twice")
	}
	var err error
	if action.launchDiagnostic != 0 {
		if driver.readDiagnostic == nil {
			invariant(supervisorDriverOperation, "launch diagnostic registry is absent")
		}
		err = driver.readDiagnostic(action.launchDiagnostic)
		if err == nil {
			invariant(supervisorDriverOperation, "launch diagnostic reference resolved nil")
		}
	}
	attempt.launchResult = NotReleased{Kind: action.launchFailure, Err: err}
	if attempt.preempted {
		select {
		case attempt.launchWake <- struct{}{}:
		default:
		}
	}
	driver.mutex.Unlock()
}

func (driver *supervisorDriver) rememberMonitor(action supervisorAction, sample bool) {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(action.generation)
	if sample {
		if attempt.sampleAction.token != 0 {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "running sampler action was duplicated")
		}
		attempt.sampleAction = action
	} else {
		if attempt.waitAction.token != 0 {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "root waiter action was duplicated")
		}
		attempt.waitAction = action
	}
	driver.mutex.Unlock()
}

func (driver *supervisorDriver) startEligibleMonitors() {
	type monitorStart struct {
		wait, sample supervisorAction
		deadline     time.Time
	}
	driver.mutex.Lock()
	starts := make([]monitorStart, 0, len(driver.attempts))
	for generation, attempt := range driver.attempts {
		ready := attempt.waitAction.token != 0 &&
			(attempt.spec.Profile == SerialProfile || attempt.sampleAction.token != 0)
		if !ready || attempt.monitorStarted {
			continue
		}
		attempt.monitorStarted = true
		state := driver.supervisorState()
		attemptState := state.attempts[state.requireAttempt(generation)]
		starts = append(starts, monitorStart{
			wait: attempt.waitAction, sample: attempt.sampleAction, deadline: attemptState.deadlineAt,
		})
	}
	driver.mutex.Unlock()
	for _, start := range starts {
		go driver.monitor(start.wait, start.sample, start.deadline)
	}
}

func (driver *supervisorDriver) monitor(
	waitAction supervisorAction,
	sampleAction supervisorAction,
	deadlineAt time.Time,
) {
	defer driver.completeOwnerEffect(waitAction)
	if sampleAction.token != 0 {
		defer driver.completeOwnerEffect(sampleAction)
	}
	if driver.recheckRoot == nil {
		driver.executeAction(waitAction)

		return
	}
	driver.waitThroughDeadline(waitAction, sampleAction, deadlineAt)
}

func (driver *supervisorDriver) wait(generation attemptGeneration) Terminal {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(generation)
	if attempt.waitStarted || !attempt.monitorStarted {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "owned monitor is absent or wait was duplicated")
	}
	attempt.waitStarted = true
	terminal := attempt.terminal
	driver.mutex.Unlock()

	return <-terminal
}

func (driver *supervisorDriver) waitThroughDeadline(
	waitAction supervisorAction,
	sampleAction supervisorAction,
	deadlineAt time.Time,
) {
	waited := make(chan *supervisorEvent, 1)
	go func() { waited <- driver.execute(waitAction) }()
	deadline := driver.commandBoundary(deadlineAt)
	var samples <-chan time.Time
	if sampleAction.token != 0 {
		if driver.sampleRunning == nil {
			invariant(supervisorDriverOperation, "automatic wait lacks a running sampler")
		}
		var stopSamples func()
		samples, stopSamples = driver.sampleTicks()
		if samples == nil || stopSamples == nil {
			invariant(supervisorDriverOperation, "automatic wait lacks a sample cadence")
		}
		defer stopSamples()
	}
	for {
		select {
		case event := <-waited:
			driver.correlateWaitCompletion(event, waitAction, sampleAction)
			select {
			case at := <-samples:
				if at.Before(deadlineAt) {
					if at.After(event.at) {
						driver.applyMonitorEvent(*event)

						return
					}
					driver.applyReadyWaitAndSample(
						event, waitAction, sampleAction, at,
						driver.runningSampleFacts(waitAction, sampleAction, at),
					)

					return
				}
			default:
			}
			driver.applyMonitorEvent(*event)

			return
		case <-deadline:
			driver.applyDeadlineBoundary(
				waited, waitAction, sampleAction, samples, deadlineAt, nil,
			)

			return
		case at := <-samples:
			if at.Equal(deadlineAt) {
				driver.applyDeadlineBoundary(
					waited, waitAction, sampleAction, samples, deadlineAt,
					driver.runningSampleFacts(waitAction, sampleAction, at),
				)

				return
			}
			if at.After(deadlineAt) {
				continue
			}
			facts := driver.runningSampleFacts(waitAction, sampleAction, at)
			select {
			case event := <-waited:
				driver.correlateWaitCompletion(event, waitAction, sampleAction)
				driver.applyReadyWaitAndSample(event, waitAction, sampleAction, at, facts)

				return
			default:
			}
			if driver.applyRunningSampleFacts(waitAction, sampleAction, at, facts) {
				return
			}
		}
	}
}

func (driver *supervisorDriver) applyDeadlineBoundary(
	waited <-chan *supervisorEvent,
	waitAction supervisorAction,
	sampleAction supervisorAction,
	samples <-chan time.Time,
	deadlineAt time.Time,
	facts []supervisorRunningFact,
) {
	facts = append(facts, driver.readyRunningFactsThroughDeadline(
		waited, waitAction, sampleAction, samples, deadlineAt,
	)...)
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
	driver.applyMonitorEvent(supervisorEvent{
		kind: supervisorRunningObserved, generation: waitAction.generation,
		at: deadlineAt, drainBy: deadlineAt.Add(driver.drainEpoch),
		running: &supervisorRunningBundle{
			generation: waitAction.generation,
			waitAction: waitAction.token, sampleAction: sampleAction.token,
			facts: facts, exitRecheck: recheck,
		},
	})
}

func (driver *supervisorDriver) readyRunningFactsThroughDeadline(
	waited <-chan *supervisorEvent,
	waitAction supervisorAction,
	sampleAction supervisorAction,
	samples <-chan time.Time,
	deadlineAt time.Time,
) []supervisorRunningFact {
	var facts []supervisorRunningFact
	select {
	case event := <-waited:
		driver.correlateWaitCompletion(event, waitAction, sampleAction)
		if !event.at.After(deadlineAt) {
			facts = append(facts, event.running.facts...)
		}
	default:
	}
	for {
		select {
		case at := <-samples:
			if !at.After(deadlineAt) {
				facts = append(facts, driver.runningSampleFacts(
					waitAction, sampleAction, at,
				)...)
			}
		default:
			return facts
		}
	}
}

func (driver *supervisorDriver) applyReadyWaitAndSample(
	event *supervisorEvent,
	waitAction supervisorAction,
	sampleAction supervisorAction,
	sampleAt time.Time,
	sampleFacts []supervisorRunningFact,
) {
	switch {
	case event.at.Equal(sampleAt):
		event.running.facts = append(event.running.facts, sampleFacts...)
		driver.applyMonitorEvent(*event)
	case event.at.Before(sampleAt):
		driver.applyMonitorEvent(*event)
	default:
		if !driver.applyRunningSampleFacts(waitAction, sampleAction, sampleAt, sampleFacts) {
			driver.applyMonitorEvent(*event)
		}
	}
}

func (driver *supervisorDriver) correlateWaitCompletion(
	event *supervisorEvent,
	waitAction supervisorAction,
	sampleAction supervisorAction,
) {
	if event == nil || event.running == nil ||
		event.generation != waitAction.generation ||
		event.running.generation != waitAction.generation ||
		event.running.waitAction != waitAction.token ||
		event.running.sampleAction != 0 {
		invariant(supervisorDriverOperation, "root wait returned a malformed completion")
	}
	event.running.sampleAction = sampleAction.token
}

func (driver *supervisorDriver) runningSampleFacts(
	waitAction supervisorAction,
	sampleAction supervisorAction,
	at time.Time,
) []supervisorRunningFact {
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

	return facts
}

func (driver *supervisorDriver) applyRunningSampleFacts(
	waitAction supervisorAction,
	sampleAction supervisorAction,
	at time.Time,
	facts []supervisorRunningFact,
) bool {
	if len(facts) == 0 {
		return false
	}
	driver.applyMonitorEvent(supervisorEvent{
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

	return terminalReady
}

func (driver *supervisorDriver) applyMonitorEvent(event supervisorEvent) {
	leaveRecorder := driver.ownerObserver().Enter()
	recorderReleased := false
	defer func() {
		if !recorderReleased {
			leaveRecorder()
		}
	}()
	driver.mutex.Lock()
	state := driver.supervisorState()
	index := state.attemptIndex(event.generation)
	if index < 0 {
		driver.mutex.Unlock()
		return
	}
	attempt := state.attempts[index]
	accept := attempt.phase == supervisorRunning || attempt.phase == supervisorIntentLatched ||
		attempt.phase == supervisorEmergencyDraining
	if !accept {
		driver.mutex.Unlock()
		return
	}
	actions := driver.reduceLocked(event)
	driver.mutex.Unlock()
	driver.publishOwnerCuts()
	leaveRecorder()
	recorderReleased = true
	for _, action := range actions {
		driver.run(action)
	}
	driver.startEligibleMonitors()
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
	receipt := driver.runtime.Observe(action.generation, processRuntimeObservation(terminalObservation(action.terminal)))
	driver.recordRuntimeReceipt(action.generation, receipt)
	completion := supervisorRuntimeCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		kind:       normalizedSupervisorRuntimeReceipt(receipt),
	}
	driver.apply(supervisorEvent{
		kind: supervisorRuntimeCompleted, generation: action.generation,
		runtime: &completion,
	})
}

func normalizedSupervisorRuntimeReceipt(receipt processruntime.Receipt) supervisorRuntimeReceiptKind {
	if receipt.SettlementAcknowledged() {
		if receipt.ConfirmationProvisional() {
			return supervisorRuntimeProvisionalDeadline
		}

		return supervisorRuntimeAcknowledged
	}
	if receipt.RuntimeClosureInProgress() {
		return supervisorRuntimeClosurePending
	}

	return 0
}

func (driver *supervisorDriver) transferResidualCustody(action supervisorAction) {
	receipt := driver.runtime.Observe(action.generation, processruntime.DrainUnconfirmed())
	driver.recordRuntimeReceipt(action.generation, receipt)
	if receipt.SettlementAcknowledged() || receipt.ConfirmationProvisional() ||
		!receipt.RuntimeClosureInProgress() {
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
		return attemptSettled{profile: evidence.profile, deadline: evidence.commandDeadline}
	case supervisorTerminalFuseTrip:
		return attemptTripped{kind: fuseTrip}
	case supervisorTerminalAutomaticDeadlineTrip, supervisorTerminalSerialDeadlineTrip:
		return attemptTripped{
			kind: deadlineTrip, profile: evidence.profile, deadline: evidence.commandDeadline,
		}
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

func terminalObservationFromEffect(effect supervisionEffect) attemptObservation {
	evidence, _, ok := effect.TerminalEvidence()
	if !ok {
		invariant(supervisorDriverOperation, "effect cannot settle a terminal observation")
	}

	return terminalObservation(evidence.production())
}

func (driver *supervisorDriver) deliverTerminal(action supervisorAction) {
	driver.mutex.Lock()
	attempt := driver.requireAttempt(action.generation)
	terminal := publicTerminal(action.terminal, driver.readOutput, driver.readDiagnostic, action.runtimeKind)
	delivery := attempt.terminal
	if attempt.terminalReady {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "terminal delivery was duplicated")
	}
	attempt.terminalReady = true
	driver.mutex.Unlock()
	delivery <- terminal
}

func (driver *supervisorDriver) recordRuntimeReceipt(generation attemptGeneration, receipt processruntime.Receipt) {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()
	attempt := driver.requireAttempt(generation)
	if attempt.receiptReady {
		invariant(supervisorDriverOperation, "runtime receipt was duplicated")
	}
	attempt.runtimeReceipt = receipt
	attempt.receiptReady = true
}

func (driver *supervisorDriver) waitManaged(
	generation attemptGeneration,
	owned *OwnedAttempt,
) managedObservedTerminal {
	if owned == nil {
		invariant(supervisorDriverOperation, "managed wait lacks owned attempt")
	}
	terminal := owned.Wait()
	driver.mutex.Lock()
	attempt := driver.requireAttempt(generation)
	if !attempt.receiptReady {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "managed terminal lacks runtime receipt")
	}
	receipt := attempt.runtimeReceipt
	driver.mutex.Unlock()

	return managedObservedTerminal{terminal: terminal, receipt: receipt}
}

func (driver *supervisorDriver) emergencyDrain(request EmergencyRequest) SweepResult {
	leaveRecorder := driver.ownerObserver().Enter()
	recorderReleased := false
	defer func() {
		if !recorderReleased {
			leaveRecorder()
		}
	}()
	driver.mutex.Lock()
	if driver.emergencyStarted {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "emergency drain was duplicated")
	}
	driver.emergencyStarted = true
	preempted := driver.preemptReservedLaunchesLocked(request)
	stateSnapshot := driver.supervisorState()
	launchEvidence := make([]supervisionEmergencyEvidence, 0, len(stateSnapshot.attempts))
	for _, state := range stateSnapshot.attempts {
		attempt := driver.requireAttempt(state.generation)
		item := supervisionEmergencyEvidence{generation: state.generation}
		if attempt.launchEvent != nil && attempt.launchEvent.completion != nil &&
			!attempt.launchEvent.at.After(request.At) {
			item.completion = attempt.launchEvent.completion
		}
		launchEvidence = append(launchEvidence, item)
	}
	plan, ready := driver.machine.PlanEmergency(request.At, request.DrainBy, driver.drainEpoch, launchEvidence)
	if !ready {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "emergency plan is not enabled")
	}
	returning := make(map[attemptGeneration]struct{})
	for _, generation := range plan.ReturningLaunches() {
		if !driver.requireAttempt(generation).preempted {
			returning[generation] = struct{}{}
		}
	}
	rootEvidence := make([]supervisionEmergencyEvidence, 0, len(plan.RootRequests()))
	for _, root := range plan.RootRequests() {
		if driver.recheckRoot == nil {
			if root.required {
				driver.mutex.Unlock()
				invariant(supervisorDriverOperation, "emergency root evidence is required")
			}
			continue
		}
		status, completedAt, observed, err := driver.recheckRoot(root.generation)
		if err != nil {
			driver.mutex.Unlock()
			invariant(supervisorDriverOperation, "emergency root snapshot failed")
		}
		rootEvidence = append(rootEvidence, supervisionEmergencyEvidence{
			generation: root.generation,
			root: supervisionEmergencyRootEvidence{
				checked: true, observed: observed, at: completedAt,
				exitCode: status.Code, exitSignal: status.Signal,
			},
		})
	}
	fact, ready := driver.machine.PrepareEmergencyPlan(plan, rootEvidence)
	if !ready {
		driver.mutex.Unlock()
		invariant(supervisorDriverOperation, "emergency evidence is not enabled")
	}
	event := fact.production()
	snapshots := event.emergencySnapshots
	actions := driver.reduceLocked(event)
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
	driver.publishOwnerCuts()
	leaveRecorder()
	recorderReleased = true
	for _, generation := range preempted {
		receipt := driver.runtime.Observe(generation, processruntime.NotReleased(false))
		driver.recordRuntimeReceipt(generation, receipt)
	}
	for _, action := range remaining {
		driver.run(action)
	}

	return <-driver.emergency
}

func (driver *supervisorDriver) preemptReservedLaunchesLocked(request EmergencyRequest) []attemptGeneration {
	type reservation struct {
		generation attemptGeneration
		cell       *processruntime.StartCell
		spec       Spec
	}
	reservations := make([]reservation, 0, len(driver.reservations))
	for cell, spec := range driver.reservations {
		if generation := cell.InstalledGeneration(); generation != 0 {
			reservations = append(reservations, reservation{generation: generation, cell: cell, spec: spec})
		}
	}
	slices.SortFunc(reservations, func(left, right reservation) int {
		return cmp.Compare(left.generation, right.generation)
	})
	preempted := make([]attemptGeneration, 0, len(reservations))
	for _, reserved := range reservations {
		launchBy := request.At.Add(driver.launchProgress)
		driver.attempts[reserved.generation] = &supervisorDrivenAttempt{
			spec: reserved.spec, terminal: make(chan Terminal, 1), launchBy: launchBy,
			launchWake: make(chan struct{}, 1), launchPublished: make(chan struct{}), preempted: true,
		}
		actions := driver.reduceLocked(supervisorEvent{
			kind: supervisorProspectiveRegistered, generation: reserved.generation,
			attempt: attemptIdentity(reserved.spec.Attempt), at: request.At,
			launchBy: launchBy, profile: reserved.spec.Profile, commandDeadline: reserved.spec.Deadline,
		})
		if len(actions) != 1 || actions[0].kind != supervisorLaunchNative {
			invariant(supervisorDriverOperation, "reserved launch registration did not issue native launch")
		}
		attempt := driver.requireAttempt(reserved.generation)
		attempt.launchAction = actions[0]
		completion := supervisorLaunchCompletion{
			generation: reserved.generation, action: actions[0].token, at: request.At,
			kind: supervisorLaunchProvenNotReleased, failure: LaunchFailed,
		}
		attempt.launchEvent = &supervisorEvent{
			kind: supervisorLaunchCompleted, generation: reserved.generation,
			at: request.At, completion: &completion,
		}
		delete(driver.reservations, reserved.cell)
		preempted = append(preempted, reserved.generation)
	}

	return preempted
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
	state := driver.supervisorState()
	driver.mutex.Unlock()
	executor := supervisorEmergencyExecutor{
		settleEmergency: func(sweep emergencySweep) emergencySettlement {
			settled := driver.runtime.SettleEmergency(processRuntimeResolutions(sweep))
			settlement := runtimeEmergencySettlement(settled)
			driver.mutex.Lock()
			if driver.emergencyReady {
				driver.mutex.Unlock()
				invariant(supervisorDriverOperation, "emergency runtime receipt was duplicated")
			}
			driver.emergencyReceipt = settled
			driver.emergencyReady = true
			driver.mutex.Unlock()

			return settlement
		},
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
	runtimeKind supervisorRuntimeReceiptKind,
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
		Failures:                diagnostics.failures,
		profile:                 evidence.profile,
		confirmationProvisional: runtimeKind == supervisorRuntimeProvisionalDeadline,
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

func publicTerminalFromEffect(
	effect supervisionEffect,
	readOutput func(supervisorOutputRef) string,
	readDiagnostic func(supervisorDiagnosticRef) error,
) Terminal {
	evidence, runtimeKind, ok := effect.TerminalEvidence()
	if !ok {
		invariant(supervisorDriverOperation, "effect cannot publish a terminal")
	}

	return publicTerminal(evidence.production(), readOutput, readDiagnostic, runtimeKind)
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
		if err == nil {
			err = diagnostics.drain
		}
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
