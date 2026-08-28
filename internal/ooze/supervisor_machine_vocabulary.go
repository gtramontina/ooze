package ooze

import (
	"slices"
	"time"
)

type simulationInstant struct {
	set        bool
	unixSecond int64
	nanosecond int32
}

type simulationDuration int64

type simulationStopRequest struct {
	at      simulationInstant
	drainBy simulationInstant
}

type supervisionExitRecheck struct {
	performed bool
	observed  bool
	at        simulationInstant
	code      int
	signal    int
	action    supervisorActionToken
}

type supervisionRunningFact struct {
	generation   attemptGeneration
	action       supervisorActionToken
	kind         supervisorRunningFactKind
	at           simulationInstant
	stop         simulationStopRequest
	rootLive     bool
	live         uint64
	liveNegative bool
	exitCode     int
	exitSignal   int
	source       supervisorObservationSource
	diagnostic   supervisorDiagnosticRef
}

type supervisionRunningBundle struct {
	generation   attemptGeneration
	sampleAction supervisorActionToken
	waitAction   supervisorActionToken
	facts        []supervisionRunningFact
	exitRecheck  supervisionExitRecheck
	drainBy      simulationInstant
}

type supervisionRunningIntent struct {
	latched           bool
	kind              supervisorRunningIntentKind
	at                simulationInstant
	drainBy           simulationInstant
	duration          simulationDuration
	count             supervisorObservedCount
	stop              simulationStopRequest
	exitCode          int
	exitSignal        int
	observationSource supervisorObservationSource
	diagnostics       supervisorObservationDiagnostics
}

type supervisionLaunchCompletion struct {
	generation attemptGeneration
	action     supervisorActionToken
	at         simulationInstant
	kind       supervisorLaunchCompletionKind
	failure    LaunchFailure
	diagnostic supervisorDiagnosticRef
}

type supervisionDrainCompletion struct {
	generation     attemptGeneration
	action         supervisorPendingAction
	at             simulationInstant
	kind           supervisorDrainCompletionKind
	waitDiagnostic supervisorDiagnosticRef
	diagnostic     supervisorDiagnosticRef
}

type supervisionOutputCompletion struct {
	generation     attemptGeneration
	action         supervisorPendingAction
	at             simulationInstant
	ref            supervisorOutputRef
	cutoff         uint64
	prefixLength   uint64
	waitDiagnostic supervisorDiagnosticRef
	diagnostic     supervisorDiagnosticRef
}

type supervisionStopSealCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         simulationInstant
}

type supervisionReleaseCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         simulationInstant
	diagnostic supervisorDiagnosticRef
}

type supervisionDrainState struct {
	effectiveDrainBy      simulationInstant
	forced                bool
	decision              supervisorDrainDecision
	waitDiagnostic        supervisorDiagnosticRef
	controlDiagnostic     supervisorDiagnosticRef
	observationDiagnostic supervisorDiagnosticRef
}

type supervisionTerminalEvidence struct {
	kind            supervisorTerminalKind
	profile         Profile
	commandDeadline simulationDuration
	launchDuration  simulationDuration
	commandDuration simulationDuration
	firedBound      supervisorFiredBound
	exitCode        int
	exitSignal      int
	count           supervisorObservedCount
	output          supervisorOutputEvidence
	diagnostics     supervisorTerminalDiagnostics
}

type supervisionAttemptState struct {
	generation        attemptGeneration
	attempt           attemptIdentity
	profile           Profile
	commandDeadline   simulationDuration
	registeredAt      simulationInstant
	launchBy          simulationInstant
	lastEventAt       simulationInstant
	revokedAt         simulationInstant
	startedAt         simulationInstant
	deadlineAt        simulationInstant
	launchAction      supervisorActionToken
	waitAction        supervisorActionToken
	sampleAction      supervisorActionToken
	pendingAction     supervisorPendingAction
	phase             supervisorAttemptPhase
	releaseRevoked    bool
	releaseDiagnostic supervisorDiagnosticRef
	runningPeak       supervisorObservedCount
	intent            supervisionRunningIntent
	drain             supervisionDrainState
	output            supervisorOutputEvidence
	terminal          supervisionTerminalEvidence
}

