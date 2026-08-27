package ooze

import (
	"sync"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

const startInstallerOperation, startLaunchOperation = "start committed installer", "start committed launch"

type processRuntimeOperation uint8

const (
	processRuntimeRegisterCampaign processRuntimeOperation = iota + 1
	processRuntimeRequestAdmission
	processRuntimeCancelAdmission
	processRuntimeAcknowledgeGrantReturn
	processRuntimeBindConfirmationBarrier
	processRuntimeCompleteConfirmationQueue
	processRuntimeStartCommitted
	processRuntimeObserveAttempt
	processRuntimeSettleEmergency
	processRuntimeCommitTerminal
	processRuntimeAuthorizeForcedAbort
	processRuntimeClose
)

type runtimeEventData struct {
	operation      processRuntimeOperation
	name           string
	provenance     campaignProvenance
	campaign       campaignToken
	request        admissionRequest
	requestToken   admissionRequestToken
	grant          admissionGrant
	barrier        barrierBinding
	sweep          emergencySweep
	fatalCause     runtimeFatalCause
	fatalEpoch     fatalEpochID
	generation     attemptGeneration
	observation    attemptObservation
	registration   campaignRegistration
	admission      admissionResult
	barrierResult  barrierResult
	queue          confirmationQueueResult
	start          startCommittedResult
	observed       observationResult
	emergency      emergencySettlement
	terminal       terminalResult
	runtimeClosure runtimeClosure
}

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

func (cell *pendingStartCell) installedGeneration() attemptGeneration {
	cell.mutex.Lock()
	defer cell.mutex.Unlock()

	return cell.generation
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
	mutex         sync.Mutex
	core          processRuntime
	emergency     chan struct{}
	observer      processruntime.Observer
	notifications runtimeNotificationQueue
	publication   runtimePublicationQueue
}

type runtimeNotificationQueue struct {
	active bool
	values []runtimeNotification
}

type runtimeNotification struct {
	delivery  chan admissionGrant
	grant     admissionGrant
	closeOnly bool
}

type runtimePublicationQueue struct {
	mutex    sync.Mutex
	draining bool
	values   []runtimePublication
}

type runtimePublication struct {
	event         processruntime.Event
	observed      bool
	notifications []runtimeNotification
	emergency     bool
}

func newProcessRuntimeShell(capacity int) *processRuntimeShell {
	return &processRuntimeShell{core: newProcessRuntime(capacity), emergency: make(chan struct{})}
}

func newProcessRuntimeShellWithObserver(capacity int, observer processruntime.Observer) *processRuntimeShell {
	shell := newProcessRuntimeShell(capacity)
	shell.observer = observer
	return shell
}

func (s *processRuntimeShell) runtimeEmergency() <-chan struct{} { return s.emergency }

func (s *processRuntimeShell) registerCampaign(p campaignProvenance) campaignRegistration {
	return applyCore(s, "register campaign", processRuntimeRegisterCampaign, p, processRuntime.registerCampaign)
}

func (s *processRuntimeShell) requestAdmission(request admissionRequest) admissionAwait {
	var recorded admissionResult

	return underRuntimeLock(s, "request admission", func(admissionAwait, processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeRequestAdmission, request: request, admission: recorded,
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
			s.closeDelivery(delivery)
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
	var recorded admissionResult

	return underRuntimeLock(s, "cancel admission", func(admissionResult, processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeCancelAdmission, requestToken: token, admission: recorded,
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
		recorded = result
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
		s, "acknowledge grant return", processRuntimeAcknowledgeGrantReturn,
		grant, processRuntime.acknowledgeGrantReturn,
	)
}

func (s *processRuntimeShell) sealAndBindConfirmationBarrier(binding barrierBinding) barrierAwait {
	var recorded barrierResult

	return underRuntimeLock(s, "bind confirmation barrier", func(barrierAwait, processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeBindConfirmationBarrier, barrier: binding, barrierResult: recorded,
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
			s.closeDelivery(delivery)
		}

		return barrierAwait{decision: result.decision, request: result.request, delivery: delivery}
	})
}

func (s *processRuntimeShell) completeConfirmationQueue(campaign campaignToken) confirmationQueueResult {
	var recorded confirmationQueueResult

	return underRuntimeLock(s, "complete confirmation queue", func(confirmationQueueResult, processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeCompleteConfirmationQueue, campaign: campaign, queue: recorded,
		}
	}, func() (result confirmationQueueResult) {
		s.core, result = s.core.completeConfirmationQueue(campaign)
		recorded = result
		s.deliver(result.deliveries)
		result.deliveries = nil

		return result
	})
}

func (s *processRuntimeShell) startCommitted(grant admissionGrant, installation startInstallation) preparedStart {
	return underRuntimeLock(s, startInstallerOperation, func(result preparedStart, _ processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeStartCommitted, grant: grant, start: result.result,
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
	var recorded observationResult

	return underRuntimeLock(s, observeOperation, func(observationResult, processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeObserveAttempt, generation: generation,
			observation: observed, observed: recorded,
		}
	}, func() (result observationResult) {
		s.core, result = s.core.observeAttempt(generation, observed)
		recorded = result
		s.closeWaiting(result.cancelledWaiting)
		s.assertReturnable(result.compensatedGrants)
		s.deliver(result.deliveries)
		result.deliveries = nil

		return result
	})
}

func (s *processRuntimeShell) settleEmergency(sweep emergencySweep) emergencySettlement {
	return applyCore(s, settleEmergencyOperation, processRuntimeSettleEmergency, sweep, processRuntime.settleEmergency)
}

func (s *processRuntimeShell) commitTerminal(campaign campaignToken) terminalResult {
	return applyCore(s, "commit terminal", processRuntimeCommitTerminal, campaign, processRuntime.commitTerminal)
}

func (s *processRuntimeShell) authorizeForcedAbort(campaign campaignToken, epoch fatalEpochID) terminalResult {
	return underRuntimeLock(s, "authorize forced abort", func(result terminalResult, _ processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeAuthorizeForcedAbort, campaign: campaign, fatalEpoch: epoch, terminal: result,
		}
	}, func() (result terminalResult) {
		s.core, result = s.core.authorizeForcedAbort(campaign, epoch)

		return result
	})
}

func (s *processRuntimeShell) closeRuntime(cause runtimeFatalCause) runtimeClosure {
	return underRuntimeLock(s, "close runtime", func(result runtimeClosure, _ processRuntime) runtimeEventData {
		return runtimeEventData{
			operation: processRuntimeClose, fatalCause: cause, runtimeClosure: result,
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
		s.closeDelivery(request.delivery)
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
	record func(T, processRuntime) runtimeEventData,
	apply func() T,
) (result T) {
	shell.mutex.Lock()
	shell.beginRuntimeNotifications()
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
			shell.enqueueRuntimePublication(runtimePublication{
				notifications: shell.takeRuntimeNotifications(),
				emergency:     wasOpen && !shell.core.open(),
			})
			shell.mutex.Unlock()
			shell.drainRuntimePublications()
			panic(violation)
		}
	}()

	result = apply()
	transition := runtimeEventData{name: operation}
	if record != nil {
		transition = record(result, shell.core)
		transition.name = operation
	}
	publication := runtimePublication{
		notifications: shell.takeRuntimeNotifications(),
		emergency:     wasOpen && !shell.core.open(),
	}
	if record != nil {
		publication.event = processRuntimeEvent(transition)
		publication.observed = true
	}
	shell.enqueueRuntimePublication(publication)
	shell.mutex.Unlock()
	shell.drainRuntimePublications()

	return result
}

func (s *processRuntimeShell) enqueueRuntimePublication(publication runtimePublication) {
	s.publication.mutex.Lock()
	s.publication.values = append(s.publication.values, publication)
	s.publication.mutex.Unlock()
}

func (s *processRuntimeShell) drainRuntimePublications() {
	s.publication.mutex.Lock()
	if s.publication.draining {
		s.publication.mutex.Unlock()

		return
	}
	s.publication.draining = true
	for len(s.publication.values) != 0 {
		publication := s.publication.values[0]
		s.publication.values = s.publication.values[1:]
		s.publication.mutex.Unlock()

		s.observeRuntimeEvent(publication)
		s.deliverRuntimeNotifications(publication.notifications)
		if publication.emergency {
			close(s.emergency)
		}

		s.publication.mutex.Lock()
	}
	s.publication.draining = false
	s.publication.mutex.Unlock()
}

func (s *processRuntimeShell) observeRuntimeEvent(publication runtimePublication) {
	if !publication.observed || s.observer == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = s.observer.Observe(publication.event)
}

func applyCore[I, O any](
	s *processRuntimeShell,
	operationName string,
	operation processRuntimeOperation,
	input I,
	reduce func(processRuntime, I) (processRuntime, O),
) O {
	return underRuntimeLock(s, operationName, func(result O, _ processRuntime) runtimeEventData {
		return runtimeEventDataFor(operation, input, result)
	}, func() (result O) {
		s.core, result = reduce(s.core, input)

		return result
	})
}

func runtimeEventDataFor[I, O any](
	operation processRuntimeOperation,
	input I,
	output O,
) runtimeEventData {
	transition := runtimeEventData{operation: operation}
	switch operation {
	case processRuntimeRegisterCampaign:
		transition.provenance = any(input).(campaignProvenance)
		transition.registration = any(output).(campaignRegistration)
	case processRuntimeAcknowledgeGrantReturn:
		transition.grant = any(input).(admissionGrant)
		transition.admission = any(output).(admissionResult)
	case processRuntimeSettleEmergency:
		transition.sweep = any(input).(emergencySweep)
		transition.emergency = any(output).(emergencySettlement)
	case processRuntimeCommitTerminal:
		transition.campaign = any(input).(campaignToken)
		transition.terminal = any(output).(terminalResult)
	default:
		invariant("record runtime", "runtime operation has no typed transition")
	}

	return transition
}

func processRuntimeEvent(transition runtimeEventData) processruntime.Event {
	switch transition.operation {
	case 0:
		return nil
	case processRuntimeRegisterCampaign:
		event, err := processruntime.NewCampaignRegistrationProcessed(
			uint64(transition.provenance.lineage), runtimeEventRegistration(transition.registration),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeRequestAdmission:
		event, err := processruntime.NewAdmissionRequestProcessed(
			runtimeEventAdmission(transition.request), runtimeEventAdmissionResult(transition.admission),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeCancelAdmission:
		event, err := processruntime.NewAdmissionCancellationProcessed(
			runtimeEventAdmission(transition.requestToken), runtimeEventAdmissionResult(transition.admission),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeAcknowledgeGrantReturn:
		event, err := processruntime.NewGrantReturnProcessed(
			runtimeEventAdmission(transition.grant), runtimeEventAdmissionResult(transition.admission),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeBindConfirmationBarrier:
		event, err := processruntime.NewConfirmationBarrierProcessed(processruntime.Admission{
			Campaign: runtimeEventCampaign(transition.barrier.campaign), Attempt: string(transition.barrier.attempt),
			Class: processruntime.AdmissionClass(confirmationBarrierAdmission), Profile: processruntime.Profile(transition.barrier.profile),
			Deadline: int64(transition.barrier.deadline),
		}, runtimeEventBarrierResult(transition.barrierResult))
		return validRuntimeEvent(event, err)
	case processRuntimeCompleteConfirmationQueue:
		event, err := processruntime.NewConfirmationQueueProcessed(
			runtimeEventCampaign(transition.campaign), runtimeEventQueueResult(transition.queue),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeStartCommitted:
		event, err := processruntime.NewStartCommitmentProcessed(
			runtimeEventAdmission(transition.grant), processruntime.StartResult{
				Decision: processruntime.StartDecision(transition.start.decision), Generation: uint64(transition.start.generation),
				SettlementAcknowledged:   transition.start.settlementAcknowledged,
				RuntimeClosureInProgress: transition.start.runtimeClosureInProgress,
			})
		return validRuntimeEvent(event, err)
	case processRuntimeObserveAttempt:
		event, err := processruntime.NewAttemptObservationProcessed(
			uint64(transition.generation), runtimeEventObservation(transition.observation),
			runtimeEventObservationResult(transition.observed),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeSettleEmergency:
		event, err := processruntime.NewEmergencySettlementProcessed(
			runtimeEventResolutions(transition.sweep.resolutions), runtimeEventEmergency(transition.emergency),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeCommitTerminal:
		event, err := processruntime.NewTerminalCommitmentProcessed(
			runtimeEventCampaign(transition.campaign), runtimeEventTerminal(transition.terminal),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeAuthorizeForcedAbort:
		event, err := processruntime.NewForcedAbortProcessed(
			runtimeEventCampaign(transition.campaign), uint64(transition.fatalEpoch),
			runtimeEventTerminal(transition.terminal),
		)
		return validRuntimeEvent(event, err)
	case processRuntimeClose:
		event, err := processruntime.NewRuntimeClosureProcessed(
			string(transition.fatalCause), runtimeEventClosure(transition.runtimeClosure),
		)
		return validRuntimeEvent(event, err)
	default:
		invariant("publish runtime event", "runtime operation has no domain event")
	}
	return nil
}

func validRuntimeEvent(event processruntime.Event, err error) processruntime.Event {
	if err != nil {
		invariant("publish runtime event", err.Error())
	}
	return event
}

func runtimeEventCampaign(campaign campaignToken) processruntime.Campaign {
	return processruntime.Campaign{ID: uint64(campaign.id), Lineage: uint64(campaign.lineage)}
}

func runtimeEventAdmission(admission admissionAuthority) processruntime.Admission {
	return processruntime.Admission{
		Campaign: runtimeEventCampaign(admission.campaign), Attempt: string(admission.attempt),
		Class: processruntime.AdmissionClass(admission.class), Profile: processruntime.Profile(admission.profile),
		Deadline: int64(admission.deadline),
	}
}

func runtimeEventAdmissions[Values ~[]admissionAuthority](values Values) []processruntime.Admission {
	result := make([]processruntime.Admission, len(values))
	for index, value := range values {
		result[index] = runtimeEventAdmission(value)
	}
	return result
}

func runtimeEventRegistration(registration campaignRegistration) processruntime.Registration {
	return processruntime.Registration{
		Decision: processruntime.RegistrationDecision(registration.decision), Campaign: runtimeEventCampaign(registration.token),
	}
}

func runtimeEventAdmissionResult(result admissionResult) processruntime.AdmissionResult {
	return processruntime.AdmissionResult{
		Decision: processruntime.AdmissionDecision(result.decision), Request: runtimeEventAdmission(result.request),
		Deliveries: runtimeEventAdmissions(result.deliveries), FatalEpoch: uint64(result.fatalEpoch),
	}
}

func runtimeEventBarrierResult(result barrierResult) processruntime.BarrierResult {
	return processruntime.BarrierResult{
		Decision: processruntime.BarrierDecision(result.decision), Request: runtimeEventAdmission(result.request),
		Deliveries: runtimeEventAdmissions(result.deliveries),
	}
}

func runtimeEventQueueResult(result confirmationQueueResult) processruntime.QueueResult {
	return processruntime.QueueResult{
		Decision: processruntime.QueueDecision(result.decision), Deliveries: runtimeEventAdmissions(result.deliveries),
	}
}

func runtimeEventObservation(observation attemptObservation) processruntime.Observation {
	switch observation := observation.(type) {
	case launchOwned:
		return processruntime.Observation{Kind: processruntime.LaunchOwned}
	case launchNotReleased:
		return processruntime.Observation{Kind: processruntime.LaunchNotReleased, Reason: processruntime.LaunchFailure(observation.reason)}
	case attemptSettled:
		return processruntime.Observation{Kind: processruntime.AttemptSettled, Profile: processruntime.Profile(observation.profile), Deadline: int64(observation.deadline)}
	case attemptTripped:
		return processruntime.Observation{Kind: processruntime.AttemptTripped, Trip: processruntime.TripKind(observation.kind), Profile: processruntime.Profile(observation.profile), Deadline: int64(observation.deadline)}
	case launchUnconfirmed:
		return processruntime.Observation{Kind: processruntime.LaunchUnconfirmed}
	case drainUnconfirmed:
		return processruntime.Observation{Kind: processruntime.DrainUnconfirmed}
	case attemptStopped:
		return processruntime.Observation{Kind: processruntime.AttemptStopped}
	case attemptInfrastructure:
		return processruntime.Observation{Kind: processruntime.AttemptInfrastructure, Cause: observation.cause}
	default:
		return processruntime.Observation{}
	}
}

func runtimeEventObservationResult(result observationResult) processruntime.ObservationResult {
	return processruntime.ObservationResult{
		Generation: uint64(result.generation), Deliveries: runtimeEventAdmissions(result.deliveries),
		CancelledWaiting: runtimeEventAdmissions(result.cancelledWaiting), CompensatedGrants: runtimeEventAdmissions(result.compensatedGrants),
		SettlementAcknowledged: result.settlementAcknowledged, ConfirmationProvisional: result.confirmationProvisional,
		PressureTransitioned: result.pressureTransitioned, RuntimeClosureInProgress: result.runtimeClosureInProgress,
		ConfirmationObserved: result.confirmationObserved, ConfirmationQueueDrained: result.confirmationQueueDrained,
		FatalEpoch: uint64(result.fatalEpoch),
	}
}

func runtimeEventResolutions(values []emergencyResolution) []processruntime.Resolution {
	result := make([]processruntime.Resolution, len(values))
	for index, value := range values {
		result[index] = processruntime.Resolution{Generation: uint64(value.generation), Disposition: processruntime.EmergencyDisposition(value.disposition)}
	}
	return result
}

func runtimeEventResiduals(values []residualCustody) []processruntime.Residual {
	result := make([]processruntime.Residual, len(values))
	for index, value := range values {
		result[index] = processruntime.Residual{
			Generation: uint64(value.generation), Attempt: string(value.attempt),
			Stage: processruntime.AdmissionStage(value.stage), Transferred: value.transferred,
		}
	}
	return result
}

func runtimeEventEmergency(result emergencySettlement) processruntime.EmergencyResult {
	acknowledged := make([]uint64, len(result.acknowledged))
	for index, generation := range result.acknowledged {
		acknowledged[index] = uint64(generation)
	}
	return processruntime.EmergencyResult{
		Epoch: uint64(result.epoch), Owner: runtimeEventCampaign(result.owner),
		Acknowledged: acknowledged, Residual: runtimeEventResiduals(result.residual),
	}
}

func runtimeEventTerminal(result terminalResult) processruntime.TerminalResult {
	return processruntime.TerminalResult{Decision: processruntime.TerminalDecision(result.decision), Epoch: uint64(result.epoch)}
}

func runtimeEventClosure(result runtimeClosure) processruntime.ClosureResult {
	return processruntime.ClosureResult{
		Epoch: uint64(result.epoch), CancelledWaiting: runtimeEventAdmissions(result.cancelledWaiting),
		CompensatedGrants: runtimeEventAdmissions(result.compensatedGrants),
		Residual:          runtimeEventResiduals(result.residual),
	}
}

func (s *processRuntimeShell) deliver(deliveries []admissionGrant) {
	for _, grant := range deliveries {
		if grant.delivery == nil {
			invariant("deliver grant", "request has no delivery")
		}
	}
	if s.notifications.active {
		for _, grant := range deliveries {
			s.notifications.values = append(s.notifications.values, runtimeNotification{
				delivery: grant.delivery, grant: grant,
			})
		}

		return
	}
	for _, grant := range deliveries {
		grant.delivery <- grant
		close(grant.delivery)
	}
}

func (s *processRuntimeShell) closeDelivery(delivery chan admissionGrant) {
	if delivery == nil {
		invariant("close admission delivery", "delivery is nil")
	}
	if s.notifications.active {
		s.notifications.values = append(s.notifications.values, runtimeNotification{
			delivery: delivery, closeOnly: true,
		})

		return
	}
	close(delivery)
}

func (s *processRuntimeShell) beginRuntimeNotifications() {
	if s.notifications.active || len(s.notifications.values) != 0 {
		invariant("begin runtime notifications", "prior operation left notifications pending")
	}
	s.notifications.active = true
}

func (s *processRuntimeShell) takeRuntimeNotifications() []runtimeNotification {
	if !s.notifications.active {
		return nil
	}
	notifications := s.notifications.values
	s.notifications = runtimeNotificationQueue{}

	return notifications
}

func (s *processRuntimeShell) deliverRuntimeNotifications(notifications []runtimeNotification) {
	for _, notification := range notifications {
		if !notification.closeOnly {
			notification.delivery <- notification.grant
		}
		close(notification.delivery)
	}
}
