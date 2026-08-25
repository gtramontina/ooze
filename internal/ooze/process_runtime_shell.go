//nolint:exhaustruct,lll,nonamedreturns // Zero states and guarded results encode expected transitions.
package ooze

import "sync"

const startInstallerOperation, startLaunchOperation = "start committed installer", "start committed launch"

type oneShotAwait[Decision any] struct {
	decision Decision
	request  admissionRequestToken
	delivery <-chan admissionGrant
	fatal    fatalEpochID
}

type (
	admissionAwait = oneShotAwait[admissionDecision]
	barrierAwait   = oneShotAwait[barrierDecision]
)

type pendingStartCell struct {
	mutex      sync.Mutex
	generation attemptGeneration
	launched   bool
}

type startInstallation struct {
	grant admissionGrant
	cell  *pendingStartCell
}

type installedStart struct {
	generation attemptGeneration
	cell       *pendingStartCell
	shell      *processRuntimeShell
}

type preparedStart struct {
	result startCommittedResult
	start  installedStart
}

// install mutates only the broker cell; only its narrower return value can launch.
func (installation startInstallation) install(generation attemptGeneration, shell *processRuntimeShell) installedStart {
	if generation == 0 || installation.cell == nil || shell == nil {
		invariant(startInstallerOperation, "generation or cell is zero")
	}
	installation.cell.mutex.Lock()
	defer installation.cell.mutex.Unlock()
	if installation.cell.generation != 0 {
		invariant(startInstallerOperation, "installation cell was already used")
	}
	installation.cell.generation = generation

	return installedStart{generation: generation, cell: installation.cell, shell: shell}
}

func (start installedStart) launch(dormant func(attemptGeneration) attemptObservation) (observed attemptObservation) {
	fatalGeneration := start.generation
	defer func() {
		recovered := recover()
		if recovered == nil && observed != nil {
			return
		}
		violation, ok := recovered.(runtimeInvariantViolation)
		if !ok {
			violation = runtimeInvariantViolation{operation: startLaunchOperation, reason: "launch thunk panicked or returned nil"}
		}
		start.fail(fatalGeneration, violation)
	}()
	var violation runtimeInvariantViolation
	fatalGeneration, violation = start.claimLaunch(dormant == nil)
	if violation.reason != "" {
		panic(violation)
	}

	return dormant(start.generation)
}

func (start installedStart) claimLaunch(dormantNil bool) (attemptGeneration, runtimeInvariantViolation) {
	if start.cell == nil {
		return start.generation, runtimeInvariantViolation{operation: startLaunchOperation, reason: "start or launch is zero"}
	}
	start.cell.mutex.Lock()
	defer start.cell.mutex.Unlock()
	fatalGeneration := start.generation
	if start.cell.generation != 0 {
		fatalGeneration = start.cell.generation
	}
	if start.generation == 0 || dormantNil {
		return fatalGeneration, runtimeInvariantViolation{operation: startLaunchOperation, reason: "start or launch is zero"}
	}
	if start.cell.generation != start.generation || start.cell.launched {
		return fatalGeneration, runtimeInvariantViolation{
			operation: startLaunchOperation, reason: "start generation mismatched or was reused",
		}
	}
	start.cell.launched = true

	return fatalGeneration, runtimeInvariantViolation{}
}

func (start installedStart) fail(generation attemptGeneration, violation runtimeInvariantViolation) {
	if start.shell == nil {
		panic(violation)
	}
	start.shell.failLaunchInvariant(generation, violation)
}

type processRuntimeShell struct {
	mutex     sync.Mutex
	core      processRuntime
	emergency chan struct{}
	recorder  *simulationRecorder
}

func newProcessRuntimeShell(capacity int) *processRuntimeShell {
	return &processRuntimeShell{core: newProcessRuntime(capacity), emergency: make(chan struct{})}
}

func newProcessRuntimeShellWithRecorder(capacity int, recorder *simulationRecorder) *processRuntimeShell {
	shell := newProcessRuntimeShell(capacity)
	shell.recorder = recorder

	return shell
}