type supervisionProjection struct {
	nextAction supervisorActionToken
	attempts   []supervisionAttemptState
	emergency  struct {
		active        bool
		at            simulationInstant
		drainBy       simulationInstant
		pendingAction supervisorPendingAction
	}
}

type supervisionEmergencySnapshot struct {
	generation attemptGeneration
	completion *supervisionLaunchCompletion
	running    *supervisionRunningBundle
}

type supervisionFact struct {
	kind                supervisorEventKind
	generation          attemptGeneration
	attempt             attemptIdentity
	at                  simulationInstant
	launchBy            simulationInstant
	drainBy             simulationInstant
	completion          *supervisionLaunchCompletion
	emergencySnapshots  []supervisionEmergencySnapshot
	profile             Profile
	commandDeadline     simulationDuration
	running             *supervisionRunningBundle
	drain               *supervisionDrainCompletion
	output              *supervisionOutputCompletion
	seal                *supervisionStopSealCompletion
	release             *supervisionReleaseCompletion
	runtime             *supervisorRuntimeCompletion
	emergencySettlement *supervisorEmergencySettlementCompletion
}

type supervisionEffect struct {
	kind             supervisorActionKind
	generation       attemptGeneration
	token            supervisorActionToken
	at               simulationInstant
	drainBy          simulationInstant
	launchKind       supervisorLaunchCompletionKind
	launchFailure    LaunchFailure
	launchDiagnostic supervisorDiagnosticRef
	launchDuration   simulationDuration
	intent           supervisionRunningIntent
	terminal         supervisionTerminalEvidence
	runtimeKind      supervisorRuntimeReceiptKind
	resolutions      []supervisorEmergencyResolution
	residuals        []supervisorEmergencyResidual
}

func simulationTraceInstant(value time.Time) simulationInstant {
	if value.IsZero() {
		return simulationInstant{}
	}

	return simulationInstant{
		set: true, unixSecond: value.Unix(), nanosecond: int32(value.Nanosecond()),
	}
}

func (instant simulationInstant) production() time.Time {
	if !instant.set {
		return time.Time{}
	}

	return time.Unix(instant.unixSecond, int64(instant.nanosecond)).UTC()
}

func simulationTraceDuration(value time.Duration) simulationDuration {
	return simulationDuration(value)
}

func (duration simulationDuration) production() time.Duration {
	return time.Duration(duration)
}

func simulationTraceStop(value StopRequest) simulationStopRequest {
	return simulationStopRequest{at: simulationTraceInstant(value.At), drainBy: simulationTraceInstant(value.DrainBy)}
}

func (stop simulationStopRequest) production() StopRequest {
	return StopRequest{At: stop.at.production(), DrainBy: stop.drainBy.production()}
}

func simulationTraceRunningIntent(value supervisorRunningIntent) supervisionRunningIntent {
	return supervisionRunningIntent{
		latched: value.latched, kind: value.kind,
		at: simulationTraceInstant(value.at), drainBy: simulationTraceInstant(value.drainBy),
		duration: simulationTraceDuration(value.duration), count: value.count,
		stop: simulationTraceStop(value.stop), exitCode: value.exitCode, exitSignal: value.exitSignal,
		observationSource: value.observationSource, diagnostics: value.diagnostics,
	}
}

func simulationTraceRunningBundle(value *supervisorRunningBundle) *supervisionRunningBundle {
	if value == nil {
		return nil
	}
	facts := make([]supervisionRunningFact, len(value.facts))
	for index, fact := range value.facts {
		facts[index] = supervisionRunningFact{
			generation: fact.generation, action: fact.action, kind: fact.kind,
			at: simulationTraceInstant(fact.at), stop: simulationTraceStop(fact.stop),
			rootLive: fact.rootLive, live: fact.live, liveNegative: fact.liveNegative,
			exitCode: fact.exitCode, exitSignal: fact.exitSignal,
			source: fact.source, diagnostic: fact.diagnostic,
		}
	}

	return &supervisionRunningBundle{
		generation: value.generation, sampleAction: value.sampleAction, waitAction: value.waitAction,
		facts: facts,
		exitRecheck: supervisionExitRecheck{
			performed: value.exitRecheck.performed, observed: value.exitRecheck.observed,
			at: simulationTraceInstant(value.exitRecheck.at), code: value.exitRecheck.code,
			signal: value.exitRecheck.signal, action: value.exitRecheck.action,
		},
		drainBy: simulationTraceInstant(value.drainBy),
	}
}

