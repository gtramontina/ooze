package ooze

import (
	"sync"
	"time"
)

const supervisorDriverOperation = "drive supervisor"

type supervisorDriverConstruction struct {
	runtime        *processRuntimeShell
	now            func() time.Time
	launchProgress time.Duration
	drainEpoch     time.Duration
	execute        func(supervisorAction) *supervisorEvent
	readOutput     func(supervisorOutputRef) string
}

type supervisorDrivenAttempt struct {
	spec         Spec
	owned        *OwnedAttempt
	launchResult LaunchResult
	waitAction   supervisorAction
	sampleAction supervisorAction
	waitStarted  bool
	terminal     chan Terminal
}

type supervisorDriver struct {
	mutex          sync.Mutex
	state          supervisorState
	runtime        *processRuntimeShell
	now            func() time.Time
	launchProgress time.Duration
	drainEpoch     time.Duration
	execute        func(supervisorAction) *supervisorEvent
	readOutput     func(supervisorOutputRef) string
	attempts       map[attemptGeneration]*supervisorDrivenAttempt
}

func newSupervisorDriver(construction supervisorDriverConstruction) *supervisorDriver {
	if construction.runtime == nil || construction.now == nil || construction.launchProgress <= 0 ||
		construction.drainEpoch <= 0 || construction.execute == nil || construction.readOutput == nil {
		panic("supervisor driver construction is incomplete")
	}

	return &supervisorDriver{
		runtime: construction.runtime, now: construction.now,
		launchProgress: construction.launchProgress, drainEpoch: construction.drainEpoch,
		execute: construction.execute, readOutput: construction.readOutput,
		attempts: make(map[attemptGeneration]*supervisorDrivenAttempt),
	}
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

func (driver *supervisorDriver) apply(event supervisorEvent) {
	driver.mutex.Lock()
	next, actions := reduceSupervisor(driver.state, event)
	driver.state = next
	driver.mutex.Unlock()
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
	driver.mutex.Unlock()
	driver.executeAction(waitAction)

	return <-terminal
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
	driver.apply(supervisorEvent{
		kind: supervisorRunningObserved, generation: generation,
		at: request.At, drainBy: request.DrainBy, running: &bundle,
	})
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
	driver.mutex.Unlock()
	delivery <- terminal
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
