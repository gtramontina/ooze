package supervision

import (
	"slices"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

// Profile selects automatic or serial supervision policy.
type Profile = processruntime.Profile

const (
	// AutomaticProfile enables descendant sampling and automatic deadline policy.
	AutomaticProfile = processruntime.AutomaticProfile
	// SerialProfile enables serial command-deadline policy.
	SerialProfile = processruntime.SerialProfile
)

// LaunchFailure classifies a proven pre-release failure.
type LaunchFailure uint8

const (
	// LaunchFailed reports an ordinary pre-release launch failure.
	LaunchFailed LaunchFailure = iota + 1
	// LaunchResourceExhausted reports proven pre-release resource exhaustion.
	LaunchResourceExhausted
)

// StopRequest bounds explicit attempt drainage.
type StopRequest struct {
	At      time.Time
	DrainBy time.Time
}

type supervisionInstant struct {
	set        bool
	unixSecond int64
	nanosecond int32
}

type supervisionDuration int64

type supervisionStopRequest struct {
	at      supervisionInstant
	drainBy supervisionInstant
}

type supervisionExitRecheck struct {
	performed bool
	observed  bool
	at        supervisionInstant
	code      int
	signal    int
	action    supervisorActionToken
}

type supervisionRunningFact struct {
	generation   attemptGeneration
	action       supervisorActionToken
	kind         supervisorRunningFactKind
	at           supervisionInstant
	stop         supervisionStopRequest
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
	drainBy      supervisionInstant
}

type supervisionRunningIntent struct {
	latched           bool
	kind              supervisorRunningIntentKind
	at                supervisionInstant
	drainBy           supervisionInstant
	duration          supervisionDuration
	count             supervisorObservedCount
	stop              supervisionStopRequest
	exitCode          int
	exitSignal        int
	observationSource supervisorObservationSource
	diagnostics       supervisorObservationDiagnostics
}

type supervisionLaunchCompletion struct {
	generation attemptGeneration
	action     supervisorActionToken
	at         supervisionInstant
	kind       supervisorLaunchCompletionKind
	failure    LaunchFailure
	diagnostic supervisorDiagnosticRef
}

type supervisionDrainCompletion struct {
	generation     attemptGeneration
	action         supervisorPendingAction
	at             supervisionInstant
	kind           supervisorDrainCompletionKind
	waitDiagnostic supervisorDiagnosticRef
	diagnostic     supervisorDiagnosticRef
}

type supervisionOutputCompletion struct {
	generation     attemptGeneration
	action         supervisorPendingAction
	at             supervisionInstant
	ref            supervisorOutputRef
	cutoff         uint64
	prefixLength   uint64
	waitDiagnostic supervisorDiagnosticRef
	diagnostic     supervisorDiagnosticRef
}

type supervisionStopSealCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         supervisionInstant
}

type supervisionReleaseCompletion struct {
	generation attemptGeneration
	action     supervisorPendingAction
	at         supervisionInstant
	diagnostic supervisorDiagnosticRef
}

type supervisionDrainState struct {
	effectiveDrainBy      supervisionInstant
	forced                bool
	decision              supervisorDrainDecision
	waitDiagnostic        supervisorDiagnosticRef
	controlDiagnostic     supervisorDiagnosticRef
	observationDiagnostic supervisorDiagnosticRef
}