func (s *processRuntimeShell) runtimeEmergency() <-chan struct{} { return s.emergency }

func (s *processRuntimeShell) registerCampaign(p campaignProvenance) campaignRegistration {
	return applyCore(s, "register campaign", simulationRegisterCampaign, p, processRuntime.registerCampaign)
}

func (s *processRuntimeShell) requestAdmission(request admissionRequest) admissionAwait {
	var recorded admissionResult

	return underRuntimeLock(s, "request admission", func(result admissionAwait, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation:    simulationRequestAdmission,
			runtimeAdmission:    simulationTraceAdmission(request),
			runtimeAdmissionOut: simulationTraceAdmissionResult(recorded),
		}
	}, func() admissionAwait {
		delivery := make(chan admissionGrant, 1)
		request.delivery = delivery
		var result admissionResult
		s.core, result = s.core.requestAdmission(request)
		recorded = result
		if result.decision == admissionAccepted {
			s.deliver(result.deliveries)
		} else {
			close(delivery)
		}

		return admissionAwait{
			decision: result.decision, request: result.request, delivery: delivery, fatal: result.fatalEpoch,
		}
	})
}

func (s *processRuntimeShell) fatalEpoch() fatalEpochID {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.core.fatalEpoch
}

func (s *processRuntimeShell) emergencySettlementRequired() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.core.lifecycle == runtimeFatalClosing
}

func (s *processRuntimeShell) cancelAdmission(token admissionRequestToken) admissionResult {
	return underRuntimeLock(s, "cancel admission", func(result admissionResult, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation:      simulationCancelAdmission,
			runtimeAdmissionToken: simulationTraceAdmission(token),
			runtimeAdmissionOut:   simulationTraceAdmissionResult(result),
		}
	}, func() (result admissionResult) {
		if token.delivery == nil {
			for _, admission := range s.core.admissions {
				if sameAdmissionRequest(campaignAdmissionFact(admission.grant), campaignAdmissionFact(token)) {
					token = admission.grant

					break
				}
			}
		}
		s.core, result = s.core.cancelAdmission(token)
		if result.decision == admissionCancelledWaiting {
			s.closeWaiting([]admissionRequestToken{token})
		}
		s.deliver(result.deliveries)
		result.deliveries = nil

		return result
	})
}

func (s *processRuntimeShell) acknowledgeGrantReturn(grant admissionGrant) admissionResult {
	return applyCore(
		s, "acknowledge grant return", simulationAcknowledgeGrantReturn,
		grant, processRuntime.acknowledgeGrantReturn,
	)
}

func (s *processRuntimeShell) sealAndBindConfirmationBarrier(binding barrierBinding) barrierAwait {
	var recorded barrierResult

	return underRuntimeLock(s, "bind confirmation barrier", func(result barrierAwait, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation:  simulationBindConfirmationBarrier,
			runtimeBarrier:    simulationTraceBarrierBinding(binding),
			runtimeBarrierOut: simulationTraceBarrierResult(recorded),
		}
	}, func() barrierAwait {
		delivery := make(chan admissionGrant, 1)
		binding.delivery = delivery
		var result barrierResult
		s.core, result = s.core.sealAndBindConfirmationBarrier(binding)
		recorded = result
		if result.decision == barrierBound {
			s.deliver(result.deliveries)
		} else {
			close(delivery)
		}

		return barrierAwait{decision: result.decision, request: result.request, delivery: delivery}
	})
}

func (s *processRuntimeShell) completeConfirmationQueue(campaign campaignToken) confirmationQueueResult {
	return underRuntimeLock(s, "complete confirmation queue", func(result confirmationQueueResult, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation: simulationCompleteConfirmationQueue, runtimeCampaign: campaign,
			runtimeQueueOut: simulationTraceConfirmationQueueResult(result),
		}
	}, func() (result confirmationQueueResult) {
		s.core, result = s.core.completeConfirmationQueue(campaign)
		s.deliver(result.deliveries)
		result.deliveries = nil

		return result
	})
}