func (bundle *supervisionRunningBundle) production() *supervisorRunningBundle {
	if bundle == nil {
		return nil
	}
	facts := make([]supervisorRunningFact, len(bundle.facts))
	for index, fact := range bundle.facts {
		facts[index] = supervisorRunningFact{
			generation: fact.generation, action: fact.action, kind: fact.kind,
			at: fact.at.production(), stop: fact.stop.production(),
			rootLive: fact.rootLive, live: fact.live, liveNegative: fact.liveNegative,
			exitCode: fact.exitCode, exitSignal: fact.exitSignal,
			source: fact.source, diagnostic: fact.diagnostic,
		}
	}

	return &supervisorRunningBundle{
		generation: bundle.generation, sampleAction: bundle.sampleAction, waitAction: bundle.waitAction,
		facts: facts,
		exitRecheck: supervisorExitRecheck{
			performed: bundle.exitRecheck.performed, observed: bundle.exitRecheck.observed,
			at: bundle.exitRecheck.at.production(), code: bundle.exitRecheck.code,
			signal: bundle.exitRecheck.signal, action: bundle.exitRecheck.action,
		},
		drainBy: bundle.drainBy.production(),
	}
}

func simulationTraceLaunchCompletion(value *supervisorLaunchCompletion) *supervisionLaunchCompletion {
	if value == nil {
		return nil
	}

	return &supervisionLaunchCompletion{
		generation: value.generation, action: value.action, at: simulationTraceInstant(value.at),
		kind: value.kind, failure: value.failure, diagnostic: value.diagnostic,
	}
}

func (completion *supervisionLaunchCompletion) production() *supervisorLaunchCompletion {
	if completion == nil {
		return nil
	}

	return &supervisorLaunchCompletion{
		generation: completion.generation, action: completion.action, at: completion.at.production(),
		kind: completion.kind, failure: completion.failure, diagnostic: completion.diagnostic,
	}
}

func simulationTraceDrainCompletion(value *supervisorDrainCompletion) *supervisionDrainCompletion {
	if value == nil {
		return nil
	}

	return &supervisionDrainCompletion{
		generation: value.generation, action: value.action, at: simulationTraceInstant(value.at),
		kind: value.kind, waitDiagnostic: value.waitDiagnostic, diagnostic: value.diagnostic,
	}
}

func (completion *supervisionDrainCompletion) production() *supervisorDrainCompletion {
	if completion == nil {
		return nil
	}

	return &supervisorDrainCompletion{
		generation: completion.generation, action: completion.action, at: completion.at.production(),
		kind: completion.kind, waitDiagnostic: completion.waitDiagnostic, diagnostic: completion.diagnostic,
	}
}

func simulationTraceOutputCompletion(value *supervisorOutputCompletion) *supervisionOutputCompletion {
	if value == nil {
		return nil
	}

	return &supervisionOutputCompletion{
		generation: value.generation, action: value.action, at: simulationTraceInstant(value.at),
		ref: value.ref, cutoff: value.cutoff, prefixLength: value.prefixLength,
		waitDiagnostic: value.waitDiagnostic, diagnostic: value.diagnostic,
	}
}

func (completion *supervisionOutputCompletion) production() *supervisorOutputCompletion {
	if completion == nil {
		return nil
	}

	return &supervisorOutputCompletion{
		generation: completion.generation, action: completion.action, at: completion.at.production(),
		ref: completion.ref, cutoff: completion.cutoff, prefixLength: completion.prefixLength,
		waitDiagnostic: completion.waitDiagnostic, diagnostic: completion.diagnostic,
	}
}

