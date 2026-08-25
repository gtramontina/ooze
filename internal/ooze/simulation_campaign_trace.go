package ooze

import (
	"errors"
	"slices"
)

type simulationCampaignEventKind uint8

const (
	simulationCampaignRegistered simulationCampaignEventKind = iota + 1
	simulationSnapshotEstablished
	simulationCatalogueDiscovered
	simulationCampaignPreparationFailed
	simulationResourceSettled
	simulationResourceSettlementFailed
	simulationTerminalCommitted
	simulationWorkspaceMaterialized
	simulationWorkspaceMaterializationFailed
	simulationAdmissionGranted
	simulationAdmissionCancelled
	simulationAdmissionRejected
	simulationStartCommittedEvent
	simulationAttemptLaunched
	simulationAttemptTerminal
	simulationConfirmationBarrierBound
	simulationGrantReturnAcknowledged
	simulationRuntimeEmergencySettled
	simulationRuntimeEmergencyStarted
)

type simulationTerminalKind uint8

const (
	simulationSettled simulationTerminalKind = iota + 1
	simulationFuseTrip
	simulationAutomaticDeadlineTrip
	simulationSerialDeadlineTrip
	simulationStopped
	simulationInfrastructure
	simulationDrainUnconfirmed
)

type simulationTerminal struct {
	kind      simulationTerminalKind
	exit      ExitStatus
	data      ExecutionData
	count     ObservedCount
	cause     Cause
	errorText string
	residual  Residual
}

func simulationTraceTerminal(terminal Terminal) simulationTerminal {
	trace := simulationTerminal{data: terminalExecutionData(terminal)}
	switch value := terminal.(type) {
	case Settled:
		trace.kind, trace.exit = simulationSettled, value.Exit
	case Tripped:
		switch trip := value.Trip.(type) {
		case FuseTrip:
			trace.kind, trace.count = simulationFuseTrip, ObservedCount{Value: trip.Live, Present: true}
		case AutomaticDeadlineTrip:
			trace.kind, trace.count = simulationAutomaticDeadlineTrip, trip.Peak
		case SerialDeadlineTrip:
			trace.kind = simulationSerialDeadlineTrip
		default:
			panic("simulation terminal trip is invalid")
		}
	case Stopped:
		trace.kind = simulationStopped
	case Infrastructure:
		trace.kind, trace.cause = simulationInfrastructure, value.Cause
		if value.Err != nil {
			trace.errorText = value.Err.Error()
		}
	case DrainUnconfirmed:
		trace.kind, trace.residual = simulationDrainUnconfirmed, value.Residual
	default:
		panic("simulation terminal is invalid")
	}
	return trace
}

func (trace simulationTerminal) production() Terminal {
	switch trace.kind {
	case simulationSettled:
		return Settled{Exit: trace.exit, ExecutionData: trace.data}
	case simulationFuseTrip:
		return Tripped{Trip: FuseTrip{Live: trace.count.Value}, ExecutionData: trace.data}
	case simulationAutomaticDeadlineTrip:
		return Tripped{Trip: AutomaticDeadlineTrip{Peak: trace.count}, ExecutionData: trace.data}
	case simulationSerialDeadlineTrip:
		return Tripped{Trip: SerialDeadlineTrip{}, ExecutionData: trace.data}
	case simulationStopped:
		return Stopped{ExecutionData: trace.data}
	case simulationInfrastructure:
		var cause error
		if trace.errorText != "" {
			cause = errors.New(trace.errorText)
		}
		return Infrastructure{Cause: trace.cause, Err: cause, ExecutionData: trace.data}
	case simulationDrainUnconfirmed:
		return DrainUnconfirmed{Residual: trace.residual, ExecutionData: trace.data}
	default:
		panic("simulation terminal kind is invalid")
	}
}

type simulationAttemptTerminalEvent struct {
	attempt                  attemptIdentity
	generation               attemptGeneration
	terminal                 simulationTerminal
	receipt                  campaignRuntimeReceipt
	resolvedMutationDeadline mutationDeadlineResolution
}

type simulationCampaignEvent struct {
	id   campaignEventID
	kind simulationCampaignEventKind

	registered                     campaignRegisteredEvent
	snapshotEstablished            snapshotEstablishedEvent
	catalogueDiscovered            catalogueDiscoveredEvent
	preparationFailed              campaignPreparationFailedEvent
	resourceSettled                resourceSettledEvent
	resourceSettlementFailed       resourceSettlementFailedEvent
	terminalCommitted              terminalCommittedEvent
	workspaceMaterialized          workspaceMaterializedEvent
	workspaceMaterializationFailed workspaceMaterializationFailedEvent
	admissionGranted               admissionGrantedEvent
	admissionCancelled             admissionCancelledEvent
	admissionRejected              admissionRejectedEvent
	startCommitted                 startCommittedEvent
	attemptLaunched                attemptLaunchEvent
	attemptTerminal                simulationAttemptTerminalEvent
	confirmationBarrierBound       confirmationBarrierBoundEvent
	grantReturnAcknowledged        grantReturnAcknowledgedEvent
	runtimeEmergencySettled        runtimeEmergencySettledEvent
	runtimeEmergencyStarted        runtimeEmergencyStartedEvent
}