type supervisionTerminalEvidence struct {
	kind            supervisorTerminalKind
	profile         Profile
	commandDeadline supervisionDuration
	launchDuration  supervisionDuration
	commandDuration supervisionDuration
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
	commandDeadline   supervisionDuration
	registeredAt      supervisionInstant
	launchBy          supervisionInstant
	lastEventAt       supervisionInstant
	revokedAt         supervisionInstant
	startedAt         supervisionInstant
	deadlineAt        supervisionInstant
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

// Projection is an opaque immutable supervision-state view.
type Projection struct {
	value supervisionProjectionValue
}

type supervisionProjectionValue struct {
	nextAction supervisorActionToken
	attempts   []supervisionAttemptState
	emergency  struct {
		active        bool
		at            supervisionInstant
		drainBy       supervisionInstant
		pendingAction supervisorPendingAction
	}
}

type supervisionEmergencySnapshot struct {
	generation attemptGeneration
	completion *supervisionLaunchCompletion
	running    *supervisionRunningBundle
}

// Fact is an immutable normalized supervision input.
type Fact struct {
	kind                supervisionFactKind
	generation          attemptGeneration
	attempt             attemptIdentity
	at                  supervisionInstant
	launchBy            supervisionInstant
	drainBy             supervisionInstant
	completion          *supervisionLaunchCompletion
	emergencySnapshots  []supervisionEmergencySnapshot
	profile             Profile
	commandDeadline     supervisionDuration
	running             *supervisionRunningBundle
	drain               *supervisionDrainCompletion
	output              *supervisionOutputCompletion
	seal                *supervisionStopSealCompletion
	release             *supervisionReleaseCompletion
	runtime             *supervisorRuntimeCompletion
	emergencySettlement *supervisorEmergencySettlementCompletion
}

// Effect is an immutable normalized supervision output.
type Effect struct {
	kind             supervisionEffectKind
	generation       attemptGeneration
	token            supervisorActionToken
	at               supervisionInstant
	drainBy          supervisionInstant
	launchKind       supervisorLaunchCompletionKind
	launchFailure    LaunchFailure
	launchDiagnostic supervisorDiagnosticRef
	launchDuration   supervisionDuration
	intent           supervisionRunningIntent
	terminal         supervisionTerminalEvidence
	runtimeKind      supervisorRuntimeReceiptKind
	resolutions      []supervisorEmergencyResolution
	residuals        []supervisorEmergencyResidual
}

func supervisionInstantFromTime(value time.Time) supervisionInstant {
	if value.IsZero() {
		return supervisionInstant{}
	}

	return supervisionInstant{
		set: true, unixSecond: value.Unix(), nanosecond: int32(value.Nanosecond()),
	}
}

func (instant supervisionInstant) production() time.Time {
	if !instant.set {
		return time.Time{}
	}

	return time.Unix(instant.unixSecond, int64(instant.nanosecond)).UTC()
}

func supervisionDurationFromTime(value time.Duration) supervisionDuration {
	return supervisionDuration(value)
}

func (duration supervisionDuration) production() time.Duration {
	return time.Duration(duration)
}

func supervisionStopRequestFromStop(value StopRequest) supervisionStopRequest {
	return supervisionStopRequest{at: supervisionInstantFromTime(value.At), drainBy: supervisionInstantFromTime(value.DrainBy)}
}

func (stop supervisionStopRequest) production() StopRequest {
	return StopRequest{At: stop.at.production(), DrainBy: stop.drainBy.production()}
}

func simulationTraceRunningIntent(value supervisorRunningIntent) supervisionRunningIntent {
	return supervisionRunningIntent{
		latched: value.latched, kind: value.kind,
		at: supervisionInstantFromTime(value.at), drainBy: supervisionInstantFromTime(value.drainBy),
		duration: supervisionDurationFromTime(value.duration), count: value.count,
		stop: supervisionStopRequestFromStop(value.stop), exitCode: value.exitCode, exitSignal: value.exitSignal,
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
			at: supervisionInstantFromTime(fact.at), stop: supervisionStopRequestFromStop(fact.stop),
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
			at: supervisionInstantFromTime(value.exitRecheck.at), code: value.exitRecheck.code,
			signal: value.exitRecheck.signal, action: value.exitRecheck.action,
		},
		drainBy: supervisionInstantFromTime(value.drainBy),
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

func simulationTracelaunchCompletion(value *supervisorLaunchCompletion) *supervisionLaunchCompletion {
	if value == nil {
		return nil
	}

	return &supervisionLaunchCompletion{
		generation: value.generation, action: value.action, at: supervisionInstantFromTime(value.at),
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
		generation: value.generation, action: value.action, at: supervisionInstantFromTime(value.at),
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
		generation: value.generation, action: value.action, at: supervisionInstantFromTime(value.at),
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

func supervisionFactFromEvent(value supervisorEvent) Fact {
	event := Fact{
		kind: value.kind, generation: value.generation, attempt: value.attempt,
		at: supervisionInstantFromTime(value.at), launchBy: supervisionInstantFromTime(value.launchBy),
		drainBy: supervisionInstantFromTime(value.drainBy), completion: simulationTracelaunchCompletion(value.completion),
		profile: value.profile, commandDeadline: supervisionDurationFromTime(value.commandDeadline),
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
			at: supervisionInstantFromTime(value.seal.at),
		}
	}
	if value.release != nil {
		event.release = &supervisionReleaseCompletion{
			generation: value.release.generation, action: value.release.action,
			at: supervisionInstantFromTime(value.release.at), diagnostic: value.release.diagnostic,
		}
	}
	if value.emergencySnapshots != nil {
		event.emergencySnapshots = make([]supervisionEmergencySnapshot, len(value.emergencySnapshots))
		for index, snapshot := range value.emergencySnapshots {
			event.emergencySnapshots[index] = supervisionEmergencySnapshot{
				generation: snapshot.generation, completion: simulationTracelaunchCompletion(snapshot.completion),
				running: simulationTraceRunningBundle(snapshot.running),
			}
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

func (event Fact) production() supervisorEvent {
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
	if event.emergencySnapshots != nil {
		value.emergencySnapshots = make([]supervisorEmergencySnapshot, len(event.emergencySnapshots))
		for index, snapshot := range event.emergencySnapshots {
			value.emergencySnapshots[index] = supervisorEmergencySnapshot{
				generation: snapshot.generation, completion: snapshot.completion.production(),
				running: snapshot.running.production(),
			}
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
		commandDeadline: supervisionDurationFromTime(value.commandDeadline),
		launchDuration:  supervisionDurationFromTime(value.launchDuration),
		commandDuration: supervisionDurationFromTime(value.commandDuration),
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

func supervisionProjectionFromState(value supervisorState) Projection {
	state := Projection{value: supervisionProjectionValue{nextAction: value.nextAction}}
	state.value.emergency.active = value.emergency.active
	state.value.emergency.at = supervisionInstantFromTime(value.emergency.at)
	state.value.emergency.drainBy = supervisionInstantFromTime(value.emergency.drainBy)
	state.value.emergency.pendingAction = value.emergency.pendingAction
	state.value.attempts = make([]supervisionAttemptState, len(value.attempts))
	for index, attempt := range value.attempts {
		state.value.attempts[index] = supervisionAttemptState{
			generation: attempt.generation, attempt: attempt.attempt, profile: attempt.profile,
			commandDeadline: supervisionDurationFromTime(attempt.commandDeadline),
			registeredAt:    supervisionInstantFromTime(attempt.registeredAt), launchBy: supervisionInstantFromTime(attempt.launchBy),
			lastEventAt: supervisionInstantFromTime(attempt.lastEventAt), revokedAt: supervisionInstantFromTime(attempt.revokedAt),
			startedAt: supervisionInstantFromTime(attempt.startedAt), deadlineAt: supervisionInstantFromTime(attempt.deadlineAt),
			launchAction: attempt.launchAction, waitAction: attempt.waitAction,
			sampleAction: attempt.sampleAction, pendingAction: attempt.pendingAction,
			phase: attempt.phase, releaseRevoked: attempt.releaseRevoked,
			releaseDiagnostic: attempt.releaseDiagnostic, runningPeak: attempt.runningPeak,
			intent: simulationTraceRunningIntent(attempt.intent),
			drain: supervisionDrainState{
				effectiveDrainBy: supervisionInstantFromTime(attempt.drain.effectiveDrainBy),
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

func supervisionEffectsFromActions(values []supervisorAction) []Effect {
	actions := make([]Effect, len(values))
	for index, value := range values {
		actions[index] = Effect{
			kind: value.kind, generation: value.generation, token: value.token,
			at: supervisionInstantFromTime(value.at), drainBy: supervisionInstantFromTime(value.drainBy),
			launchKind: value.launchKind, launchFailure: value.launchFailure,
			launchDiagnostic: value.launchDiagnostic,
			launchDuration:   supervisionDurationFromTime(value.launchDuration),
			intent:           simulationTraceRunningIntent(value.intent),
			terminal:         supervisionTerminalFromEvidence(value.terminal),
			runtimeKind:      value.runtimeKind,
			resolutions:      slices.Clone(value.resolutions), residuals: slices.Clone(value.residuals),
		}
	}

	return actions
}

func supervisionEffectFromAction(value supervisorAction) Effect {
	return supervisionEffectsFromActions([]supervisorAction{value})[0]
}

func (value supervisionRunningIntent) production() supervisorRunningIntent {
	return supervisorRunningIntent{
		latched: value.latched, kind: value.kind,
		at: value.at.production(), drainBy: value.drainBy.production(), duration: value.duration.production(),
		count: value.count, stop: value.stop.production(), exitCode: value.exitCode, exitSignal: value.exitSignal,
		observationSource: value.observationSource, diagnostics: value.diagnostics,
	}
}

func (effect Effect) production() supervisorAction {
	return supervisorAction{
		kind: effect.kind, generation: effect.generation, token: effect.token,
		at: effect.at.production(), drainBy: effect.drainBy.production(),
		launchKind: effect.launchKind, launchFailure: effect.launchFailure,
		launchDiagnostic: effect.launchDiagnostic, launchDuration: effect.launchDuration.production(),
		intent: effect.intent.production(), terminal: effect.terminal.production(), runtimeKind: effect.runtimeKind,
		resolutions: slices.Clone(effect.resolutions), residuals: slices.Clone(effect.residuals),
	}
}

func supervisorActionsFromEffects(effects []Effect) []supervisorAction {
	actions := make([]supervisorAction, len(effects))
	for index, effect := range effects {
		actions[index] = effect.production()
	}

	return actions
}