// No executable value enters this lock; only the post-unlock return accepts a native thunk.
func (s *processRuntimeShell) startCommitted(grant admissionGrant, installation startInstallation) preparedStart {
	return underRuntimeLock(s, startInstallerOperation, func(result preparedStart, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation: simulationStartCommitted, runtimeGrant: simulationTraceAdmission(grant),
			runtimeStart: result.result,
		}
	}, func() preparedStart {
		if installation.grant != grant {
			invariant(startInstallerOperation, "installation grant is stale or wrong")
		}
		var result startCommittedResult
		next, result := s.core.startCommitted(grant)
		if result.decision != startCommittedAccepted {
			return preparedStart{result: result}
		}
		start := installation.install(result.generation, s)
		s.core = next

		return preparedStart{result: result, start: start}
	})
}

func (s *processRuntimeShell) failLaunchInvariant(generation attemptGeneration, violation runtimeInvariantViolation) {
	underRuntimeLock(s, violation.operation, nil, func() struct{} {
		wasOpen := s.core.open()
		cause := runtimeFatalCause(violation.reason)
		if generation != 0 {
			cause = attemptFatalCause("launch invariant: "+violation.reason, generation)
		}
		closure := s.closeCore(cause)
		if wasOpen && s.core.lifecycle == runtimeFatalClosing {
			resolutions := make([]emergencyResolution, len(closure.residual))
			for index, residual := range closure.residual {
				resolutions[index] = emergencyResolution{
					generation: residual.generation, disposition: emergencyCustodyTransferred,
				}
			}
			s.core, _ = s.core.settleEmergency(emergencySweep{resolutions: resolutions})
		}

		return struct{}{}
	})
	panic(violation)
}

func (s *processRuntimeShell) observeAttempt(generation attemptGeneration, observed attemptObservation) observationResult {
	return underRuntimeLock(s, observeOperation, func(result observationResult, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation: simulationObserveAttempt, runtimeGeneration: generation,
			runtimeObservation:    simulationTraceObservation(observed),
			runtimeObservationOut: simulationTraceObservationResult(result),
		}
	}, func() (result observationResult) {
		s.core, result = s.core.observeAttempt(generation, observed)
		s.closeWaiting(result.cancelledWaiting)
		s.assertReturnable(result.compensatedGrants)
		s.deliver(result.deliveries)
		result.deliveries = nil

		return result
	})
}

func (s *processRuntimeShell) settleEmergency(sweep emergencySweep) emergencySettlement {
	return applyCore(s, settleEmergencyOperation, simulationSettleEmergency, sweep, processRuntime.settleEmergency)
}

func (s *processRuntimeShell) commitTerminal(campaign campaignToken) terminalResult {
	return applyCore(s, "commit terminal", simulationCommitTerminal, campaign, processRuntime.commitTerminal)
}

func (s *processRuntimeShell) authorizeForcedAbort(campaign campaignToken, epoch fatalEpochID) terminalResult {
	return underRuntimeLock(s, "authorize forced abort", func(result terminalResult, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation: simulationAuthorizeForcedAbort, runtimeCampaign: campaign,
			runtimeFatalEpoch: epoch, runtimeTerminal: result,
		}
	}, func() (result terminalResult) {
		s.core, result = s.core.authorizeForcedAbort(campaign, epoch)

		return result
	})
}

func (s *processRuntimeShell) closeRuntime(cause runtimeFatalCause) runtimeClosure {
	return underRuntimeLock(s, "close runtime", func(result runtimeClosure, _ processRuntime) simulationRecord {
		return simulationRecord{
			runtimeOperation: simulationCloseRuntime, runtimeFatalCause: cause,
			runtimeClosure: simulationTraceRuntimeClosure(result),
		}
	}, func() runtimeClosure { return s.closeCore(cause) })
}

func (s *processRuntimeShell) closeCore(cause runtimeFatalCause) (result runtimeClosure) {
	s.core, result = s.core.closeRuntime(cause)
	s.closeWaiting(result.cancelledWaiting)
	s.assertReturnable(result.compensatedGrants)

	return result
}