func simulationTraceCampaignEvent(event campaignEvent) simulationCampaignEvent {
	trace := simulationCampaignEvent{id: event.id}
	switch payload := event.payload.(type) {
	case campaignRegisteredEvent:
		trace.kind, trace.registered = simulationCampaignRegistered, payload
	case snapshotEstablishedEvent:
		trace.kind, trace.snapshotEstablished = simulationSnapshotEstablished, payload
	case catalogueDiscoveredEvent:
		payload.mutants = slices.Clone(payload.mutants)
		trace.kind, trace.catalogueDiscovered = simulationCatalogueDiscovered, payload
	case campaignPreparationFailedEvent:
		trace.kind, trace.preparationFailed = simulationCampaignPreparationFailed, payload
	case resourceSettledEvent:
		trace.kind, trace.resourceSettled = simulationResourceSettled, payload
	case resourceSettlementFailedEvent:
		trace.kind, trace.resourceSettlementFailed = simulationResourceSettlementFailed, payload
	case terminalCommittedEvent:
		trace.kind, trace.terminalCommitted = simulationTerminalCommitted, payload
	case workspaceMaterializedEvent:
		trace.kind, trace.workspaceMaterialized = simulationWorkspaceMaterialized, payload
	case workspaceMaterializationFailedEvent:
		payload.artifactResidue = slices.Clone(payload.artifactResidue)
		trace.kind, trace.workspaceMaterializationFailed = simulationWorkspaceMaterializationFailed, payload
	case admissionGrantedEvent:
		trace.kind, trace.admissionGranted = simulationAdmissionGranted, payload
	case admissionCancelledEvent:
		trace.kind, trace.admissionCancelled = simulationAdmissionCancelled, payload
	case admissionRejectedEvent:
		trace.kind, trace.admissionRejected = simulationAdmissionRejected, payload
	case startCommittedEvent:
		trace.kind, trace.startCommitted = simulationStartCommittedEvent, payload
	case attemptLaunchEvent:
		trace.kind, trace.attemptLaunched = simulationAttemptLaunched, payload
	case attemptTerminalEvent:
		trace.kind = simulationAttemptTerminal
		trace.attemptTerminal = simulationAttemptTerminalEvent{
			attempt: payload.attempt, generation: payload.generation,
			terminal: simulationTraceTerminal(payload.terminal), receipt: payload.receipt,
			resolvedMutationDeadline: payload.resolvedMutationDeadline,
		}
	case confirmationBarrierBoundEvent:
		trace.kind, trace.confirmationBarrierBound = simulationConfirmationBarrierBound, payload
	case grantReturnAcknowledgedEvent:
		trace.kind, trace.grantReturnAcknowledged = simulationGrantReturnAcknowledged, payload
	case runtimeEmergencySettledEvent:
		trace.kind, trace.runtimeEmergencySettled = simulationRuntimeEmergencySettled, payload
	case runtimeEmergencyStartedEvent:
		trace.kind, trace.runtimeEmergencyStarted = simulationRuntimeEmergencyStarted, payload
	default:
		panic("simulation campaign event is invalid")
	}
	return trace
}

func (trace simulationCampaignEvent) production() campaignEvent {
	var payload campaignEventPayload
	switch trace.kind {
	case simulationCampaignRegistered:
		payload = trace.registered
	case simulationSnapshotEstablished:
		payload = trace.snapshotEstablished
	case simulationCatalogueDiscovered:
		payload = trace.catalogueDiscovered
	case simulationCampaignPreparationFailed:
		payload = trace.preparationFailed
	case simulationResourceSettled:
		payload = trace.resourceSettled
	case simulationResourceSettlementFailed:
		payload = trace.resourceSettlementFailed
	case simulationTerminalCommitted:
		payload = trace.terminalCommitted
	case simulationWorkspaceMaterialized:
		payload = trace.workspaceMaterialized
	case simulationWorkspaceMaterializationFailed:
		payload = trace.workspaceMaterializationFailed
	case simulationAdmissionGranted:
		payload = trace.admissionGranted
	case simulationAdmissionCancelled:
		payload = trace.admissionCancelled
	case simulationAdmissionRejected:
		payload = trace.admissionRejected
	case simulationStartCommittedEvent:
		payload = trace.startCommitted
	case simulationAttemptLaunched:
		payload = trace.attemptLaunched
	case simulationAttemptTerminal:
		payload = attemptTerminalEvent{
			attempt: trace.attemptTerminal.attempt, generation: trace.attemptTerminal.generation,
			terminal: trace.attemptTerminal.terminal.production(), receipt: trace.attemptTerminal.receipt,
			resolvedMutationDeadline: trace.attemptTerminal.resolvedMutationDeadline,
		}
	case simulationConfirmationBarrierBound:
		payload = trace.confirmationBarrierBound
	case simulationGrantReturnAcknowledged:
		payload = trace.grantReturnAcknowledged
	case simulationRuntimeEmergencySettled:
		payload = trace.runtimeEmergencySettled
	case simulationRuntimeEmergencyStarted:
		payload = trace.runtimeEmergencyStarted
	default:
		panic("simulation campaign event kind is invalid")
	}
	return campaignEvent{id: trace.id, payload: payload}
}

