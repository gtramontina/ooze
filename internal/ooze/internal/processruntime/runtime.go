package processruntime

import "sync"

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
	observer      processRuntimeObserver
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
	event         processRuntimeEvent
	observed      bool
	notifications []runtimeNotification
	emergency     bool
}

func newProcessRuntimeShell(capacity int) *processRuntimeShell {
	return &processRuntimeShell{core: newProcessRuntime(capacity), emergency: make(chan struct{})}
}

func newProcessRuntimeShellWithObserver(capacity int, observer processRuntimeObserver) *processRuntimeShell {
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
				if sameAdmission(admission.grant, token) {
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

func sameAdmission(left, right admissionAuthority) bool {
	return left.campaign == right.campaign && left.attempt == right.attempt && left.class == right.class &&
		left.profile == right.profile && left.deadline == right.deadline
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
		publication.event = buildProcessRuntimeEvent(transition)
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

func buildProcessRuntimeEvent(data runtimeEventData) processRuntimeEvent {
	switch data.operation {
	case processRuntimeRegisterCampaign:
		return runtimeCampaignRegistrationProcessed{provenance: data.provenance, result: data.registration}
	case processRuntimeRequestAdmission:
		return runtimeAdmissionRequestProcessed{
			request: runtimeEventAdmission(data.request), result: runtimeEventAdmissionResult(data.admission),
		}
	case processRuntimeCancelAdmission:
		return runtimeAdmissionCancellationProcessed{
			request: runtimeEventAdmission(data.requestToken), result: runtimeEventAdmissionResult(data.admission),
		}
	case processRuntimeAcknowledgeGrantReturn:
		return runtimeGrantReturnProcessed{
			grant: runtimeEventAdmission(data.grant), result: runtimeEventAdmissionResult(data.admission),
		}
	case processRuntimeBindConfirmationBarrier:
		barrier := data.barrier
		barrier.delivery = nil
		return runtimeConfirmationBarrierProcessed{
			barrier: barrier, result: runtimeEventBarrierResult(data.barrierResult),
		}
	case processRuntimeCompleteConfirmationQueue:
		result := data.queue
		result.deliveries = runtimeEventAdmissions(result.deliveries)
		return runtimeConfirmationQueueProcessed{campaign: data.campaign, result: result}
	case processRuntimeStartCommitted:
		return runtimeStartCommitmentProcessed{
			grant: runtimeEventAdmission(data.grant), result: data.start,
		}
	case processRuntimeObserveAttempt:
		return runtimeAttemptObservationProcessed{
			generation: data.generation, observation: data.observation,
			result: runtimeEventObservationResult(data.observed),
		}
	case processRuntimeSettleEmergency:
		return runtimeEmergencySettlementProcessed{
			sweep:  emergencySweep{resolutions: append([]emergencyResolution(nil), data.sweep.resolutions...)},
			result: runtimeEventEmergency(data.emergency),
		}
	case processRuntimeCommitTerminal:
		return runtimeTerminalCommitmentProcessed{campaign: data.campaign, result: data.terminal}
	case processRuntimeAuthorizeForcedAbort:
		return runtimeForcedAbortProcessed{
			campaign: data.campaign, epoch: data.fatalEpoch, result: data.terminal,
		}
	case processRuntimeClose:
		return runtimeClosureProcessed{cause: data.fatalCause, result: runtimeEventClosure(data.runtimeClosure)}
	default:
		invariant("publish runtime event", "runtime operation has no domain event")
	}
	return nil
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