func supervisionFactFromEvent(value supervisorEvent) supervisionFact {
	event := supervisionFact{
		kind: value.kind, generation: value.generation, attempt: value.attempt,
		at: simulationTraceInstant(value.at), launchBy: simulationTraceInstant(value.launchBy),
		drainBy: simulationTraceInstant(value.drainBy), completion: simulationTraceLaunchCompletion(value.completion),
		profile: value.profile, commandDeadline: simulationTraceDuration(value.commandDeadline),
		running: simulationTraceRunningBundle(value.running),
		runtime: value.runtime, emergencySettlement: value.emergencySettlement,
	}
	if value.drain != nil {
		event.drain = simulationTraceDrainCompletion(value.drain)
	}
	if value.output != nil {
		event.output = simulationTraceOutputCompletion(value.output)
	}
	if value.seal != nil {
		event.seal = &supervisionStopSealCompletion{
			generation: value.seal.generation, action: value.seal.action,
			at: simulationTraceInstant(value.seal.at),
		}
	}
	if value.release != nil {
		event.release = &supervisionReleaseCompletion{
			generation: value.release.generation, action: value.release.action,
			at: simulationTraceInstant(value.release.at), diagnostic: value.release.diagnostic,
		}
	}
	event.emergencySnapshots = make([]supervisionEmergencySnapshot, len(value.emergencySnapshots))
	for index, snapshot := range value.emergencySnapshots {
		event.emergencySnapshots[index] = supervisionEmergencySnapshot{
			generation: snapshot.generation, completion: simulationTraceLaunchCompletion(snapshot.completion),
			running: simulationTraceRunningBundle(snapshot.running),
		}
	}
	if value.runtime != nil {
		runtime := *value.runtime
		event.runtime = &runtime
	}
	if value.emergencySettlement != nil {
		settlement := *value.emergencySettlement
		settlement.acknowledged = slices.Clone(settlement.acknowledged)
		settlement.residuals = slices.Clone(settlement.residuals)
		event.emergencySettlement = &settlement
	}

	return event
}

func (event supervisionFact) production() supervisorEvent {
	value := supervisorEvent{
		kind: event.kind, generation: event.generation, attempt: event.attempt,
		at: event.at.production(), launchBy: event.launchBy.production(), drainBy: event.drainBy.production(),
		completion: event.completion.production(), profile: event.profile,
		commandDeadline: event.commandDeadline.production(), running: event.running.production(),
	}
	if event.drain != nil {
		value.drain = event.drain.production()
	}
	if event.output != nil {
		value.output = event.output.production()
	}
	if event.seal != nil {
		value.seal = &supervisorStopSealCompletion{
			generation: event.seal.generation, action: event.seal.action, at: event.seal.at.production(),
		}
	}
	if event.release != nil {
		value.release = &supervisorReleaseCompletion{
			generation: event.release.generation, action: event.release.action,
			at: event.release.at.production(), diagnostic: event.release.diagnostic,
		}
	}
	value.emergencySnapshots = make([]supervisorEmergencySnapshot, len(event.emergencySnapshots))
	for index, snapshot := range event.emergencySnapshots {
		value.emergencySnapshots[index] = supervisorEmergencySnapshot{
			generation: snapshot.generation, completion: snapshot.completion.production(),
			running: snapshot.running.production(),
		}
	}
	if event.runtime != nil {
		runtime := *event.runtime
		value.runtime = &runtime
	}
	if event.emergencySettlement != nil {
		settlement := *event.emergencySettlement
		settlement.acknowledged = slices.Clone(settlement.acknowledged)
		settlement.residuals = slices.Clone(settlement.residuals)
		value.emergencySettlement = &settlement
	}

	return value
}

func supervisionTerminalFromEvidence(value supervisorTerminalEvidence) supervisionTerminalEvidence {
	return supervisionTerminalEvidence{
		kind: value.kind, profile: value.profile,
		commandDeadline: simulationTraceDuration(value.commandDeadline),
		launchDuration:  simulationTraceDuration(value.launchDuration),
		commandDuration: simulationTraceDuration(value.commandDuration),
		firedBound:      value.firedBound, exitCode: value.exitCode, exitSignal: value.exitSignal,
		count: value.count, output: value.output, diagnostics: value.diagnostics,
	}
}

func (value supervisionTerminalEvidence) production() supervisorTerminalEvidence {
	return supervisorTerminalEvidence{
		kind: value.kind, profile: value.profile,
		commandDeadline: value.commandDeadline.production(),
		launchDuration:  value.launchDuration.production(), commandDuration: value.commandDuration.production(),
		firedBound: value.firedBound, exitCode: value.exitCode, exitSignal: value.exitSignal,
		count: value.count, output: value.output, diagnostics: value.diagnostics,
	}
}