type simulationCampaignOutcome struct {
	kind                    campaignTerminalKind
	mutants                 []mutantResult
	cause                   string
	total                   int
	baseline                campaignAttemptEvidence
	artifactResidue         []string
	singleAdmissionFallback bool
}

type simulationCampaignFailure struct {
	kind     uint8
	residual nonEmptyResidualCustody
	attempts []campaignFatalAttemptEvidence
}

type simulationCampaignState struct {
	definition               campaignDefinition
	phase                    campaignPhase
	runtimeToken             campaignToken
	snapshot                 snapshotIdentity
	catalogue                []mutantIdentity
	catalogueKnown           bool
	mutants                  []campaignMutant
	attempts                 []campaignAttempt
	obligations              []campaignObligation
	trace                    []campaignTraceRecord
	outcome                  simulationCampaignOutcome
	failure                  simulationCampaignFailure
	candidate                campaignTerminalCandidate
	nextEffect               campaignEffectID
	commands                 int
	nextAttempt              uint64
	mutationDeadline         simulationDuration
	baselineEvidence         campaignAttemptEvidence
	artifactResidue          []string
	fatalAttempts            []campaignFatalAttemptEvidence
	drain                    campaignDrainIntent
	confirmationBarrierBound bool
	pendingGrantReturns      []campaignAdmission
	singleAdmissionFallback  bool
}

func simulationTraceCampaignState(state campaignState) simulationCampaignState {
	trace := simulationCampaignState{
		definition: state.definition, phase: state.phase, runtimeToken: state.runtimeToken,
		snapshot: state.snapshot, catalogue: slices.Clone(state.catalogue), catalogueKnown: state.catalogueKnown,
		mutants: slices.Clone(state.mutants), attempts: slices.Clone(state.attempts),
		obligations: slices.Clone(state.obligations), trace: slices.Clone(state.trace),
		candidate: state.candidate, nextEffect: state.nextEffect, commands: state.commands,
		nextAttempt: state.nextAttempt, mutationDeadline: simulationTraceDuration(state.mutationDeadline),
		baselineEvidence: state.baselineEvidence, artifactResidue: slices.Clone(state.artifactResidue),
		fatalAttempts: slices.Clone(state.fatalAttempts), drain: state.drain,
		confirmationBarrierBound: state.confirmationBarrierBound,
		pendingGrantReturns:      slices.Clone(state.pendingGrantReturns),
		singleAdmissionFallback:  state.singleAdmissionFallback,
	}
	switch outcome := state.outcome.(type) {
	case nil:
	case noMutantsOutcome:
		trace.outcome.kind = campaignTerminalNoMutants
	case completedOutcome:
		trace.outcome = simulationCampaignOutcome{
			kind: campaignTerminalCompleted, mutants: slices.Clone(outcome.mutants),
			singleAdmissionFallback: outcome.singleAdmissionFallback,
		}
	case abortedOutcome:
		trace.outcome = simulationCampaignOutcome{
			kind: campaignTerminalAborted, cause: outcome.cause, mutants: slices.Clone(outcome.mutants),
			total: outcome.total, baseline: outcome.baseline,
			artifactResidue:         slices.Clone(outcome.artifactResidue),
			singleAdmissionFallback: outcome.singleAdmissionFallback,
		}
	default:
		panic("simulation campaign outcome is invalid")
	}
	switch failure := state.failure.(type) {
	case nil:
	case cleanupUnconfirmedFault:
		trace.failure = simulationCampaignFailure{
			kind: 1, residual: failure.residual, attempts: slices.Clone(failure.attempts),
		}
	default:
		panic("simulation campaign failure is invalid")
	}
	return trace
}