func (s *processRuntimeShell) closeWaiting(requests []admissionRequestToken) {
	for _, request := range requests {
		if request.delivery == nil {
			invariant("close waiting admission", "cancelled waiter has no delivery")
		}
		close(request.delivery)
	}
}

func (s *processRuntimeShell) assertReturnable(grants []admissionGrant) {
	for _, grant := range grants {
		if grant.delivery == nil {
			invariant("emit grant return", "grant has no return acknowledgement")
		}
	}
}

func underRuntimeLock[T any](
	shell *processRuntimeShell,
	operation string,
	record func(T, processRuntime) simulationRecord,
	apply func() T,
) (result T) {
	leaveRecorder := shell.recorder.enter()
	defer leaveRecorder()
	shell.mutex.Lock()
	reservation := shell.recorder.reserve(simulationRuntimeAuthority)
	wasOpen := shell.core.open()
	defer func() {
		if recovered := recover(); recovered != nil {
			violation, ok := recovered.(runtimeInvariantViolation)
			if !ok {
				violation = runtimeInvariantViolation{operation: operation, reason: "unexpected panic"}
			}
			if shell.core.lifecycle <= runtimeFatalSettledClosing {
				shell.closeCore(runtimeFatalCause(violation.reason))
			}
			shell.broadcastEmergency(wasOpen)
			shell.mutex.Unlock()
			panic(violation)
		}
		shell.broadcastEmergency(wasOpen)
		shell.mutex.Unlock()
	}()

	result = apply()
	runtimeRecord := simulationRecord{runtimeOperationName: operation}
	if record != nil {
		runtimeRecord = record(result, shell.core)
		runtimeRecord.runtimeOperationName = operation
	}
	shell.recorder.recordRuntime(reservation, runtimeRecord, shell.core)

	return result
}

func (s *processRuntimeShell) broadcastEmergency(wasOpen bool) {
	if !wasOpen || s.core.open() {
		return
	}
	if s.emergency == nil {
		invariant("broadcast runtime emergency", "process-wide channel is nil")
	}
	close(s.emergency)
}

func applyCore[I, O any](
	s *processRuntimeShell,
	operationName string,
	operation simulationRuntimeOperation,
	input I,
	reduce func(processRuntime, I) (processRuntime, O),
) O {
	return underRuntimeLock(s, operationName, func(result O, _ processRuntime) simulationRecord {
		return simulationRuntimeRecord(operation, input, result)
	}, func() (result O) {
		s.core, result = reduce(s.core, input)

		return result
	})
}

func simulationRuntimeRecord[I, O any](
	operation simulationRuntimeOperation,
	input I,
	result O,
) simulationRecord {
	record := simulationRecord{runtimeOperation: operation}
	switch operation {
	case simulationRegisterCampaign:
		record.runtimeProvenance = any(input).(campaignProvenance)
		record.runtimeRegistration = any(result).(campaignRegistration)
	case simulationAcknowledgeGrantReturn:
		record.runtimeGrant = simulationTraceAdmission(any(input).(admissionGrant))
		record.runtimeAdmissionOut = simulationTraceAdmissionResult(any(result).(admissionResult))
	case simulationSettleEmergency:
		record.runtimeSweep = simulationTraceEmergencySweep(any(input).(emergencySweep))
		record.runtimeEmergencyOut = simulationTraceEmergencySettlement(any(result).(emergencySettlement))
	case simulationCommitTerminal:
		record.runtimeCampaign = any(input).(campaignToken)
		record.runtimeTerminal = any(result).(terminalResult)
	default:
		invariant("record runtime", "runtime operation has no typed trace projection")
	}

	return record
}

func (s *processRuntimeShell) deliver(deliveries []admissionGrant) {
	for _, grant := range deliveries {
		if grant.delivery == nil {
			invariant("deliver grant", "request has no delivery")
		}
		grant.delivery <- grant
		close(grant.delivery)
	}
}