func supervisionProjectionFromState(value supervisorState) supervisionProjection {
	state := supervisionProjection{nextAction: value.nextAction}
	state.emergency.active = value.emergency.active
	state.emergency.at = simulationTraceInstant(value.emergency.at)
	state.emergency.drainBy = simulationTraceInstant(value.emergency.drainBy)
	state.emergency.pendingAction = value.emergency.pendingAction
	state.attempts = make([]supervisionAttemptState, len(value.attempts))
	for index, attempt := range value.attempts {
		state.attempts[index] = supervisionAttemptState{
			generation: attempt.generation, attempt: attempt.attempt, profile: attempt.profile,
			commandDeadline: simulationTraceDuration(attempt.commandDeadline),
			registeredAt:    simulationTraceInstant(attempt.registeredAt), launchBy: simulationTraceInstant(attempt.launchBy),
			lastEventAt: simulationTraceInstant(attempt.lastEventAt), revokedAt: simulationTraceInstant(attempt.revokedAt),
			startedAt: simulationTraceInstant(attempt.startedAt), deadlineAt: simulationTraceInstant(attempt.deadlineAt),
			launchAction: attempt.launchAction, waitAction: attempt.waitAction,
			sampleAction: attempt.sampleAction, pendingAction: attempt.pendingAction,
			phase: attempt.phase, releaseRevoked: attempt.releaseRevoked,
			releaseDiagnostic: attempt.releaseDiagnostic, runningPeak: attempt.runningPeak,
			intent: simulationTraceRunningIntent(attempt.intent),
			drain: supervisionDrainState{
				effectiveDrainBy: simulationTraceInstant(attempt.drain.effectiveDrainBy),
				forced:           attempt.drain.forced, decision: attempt.drain.decision,
				waitDiagnostic:        attempt.drain.waitDiagnostic,
				controlDiagnostic:     attempt.drain.controlDiagnostic,
				observationDiagnostic: attempt.drain.observationDiagnostic,
			},
			output: attempt.output, terminal: supervisionTerminalFromEvidence(attempt.terminal),
		}
	}

	return state
}

func supervisionEffectsFromActions(values []supervisorAction) []supervisionEffect {
	actions := make([]supervisionEffect, len(values))
	for index, value := range values {
		actions[index] = supervisionEffect{
			kind: value.kind, generation: value.generation, token: value.token,
			at: simulationTraceInstant(value.at), drainBy: simulationTraceInstant(value.drainBy),
			launchKind: value.launchKind, launchFailure: value.launchFailure,
			launchDiagnostic: value.launchDiagnostic,
			launchDuration:   simulationTraceDuration(value.launchDuration),
			intent:           simulationTraceRunningIntent(value.intent),
			terminal:         supervisionTerminalFromEvidence(value.terminal),
			runtimeKind:      value.runtimeKind,
			resolutions:      slices.Clone(value.resolutions), residuals: slices.Clone(value.residuals),
		}
	}

	return actions
}

func (value supervisionRunningIntent) production() supervisorRunningIntent {
	return supervisorRunningIntent{
		latched: value.latched, kind: value.kind,
		at: value.at.production(), drainBy: value.drainBy.production(), duration: value.duration.production(),
		count: value.count, stop: value.stop.production(), exitCode: value.exitCode, exitSignal: value.exitSignal,
		observationSource: value.observationSource, diagnostics: value.diagnostics,
	}
}

func (effect supervisionEffect) production() supervisorAction {
	return supervisorAction{
		kind: effect.kind, generation: effect.generation, token: effect.token,
		at: effect.at.production(), drainBy: effect.drainBy.production(),
		launchKind: effect.launchKind, launchFailure: effect.launchFailure,
		launchDiagnostic: effect.launchDiagnostic, launchDuration: effect.launchDuration.production(),
		intent: effect.intent.production(), terminal: effect.terminal.production(), runtimeKind: effect.runtimeKind,
		resolutions: slices.Clone(effect.resolutions), residuals: slices.Clone(effect.residuals),
	}
}

func supervisorActionsFromEffects(effects []supervisionEffect) []supervisorAction {
	actions := make([]supervisorAction, len(effects))
	for index, effect := range effects {
		actions[index] = effect.production()
	}

	return actions
}
