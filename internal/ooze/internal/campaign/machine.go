package campaign

import (
	"reflect"
	"slices"
	"time"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

// Definition fixes one campaign's immutable policy inputs.
type Definition struct {
	Identity string
	Lineage  processruntime.Lineage
	Command  []string
	Env      []string
	Profile  processruntime.Profile
	Peers    int
}

// Machine applies campaign facts through the production reducer.
type Machine struct{ state campaignState }

// RuntimeBinding resolves inert campaign identities at the process-runtime boundary.
type RuntimeBinding struct{ campaign processruntime.Campaign }

// RuntimeRequest is one opaque process-runtime request emitted by a campaign.
type RuntimeRequest struct {
	effect Effect
	cut    processruntime.Cut
}

// ArtifactRequest is one opaque repository-artifact request emitted by a campaign.
type ArtifactRequest struct{ effect Effect }

// SupervisionRequest is one opaque attempt-supervision request emitted by a campaign.
type SupervisionRequest struct{ effect Effect }

// BindRuntime retains the executable authority outside pure campaign state.
func BindRuntime(registration processruntime.Registration) RuntimeBinding {
	return RuntimeBinding{campaign: registration.Campaign()}
}

// RuntimeRequest returns the process-runtime request represented by an effect.
func (binding RuntimeBinding) RuntimeRequest(effect Effect, definition Definition) (RuntimeRequest, bool) {
	cut, ok := binding.runtimeCut(effect, definition)
	return RuntimeRequest{effect: effect, cut: cut}, ok
}

// Cut returns the input for the process-runtime reducer.
func (request RuntimeRequest) Cut() processruntime.Cut { return request.cut }

// Matches reports whether a recorded runtime cut accepted this request.
func (request RuntimeRequest) Matches(cut processruntime.RecordedCut) bool {
	return cut.Matches(request.cut)
}

// Complete translates an accepted runtime cut into campaign facts.
func (request RuntimeRequest) Complete(recorded processruntime.RecordedCut) []Fact {
	if !request.Matches(recorded) {
		panic("process runtime result does not match campaign request")
	}
	result := recorded.Result()
	switch recorded.Operation() {
	case processruntime.RegisterCampaignOperation:
		return []Fact{Registered(result.Registration())}
	case processruntime.RequestAdmissionOperation:
		admission := result.Admission()
		if admission.Decision() != processruntime.AdmissionAccepted {
			return []Fact{AdmissionRejected(request.effect, admission, "process runtime rejected admission")}
		}
		return admissionDeliveryFacts(admission.Deliveries())
	case processruntime.CancelAdmissionOperation:
		admission := result.Admission()
		return append([]Fact{AdmissionCancelled(request.effect, admission)}, admissionDeliveryFacts(admission.Deliveries())...)
	case processruntime.ReturnGrantOperation:
		admission := result.Admission()
		return append([]Fact{GrantReturnAcknowledged(request.effect, admission)}, admissionDeliveryFacts(admission.Deliveries())...)
	case processruntime.BindConfirmationBarrierOperation:
		barrier := result.Barrier()
		return append([]Fact{ConfirmationBarrierBound(request.effect, barrier)}, admissionDeliveryFacts(barrier.Deliveries())...)
	case processruntime.CommitStartOperation:
		return []Fact{StartCommitted(request.effect, result.Start())}
	case processruntime.CommitTerminalOperation, processruntime.AuthorizeForcedAbortOperation:
		return []Fact{TerminalCommitted(result.Terminal().Decision())}
	default:
		panic("campaign runtime request operation is invalid")
	}
}

// ArtifactRequest returns the repository-artifact request represented by an effect.
func (effect Effect) ArtifactRequest() (ArtifactRequest, bool) {
	switch effect.value.kind {
	case campaignEffectEstablishSnapshot, campaignEffectDiscoverCatalogue,
		campaignEffectReleaseSnapshot, campaignEffectMaterializeWorkspace,
		campaignEffectReleaseWorkspace:
		return ArtifactRequest{effect: effect}, true
	default:
		return ArtifactRequest{}, false
	}
}

// EstablishesSnapshot reports whether the request creates the campaign snapshot.
func (request ArtifactRequest) EstablishesSnapshot() bool {
	return request.effect.value.kind == campaignEffectEstablishSnapshot
}

// CatalogueSnapshot returns the snapshot whose catalogue is requested.
func (request ArtifactRequest) CatalogueSnapshot() (string, bool) {
	if request.effect.value.kind != campaignEffectDiscoverCatalogue {
		return "", false
	}
	return string(request.effect.value.snapshot), true
}

// Workspace returns the attempt and snapshot for a workspace request.
func (request ArtifactRequest) Workspace() (supervision.Identity, string, bool) {
	if request.effect.value.kind != campaignEffectMaterializeWorkspace {
		return "", "", false
	}
	return request.effect.value.attempt, string(request.effect.value.snapshot), true
}

// Settlement returns the resource released by the request.
func (request ArtifactRequest) Settlement() (ResourceKind, string, bool) {
	switch request.effect.value.kind {
	case campaignEffectReleaseSnapshot:
		return SnapshotResource, string(request.effect.value.snapshot), true
	case campaignEffectReleaseWorkspace:
		return WorkspaceResource, request.effect.value.workspace, true
	default:
		return 0, "", false
	}
}

// EstablishedSnapshot completes snapshot establishment.
func (request ArtifactRequest) EstablishedSnapshot(snapshot string) Fact {
	if !request.EstablishesSnapshot() {
		panic("campaign artifact request does not establish a snapshot")
	}
	return SnapshotEstablished(snapshot)
}

// DiscoveredCatalogue completes catalogue discovery.
func (request ArtifactRequest) DiscoveredCatalogue(mutants []string) Fact {
	snapshot, ok := request.CatalogueSnapshot()
	if !ok {
		panic("campaign artifact request does not discover a catalogue")
	}
	return CatalogueDiscovered(snapshot, mutants)
}

// MaterializedWorkspace completes workspace materialization.
func (request ArtifactRequest) MaterializedWorkspace(workspace string) Fact {
	if _, _, ok := request.Workspace(); !ok {
		panic("campaign artifact request does not materialize a workspace")
	}
	return WorkspaceMaterialized(request.effect, workspace)
}

// Settled completes authoritative resource cleanup.
func (request ArtifactRequest) Settled() Fact {
	kind, identity, ok := request.Settlement()
	if !ok {
		panic("campaign artifact request does not settle a resource")
	}
	return ResourceSettled(kind, identity)
}

// SupervisionRequest returns the attempt-supervision request represented by an effect.
func (effect Effect) SupervisionRequest() (SupervisionRequest, bool) {
	switch effect.value.kind {
	case campaignEffectLaunchAttempt, campaignEffectStopAttempt:
		return SupervisionRequest{effect: effect}, true
	default:
		return SupervisionRequest{}, false
	}
}

// Prospective returns the attempt launched by this request.
func (request SupervisionRequest) Prospective(registeredAt, launchBy time.Time) (supervision.Fact, bool) {
	if request.effect.value.kind != campaignEffectLaunchAttempt {
		return supervision.Fact{}, false
	}
	spec := request.effect.Spec()
	return supervision.ProspectiveRegistration(
		request.effect.Generation(), request.effect.Attempt(), registeredAt, launchBy,
		spec.Profile, spec.Deadline,
	), true
}

// StopGeneration returns the generation stopped by this request.
func (request SupervisionRequest) StopGeneration() (processruntime.Generation, bool) {
	if request.effect.value.kind != campaignEffectStopAttempt {
		return 0, false
	}
	return request.effect.Generation(), true
}

func admissionDeliveryFacts(deliveries []processruntime.Admission) []Fact {
	facts := make([]Fact, len(deliveries))
	for index, delivery := range deliveries {
		facts[index] = AdmissionDelivered(delivery)
	}
	return facts
}

// Fact is an immutable campaign input.
type Fact struct {
	payload campaignEventPayload
	label   string
}

// Transition contains the normalized event, effects, and projection produced by one accepted fact.
type Transition struct {
	event      Event
	effects    []campaignEffect
	projection Projection
}

// Event is an immutable accepted campaign fact.
type Event struct {
	value    campaignEvent
	previous campaignState
}

// EventKind identifies one accepted campaign fact.
type EventKind uint8

// Campaign event kinds.
const (
	CampaignRegisteredEvent EventKind = iota + 1
	SnapshotEstablishedEvent
	CatalogueDiscoveredEvent
	CampaignPreparationFailedEvent
	ResourceSettledEvent
	ResourceSettlementFailedEvent
	TerminalCommittedEvent
	WorkspaceMaterializedEvent
	WorkspaceMaterializationFailedEvent
	AdmissionGrantedEvent
	AdmissionCancelledEvent
	AdmissionRejectedEvent
	StartCommittedEvent
	AttemptLaunchedEvent
	AttemptTerminalEvent
	ConfirmationBarrierBoundEvent
	GrantReturnAcknowledgedEvent
	RuntimeEmergencySettledEvent
	RuntimeEmergencyStartedEvent
)

// Effect is an immutable normalized campaign effect.
type Effect struct{ value campaignEffect }

// Owner identifies the system boundary that interprets an effect.
type Owner uint8

// Campaign effect owners.
const (
	ArtifactOwner Owner = iota + 1
	RuntimeOwner
	SupervisionOwner
)

// AttemptRole identifies one attempt's role in a campaign.
type AttemptRole uint8

// Campaign attempt roles.
const (
	BaselineAttempt AttemptRole = iota + 1
	PrimaryAttempt
	ConfirmationAttempt
)

// ID returns the stable effect identity.
func (effect Effect) ID() uint64 { return uint64(effect.value.id) }

// IsZero reports whether the effect is absent.
func (effect Effect) IsZero() bool { return effect.value.kind == 0 }

// Less reports the canonical order of two campaign effects.
func (effect Effect) Less(other Effect, catalogue []string) bool {
	ranks := make(map[mutantIdentity]int, len(catalogue))
	for rank, mutant := range catalogue {
		ranks[mutantIdentity(mutant)] = rank + 1
	}
	firstRank, secondRank := ranks[effect.value.mutant], ranks[other.value.mutant]
	if firstRank != secondRank {
		if firstRank == 0 {
			return false
		}
		if secondRank == 0 {
			return true
		}
		return firstRank < secondRank
	}
	if effect.value.attempt != other.value.attempt {
		return effect.value.attempt < other.value.attempt
	}
	if effect.value.generation != other.value.generation {
		return effect.value.generation < other.value.generation
	}
	if effect.value.id != other.value.id {
		return effect.value.id < other.value.id
	}
	return effect.value.kind < other.value.kind
}

// Owner returns the boundary that interprets the effect.
func (effect Effect) Owner() Owner {
	switch effect.value.kind {
	case campaignEffectEstablishSnapshot, campaignEffectDiscoverCatalogue,
		campaignEffectReleaseSnapshot, campaignEffectMaterializeWorkspace,
		campaignEffectReleaseWorkspace:
		return ArtifactOwner
	case campaignEffectRegister, campaignEffectRequestAdmission,
		campaignEffectRequestStartCommitment, campaignEffectCancelAdmission,
		campaignEffectReturnAdmission, campaignEffectBindConfirmationBarrier,
		campaignEffectProposeTerminal:
		return RuntimeOwner
	case campaignEffectLaunchAttempt, campaignEffectStopAttempt:
		return SupervisionOwner
	default:
		panic("campaign effect owner is invalid")
	}
}

// Attempt returns the affected attempt identity.
func (effect Effect) Attempt() supervision.Identity { return effect.value.attempt }

// Generation returns the affected execution generation.
func (effect Effect) Generation() processruntime.Generation { return effect.value.generation }

// Mutant returns the affected mutant identity.
func (effect Effect) Mutant() string { return string(effect.value.mutant) }

// AttemptRole returns the affected attempt role.
func (effect Effect) AttemptRole() AttemptRole { return AttemptRole(effect.value.attemptKind) }

// Equal reports whether two effects carry the same immutable value.
func (effect Effect) Equal(other Effect) bool { return reflect.DeepEqual(effect.value, other.value) }

// Canonical returns a capability-free effect with logical artifact identities.
func (effect Effect) Canonical(projection Projection) Effect {
	value := effect.value
	value.runtimeToken = campaignToken{}
	value.request.campaign = campaignToken{}
	value.grant.campaign = campaignToken{}
	value.binding.campaign = campaignToken{}
	identity := projection.state.definition.identity
	if value.snapshot != "" {
		value.snapshot = snapshotIdentity("snapshot:" + string(identity))
	}
	if value.workspace != "" {
		value.workspace = "workspace:" + string(value.attempt)
	}
	if value.spec.Dir != "" {
		value.spec.Dir = "workspace:" + string(value.attempt)
	}
	value.spec.Command = slices.Clone(value.spec.Command)
	value.spec.Env = slices.Clone(value.spec.Env)
	return Effect{value: value}
}

// Spec returns a detached supervision specification.
func (effect Effect) Spec() supervision.Spec {
	result := effect.value.spec
	result.Command = slices.Clone(result.Command)
	result.Env = slices.Clone(result.Env)
	return result
}

// CompletesConfirmationQueue reports whether the attempt closes its confirmation queue.
func (effect Effect) CompletesConfirmationQueue() bool {
	return effect.value.completesConfirmationQueue
}

func (binding RuntimeBinding) runtimeCut(effect Effect, definition Definition) (processruntime.Cut, bool) {
	switch effect.value.kind {
	case campaignEffectRegister:
		return processruntime.RegisterCampaignCut(definition.Lineage), true
	case campaignEffectRequestAdmission:
		admission, ok := processRuntimeAdmission(effect.value.request, binding.campaign)
		return processruntime.RequestAdmissionCut(admission), ok
	case campaignEffectCancelAdmission:
		admission, ok := processRuntimeAdmission(effect.value.request, binding.campaign)
		return processruntime.CancelAdmissionCut(admission), ok
	case campaignEffectReturnAdmission:
		admission, ok := processRuntimeAdmission(effect.value.grant, binding.campaign)
		return processruntime.ReturnGrantCut(admission), ok
	case campaignEffectBindConfirmationBarrier:
		barrier := effect.value.binding
		if barrier.campaign != campaignTokenValue(binding.campaign) {
			panic("campaign runtime authority does not match the inert campaign identity")
		}
		return processruntime.BindConfirmationBarrierCut(processruntime.Barrier{
			Campaign: binding.campaign, Attempt: string(barrier.attempt),
			Profile: barrier.profile, Deadline: barrier.deadline,
		}), true
	case campaignEffectRequestStartCommitment:
		admission, ok := processRuntimeAdmission(effect.value.grant, binding.campaign)
		return processruntime.CommitStartCut(admission), ok
	case campaignEffectProposeTerminal:
		if effect.value.runtimeToken != campaignTokenValue(binding.campaign) {
			panic("campaign runtime authority does not match the inert campaign identity")
		}
		if effect.value.fatalEpoch != 0 {
			return processruntime.AuthorizeForcedAbortCut(
				binding.campaign, uint64(effect.value.fatalEpoch),
			), true
		}
		return processruntime.CommitTerminalCut(binding.campaign), true
	default:
		return processruntime.Cut{}, false
	}
}

// ResourceKind identifies a campaign-owned resource.
type ResourceKind uint8

// Campaign-owned resource kinds.
const (
	RegistrationResource ResourceKind = iota + 1
	SnapshotResource
	WorkspaceResource
	AdmissionResource
	PendingStartResource
	ExecutionDomainResource
)

// OutcomeKind identifies a terminal campaign outcome.
type OutcomeKind uint8

// Terminal campaign outcome kinds.
const (
	NoMutantsOutcome OutcomeKind = iota + 1
	CompletedOutcome
	AbortedOutcome
)

// Outcome is immutable terminal campaign evidence.
type Outcome struct{ kind OutcomeKind }

// MutationEvidence contains one mutant's pure reducer evidence.
type MutationEvidence struct {
	identity     string
	result       ManagedMutationOutcome
	primary      AttemptKind
	confirmation AttemptKind
}

// Identity returns the stable mutant identity.
func (evidence MutationEvidence) Identity() string { return evidence.identity }

// Result returns the attributable mutant outcome.
func (evidence MutationEvidence) Result() ManagedMutationOutcome { return evidence.result }

// Primary returns the primary attempt evidence kind.
func (evidence MutationEvidence) Primary() AttemptKind { return evidence.primary }

// Confirmation returns the confirmation attempt evidence kind when present.
func (evidence MutationEvidence) Confirmation() AttemptKind { return evidence.confirmation }

// Kind returns the terminal outcome kind.
func (outcome Outcome) Kind() OutcomeKind { return outcome.kind }

// Projection is an immutable campaign-state view.
type Projection struct{ state campaignState }

// Obligations returns stable campaign-owned resource identities.
func (projection Projection) Obligations() []string {
	result := make([]string, len(projection.state.obligations))
	for index, obligation := range projection.state.obligations {
		result[index] = obligation.identity
	}
	return result
}

// Equal reports whether two projections describe the same campaign state.
func (projection Projection) Equal(other Projection) bool {
	return reflect.DeepEqual(projection.state, other.state)
}

// Canonical returns a capability-free projection with logical artifact identities.
func (projection Projection) Canonical() Projection {
	state := projection.state.clone()
	logicalSnapshot := snapshotIdentity("snapshot:" + string(state.definition.identity))
	if state.snapshot != "" {
		state.snapshot = logicalSnapshot
	}
	for index := range state.attempts {
		state.attempts[index].request = canonicalCampaignAdmission(state.attempts[index].request)
		state.attempts[index].grant = canonicalCampaignAdmission(state.attempts[index].grant)
		if state.attempts[index].workspace != "" {
			state.attempts[index].workspace = "workspace:" + string(state.attempts[index].identity)
		}
	}
	for index := range state.obligations {
		switch state.obligations[index].kind {
		case campaignResourceSnapshot:
			if state.snapshot != "" {
				state.obligations[index].identity = string(logicalSnapshot)
			}
		case campaignResourceWorkspace:
			attempt := state.obligations[index].attempt
			if attempt != "" {
				state.obligations[index].identity = "workspace:" + string(attempt)
			}
		}
	}
	for index := range state.artifactResidue {
		state.artifactResidue[index] = "artifact-residue"
	}
	state.definition.baselineDeadline = 0
	state.runtimeToken = campaignToken{}
	state.pendingGrantReturns = canonicalCampaignAdmissions(state.pendingGrantReturns)
	state.acknowledgedGrantReturns = canonicalCampaignAdmissions(state.acknowledgedGrantReturns)
	return Projection{state: state}
}

// Settled reports whether the campaign has reached an outcome or failure.
func (projection Projection) Settled() bool {
	return projection.state.outcome != nil || projection.state.failure != nil
}

// Failed reports whether the projection carries infrastructure failure evidence.
func (projection Projection) Failed() bool { return projection.state.failure != nil }

// PhaseName returns the current campaign phase.
func (projection Projection) PhaseName() string {
	switch projection.state.phase {
	case campaignPreparing:
		return "Preparing"
	case campaignBaselining:
		return "Baselining"
	case campaignRunning:
		return "Running"
	case campaignDraining:
		return "Draining"
	case campaignConfirming:
		return "Confirming"
	default:
		return ""
	}
}

// Catalogue returns the stable mutant identities.
func (projection Projection) Catalogue() []string {
	result := make([]string, len(projection.state.catalogue))
	for index, mutant := range projection.state.catalogue {
		result[index] = string(mutant)
	}
	return result
}

// Definition returns detached immutable campaign inputs.
func (projection Projection) Definition() Definition {
	definition := projection.state.definition
	return Definition{
		Identity: string(definition.identity), Lineage: definition.lineage,
		Command: slices.Clone(definition.command), Env: slices.Clone(definition.env),
		Profile: definition.profile, Peers: definition.peers,
	}
}

// Fork returns an independent machine at the projected state.
func (projection Projection) Fork() Machine { return Machine{state: projection.state.clone()} }

// Event returns the fact carried by the event.
func (event Event) Fact() Fact { return Fact{payload: event.value.payload, label: event.value.label} }

// Kind returns the accepted campaign fact kind.
func (event Event) Kind() EventKind {
	switch event.value.payload.(type) {
	case campaignRegisteredEvent:
		return CampaignRegisteredEvent
	case snapshotEstablishedEvent:
		return SnapshotEstablishedEvent
	case catalogueDiscoveredEvent:
		return CatalogueDiscoveredEvent
	case campaignPreparationFailedEvent:
		return CampaignPreparationFailedEvent
	case resourceSettledEvent:
		return ResourceSettledEvent
	case resourceSettlementFailedEvent:
		return ResourceSettlementFailedEvent
	case terminalCommittedEvent:
		return TerminalCommittedEvent
	case workspaceMaterializedEvent:
		return WorkspaceMaterializedEvent
	case workspaceMaterializationFailedEvent:
		return WorkspaceMaterializationFailedEvent
	case admissionGrantedEvent:
		return AdmissionGrantedEvent
	case admissionCancelledEvent:
		return AdmissionCancelledEvent
	case admissionRejectedEvent:
		return AdmissionRejectedEvent
	case startCommittedEvent:
		return StartCommittedEvent
	case attemptLaunchEvent:
		return AttemptLaunchedEvent
	case attemptTerminalEvent:
		return AttemptTerminalEvent
	case confirmationBarrierBoundEvent:
		return ConfirmationBarrierBoundEvent
	case grantReturnAcknowledgedEvent:
		return GrantReturnAcknowledgedEvent
	case runtimeEmergencySettledEvent:
		return RuntimeEmergencySettledEvent
	case runtimeEmergencyStartedEvent:
		return RuntimeEmergencyStartedEvent
	default:
		return 0
	}
}

// CatalogueSize returns the discovered mutant count when present.
func (event Event) CatalogueSize() (int, bool) {
	discovered, ok := event.value.payload.(catalogueDiscoveredEvent)
	return len(discovered.mutants), ok
}

// AttemptRole returns the role of the attempt named by the event.
func (event Event) AttemptRole() (AttemptRole, bool) {
	attempt, ok := event.attempt()
	return AttemptRole(attempt.kind), ok
}

// MutationLabel returns the mutation label named by the event when present.
func (event Event) MutationLabel() (string, bool) {
	attempt, ok := event.attempt()
	return event.value.label, ok && attempt.mutant != ""
}

// LaunchOwned reports whether an attempt launch established owned custody.
func (event Event) LaunchOwned() bool {
	launched, ok := event.value.payload.(attemptLaunchEvent)
	return ok && launched.result.kind == campaignLaunchOwned
}

// AttemptPassed returns baseline settlement evidence when present.
func (event Event) AttemptPassed() (bool, bool) {
	_, ok := event.value.payload.(attemptTerminalEvent)
	if !ok {
		return false, false
	}
	attempt, found := event.attempt()
	if !found || attempt.kind != campaignAttemptBaseline {
		return false, false
	}
	return event.value.baselinePassed, true
}

// MutationOutcome returns a newly attributable mutant outcome when present.
func (event Event) MutationOutcome() (ManagedMutationOutcome, bool) {
	attempt, ok := event.attempt()
	if !ok || attempt.mutant == "" {
		return 0, false
	}
	if event.value.mutationOutcome == 0 {
		return 0, false
	}
	return presentManagedMutation(event.value.mutationOutcome), true
}

// TerminalOutcome returns campaign terminal evidence when present.
func (event Event) TerminalOutcome() (OutcomeKind, bool) {
	if event.Kind() != TerminalCommittedEvent {
		return 0, false
	}
	switch event.value.terminalOutcome {
	case campaignTerminalNoMutants:
		return NoMutantsOutcome, true
	case campaignTerminalCompleted:
		return CompletedOutcome, true
	case campaignTerminalAborted:
		return AbortedOutcome, true
	default:
		return 0, false
	}
}

func (event Event) attempt() (campaignAttempt, bool) {
	attempt := event.Fact().Attempt()
	if attempt == "" {
		return campaignAttempt{}, false
	}
	attemptAt := event.previous.attemptIndex(attemptIdentity(attempt))
	if attemptAt < 0 {
		return campaignAttempt{}, false
	}
	return event.previous.attempts[attemptAt], true
}

// WithFact returns the same event identity carrying replacement campaign input.
func (event Event) WithFact(fact Fact) Event {
	value := event.value
	value.payload = fact.payload
	value.label = fact.label
	value.baselinePassed = false
	value.mutationOutcome = 0
	value.terminalOutcome = 0
	return Event{value: value, previous: event.previous}
}

// Equal reports whether two events carry the same immutable value.
func (event Event) Equal(other Event) bool { return reflect.DeepEqual(event.value, other.value) }

// Canonical returns a capability-free event with logical artifact identities.
func (event Event) Canonical() Event {
	value := event.value
	canonical := Fact{payload: value.payload, label: value.label}.Canonical()
	value.payload = canonical.payload
	value.label = canonical.label
	identity := event.previous.definition.identity
	logicalSnapshot := snapshotIdentity("snapshot:" + string(identity))
	switch payload := value.payload.(type) {
	case snapshotEstablishedEvent:
		payload.snapshot = logicalSnapshot
		value.payload = payload
	case catalogueDiscoveredEvent:
		payload.snapshot = logicalSnapshot
		payload.mutants = slices.Clone(payload.mutants)
		value.payload = payload
	case workspaceMaterializedEvent:
		payload.snapshot = logicalSnapshot
		payload.workspace = "workspace:" + string(payload.attempt)
		value.payload = payload
	case workspaceMaterializationFailedEvent:
		payload.artifactResidue = slices.Clone(payload.artifactResidue)
		for index := range payload.artifactResidue {
			payload.artifactResidue[index] = "artifact-residue"
		}
		value.payload = payload
	case resourceSettledEvent:
		payload.identity = canonicalResourceIdentity(payload.kind, payload.identity, event.previous)
		value.payload = payload
	case resourceSettlementFailedEvent:
		payload.identity = canonicalResourceIdentity(payload.kind, payload.identity, event.previous)
		value.payload = payload
	}
	return Event{value: value}
}

// Canonical returns the fact without process-runtime authority.
func (fact Fact) Canonical() Fact {
	payload := fact.payload
	switch event := payload.(type) {
	case campaignRegisteredEvent:
		event.registration.token = campaignToken{}
		payload = event
	case admissionGrantedEvent:
		event.grant = canonicalCampaignAdmission(event.grant)
		payload = event
	case admissionCancelledEvent:
		event.request = canonicalCampaignAdmission(event.request)
		event.result.request = canonicalCampaignAdmission(event.result.request)
		payload = event
	case admissionRejectedEvent:
		event.result.request = canonicalCampaignAdmission(event.result.request)
		payload = event
	case startCommittedEvent:
		event.grant = canonicalCampaignAdmission(event.grant)
		payload = event
	case attemptLaunchEvent:
		event.receipt = canonicalRuntimeReceipt(event.receipt)
		payload = event
	case attemptTerminalEvent:
		event.receipt = canonicalRuntimeReceipt(event.receipt)
		payload = event
	case confirmationBarrierBoundEvent:
		event.result.request = canonicalCampaignAdmission(event.result.request)
		event.result.deliveries = canonicalCampaignAdmissions(event.result.deliveries)
		payload = event
	case grantReturnAcknowledgedEvent:
		event.grant = canonicalCampaignAdmission(event.grant)
		event.result.request = canonicalCampaignAdmission(event.result.request)
		payload = event
	case runtimeEmergencyStartedEvent:
		event.closure.cancelledWaiting = canonicalCampaignAdmissions(event.closure.cancelledWaiting)
		event.closure.compensatedGrants = canonicalCampaignAdmissions(event.closure.compensatedGrants)
		payload = event
	case runtimeEmergencySettledEvent:
		event.settlement.owner = campaignToken{}
		payload = event
	}
	return Fact{payload: payload, label: fact.label}
}

func canonicalCampaignAdmission(admission campaignAdmission) campaignAdmission {
	admission.campaign = campaignToken{}
	return admission
}

func canonicalCampaignAdmissions(admissions []campaignAdmission) []campaignAdmission {
	result := slices.Clone(admissions)
	for index := range result {
		result[index] = canonicalCampaignAdmission(result[index])
	}
	return result
}

func canonicalRuntimeReceipt(receipt campaignRuntimeReceipt) campaignRuntimeReceipt {
	receipt.cancelledWaiting = canonicalCampaignAdmissions(receipt.cancelledWaiting)
	receipt.compensatedGrants = canonicalCampaignAdmissions(receipt.compensatedGrants)
	return receipt
}

func canonicalResourceIdentity(kind campaignResourceKind, identity string, state campaignState) string {
	switch kind {
	case campaignResourceSnapshot:
		return "snapshot:" + string(state.definition.identity)
	case campaignResourceWorkspace:
		for _, attempt := range state.attempts {
			if attempt.workspace == identity {
				return "workspace:" + string(attempt.identity)
			}
		}
		return "workspace:settled"
	default:
		return identity
	}
}

// Name returns the stable domain name of the fact.
func (fact Fact) Name() string {
	if fact.payload == nil {
		return ""
	}
	return fact.payload.campaignEventName()
}

// IsZero reports whether the fact is absent.
func (fact Fact) IsZero() bool { return fact.payload == nil }

// Equal reports whether two facts carry the same immutable value.
func (fact Fact) Equal(other Fact) bool { return reflect.DeepEqual(fact, other) }

// Complexity returns the semantic payload size used by deterministic shrinking.
func (fact Fact) Complexity() int {
	switch value := fact.payload.(type) {
	case catalogueDiscoveredEvent:
		return len(value.mutants)
	case workspaceMaterializationFailedEvent:
		return len(value.artifactResidue)
	default:
		return 0
	}
}

// Attempt returns the fact's attempt identity when it has one.
func (fact Fact) Attempt() supervision.Identity {
	switch value := fact.payload.(type) {
	case workspaceMaterializedEvent:
		return value.attempt
	case workspaceMaterializationFailedEvent:
		return value.attempt
	case admissionGrantedEvent:
		return value.attempt
	case admissionCancelledEvent:
		return value.attempt
	case admissionRejectedEvent:
		return value.attempt
	case startCommittedEvent:
		return value.attempt
	case attemptLaunchEvent:
		return value.attempt
	case attemptTerminalEvent:
		return value.attempt
	case confirmationBarrierBoundEvent:
		return value.attempt
	case grantReturnAcknowledgedEvent:
		return value.grant.attempt
	default:
		return ""
	}
}

// Generation returns the fact's execution generation when it has one.
func (fact Fact) Generation() processruntime.Generation {
	switch value := fact.payload.(type) {
	case attemptLaunchEvent:
		return value.generation
	case attemptTerminalEvent:
		return value.generation
	default:
		return 0
	}
}

// RuntimeClosureInProgress reports whether runtime receipt evidence carries a fatal epoch.
func (fact Fact) RuntimeClosureInProgress() bool {
	switch value := fact.payload.(type) {
	case attemptLaunchEvent:
		return value.receipt.runtimeClosureInProgress
	case attemptTerminalEvent:
		return value.receipt.runtimeClosureInProgress
	default:
		return false
	}
}

// IsAttemptTerminal reports whether the fact carries terminal attempt evidence.
func (fact Fact) IsAttemptTerminal() bool {
	_, ok := fact.payload.(attemptTerminalEvent)
	return ok
}

// IsAttemptLaunched reports whether the fact carries launch evidence.
func (fact Fact) IsAttemptLaunched() bool {
	_, ok := fact.payload.(attemptLaunchEvent)
	return ok
}

// IsAdmissionGranted reports whether the fact delivers admission authority.
func (fact Fact) IsAdmissionGranted() bool {
	_, ok := fact.payload.(admissionGrantedEvent)
	return ok
}

// IsStartCommitted reports whether the fact carries a start decision.
func (fact Fact) IsStartCommitted() bool {
	_, ok := fact.payload.(startCommittedEvent)
	return ok
}

// IsResourceSettled reports whether the fact confirms authoritative cleanup.
func (fact Fact) IsResourceSettled() bool {
	_, ok := fact.payload.(resourceSettledEvent)
	return ok
}

// SameKind reports whether two facts represent the same domain event kind.
func (fact Fact) SameKind(other Fact) bool {
	return reflect.TypeOf(fact.payload) == reflect.TypeOf(other.payload)
}

// CompletesEmergencySettlement reports whether the fact closes a runtime-wide emergency sweep.
func (fact Fact) CompletesEmergencySettlement() bool {
	_, ok := fact.payload.(runtimeEmergencySettledEvent)
	return ok
}

// SupervisorDelivery returns the supervision delivery that caused the fact.
func (fact Fact) SupervisorDelivery() (supervision.EffectKind, processruntime.Generation, bool) {
	switch fact.payload.(type) {
	case attemptTerminalEvent:
		return supervision.DeliverTerminalEffect, fact.Generation(), true
	case runtimeEmergencySettledEvent:
		return supervision.DeliverEmergencySettlementEffect, 0, true
	default:
		return 0, 0, false
	}
}

// SupersedesFact reports whether a pending fact became stale after the latest transition.
func (machine Machine) SupersedesFact(fact Fact) bool {
	switch fact.payload.(type) {
	case admissionGrantedEvent, admissionCancelledEvent, admissionRejectedEvent,
		startCommittedEvent, grantReturnAcknowledgedEvent:
		return !machine.Accepts(fact)
	default:
		return false
	}
}

// SupersedesEffect reports whether a pending asynchronous effect became stale.
func (machine Machine) SupersedesEffect(effect Effect) bool {
	switch effect.value.kind {
	case campaignEffectCancelAdmission, campaignEffectRequestStartCommitment:
		return !machine.effectPending(effect)
	default:
		return false
	}
}

// MayCommitTerminal reports whether the effect can complete campaign terminalization.
func (effect Effect) MayCommitTerminal() bool {
	return effect.value.kind == campaignEffectProposeTerminal
}

// ProvenNotReleased reports whether the fact is a proven pre-release launch failure.
func (fact Fact) ProvenNotReleased() bool {
	value, ok := fact.payload.(attemptLaunchEvent)
	return ok && value.result.kind == campaignLaunchNotReleased
}

// StartAccepted reports whether the fact accepted start commitment.
func (fact Fact) StartAccepted() bool {
	value, ok := fact.payload.(startCommittedEvent)
	return ok && value.result.decision == processruntime.StartAccepted
}

// WithResourceIdentity replaces a cleanup fact's resource identity.
func (fact Fact) WithResourceIdentity(identity string) Fact {
	switch value := fact.payload.(type) {
	case resourceSettledEvent:
		value.identity = identity
		return Fact{payload: value, label: fact.label}
	case resourceSettlementFailedEvent:
		value.identity = identity
		return Fact{payload: value, label: fact.label}
	default:
		panic("campaign fact has no resource identity")
	}
}

// ResourceKind returns the cleanup fact's resource kind.
func (fact Fact) ResourceKind() ResourceKind {
	switch value := fact.payload.(type) {
	case resourceSettledEvent:
		return ResourceKind(value.kind)
	case resourceSettlementFailedEvent:
		return ResourceKind(value.kind)
	default:
		return 0
	}
}

// Enables reports whether an effect authorizes an externally supplied fact.
func (effect Effect) Enables(fact Fact) bool {
	switch value := fact.payload.(type) {
	case snapshotEstablishedEvent:
		return effect.value.kind == campaignEffectEstablishSnapshot
	case catalogueDiscoveredEvent:
		return effect.value.kind == campaignEffectDiscoverCatalogue
	case resourceSettledEvent:
		switch value.kind {
		case campaignResourceSnapshot:
			return effect.value.kind == campaignEffectReleaseSnapshot && string(effect.value.snapshot) == value.identity
		case campaignResourceWorkspace:
			return effect.value.kind == campaignEffectReleaseWorkspace && effect.value.workspace == value.identity
		}
	case workspaceMaterializedEvent:
		return effect.value.kind == campaignEffectMaterializeWorkspace && effect.value.attempt == value.attempt
	}
	return false
}

// MatchesRuntimeCut reports whether a process-runtime cut produced the fact.
func (fact Fact) MatchesRuntimeCut(cut processruntime.RecordedCut) bool {
	result := cut.Result()
	switch event := fact.payload.(type) {
	case campaignRegisteredEvent:
		return cut.Operation() == processruntime.RegisterCampaignOperation &&
			campaignRegistrationEvidence(result.Registration()) == event.registration
	case admissionGrantedEvent:
		return slices.ContainsFunc(runtimeDeliveries(cut), func(delivery processruntime.Admission) bool {
			return campaignAdmissionFact(delivery) == event.grant
		})
	case admissionCancelledEvent:
		cancelled := runtimeAdmissionResult(result.Admission())
		return cut.Operation() == processruntime.CancelAdmissionOperation &&
			campaignAdmissionValue(cancelled.request) == event.request &&
			cancelled.decision == event.result.decision &&
			campaignAdmissionValue(cancelled.request) == event.result.request &&
			cancelled.fatalEpoch == event.result.fatalEpoch
	case admissionRejectedEvent:
		rejected := runtimeAdmissionResult(result.Admission())
		return cut.Operation() == processruntime.RequestAdmissionOperation &&
			rejected.decision == event.result.decision && campaignAdmissionValue(rejected.request) == event.result.request
	case startCommittedEvent:
		return cut.Operation() == processruntime.CommitStartOperation &&
			reflect.DeepEqual(runtimeStartResult(result.Start()), startCommittedResult(event.result))
	case attemptLaunchEvent:
		return cut.Matches(processruntime.ObserveAttemptCut(event.generation, processruntime.Owned()))
	case confirmationBarrierBoundEvent:
		bound := runtimeBarrierResult(result.Barrier())
		return cut.Operation() == processruntime.BindConfirmationBarrierOperation &&
			bound.decision == event.result.decision && campaignAdmissionValue(bound.request) == event.result.request &&
			slices.EqualFunc(bound.deliveries, event.result.deliveries,
				func(left admissionAuthority, right campaignAdmission) bool {
					return campaignAdmissionValue(left) == right
				})
	case grantReturnAcknowledgedEvent:
		return cut.Operation() == processruntime.ReturnGrantOperation &&
			result.Admission().Decision() == event.result.decision
	case terminalCommittedEvent:
		return cut.Operation() == processruntime.CommitTerminalOperation &&
			result.Terminal().Decision() == event.result.decision
	default:
		return false
	}
}

func runtimeDeliveries(cut processruntime.RecordedCut) []processruntime.Admission {
	result := cut.Result()
	switch cut.Operation() {
	case processruntime.RequestAdmissionOperation, processruntime.CancelAdmissionOperation, processruntime.ReturnGrantOperation:
		return result.Admission().Deliveries()
	case processruntime.BindConfirmationBarrierOperation:
		return result.Barrier().Deliveries()
	case processruntime.CompleteConfirmationQueueOperation:
		return result.Queue().Deliveries()
	case processruntime.ObserveAttemptOperation:
		return result.Receipt().Deliveries()
	default:
		return nil
	}
}

// NewMachine starts a campaign through the production reducer.
func NewMachine(definition Definition) (Machine, Transition) {
	state, effects := beginCampaign(campaignDefinition{
		identity: campaignIdentity(definition.Identity), lineage: definition.Lineage,
		command: slices.Clone(definition.Command), env: slices.Clone(definition.Env),
		profile: definition.Profile, peers: definition.Peers,
	})
	return Machine{state: state}, transitionFrom(campaignState{}, state, campaignEvent{}, effects)
}

// Apply accepts one campaign fact and returns its normalized effects.
func (machine Machine) Apply(fact Fact) (Machine, Transition) {
	state, effects := advanceCampaign(machine.state, campaignEvent{
		id: campaignEventID(len(machine.state.trace) + 1), payload: fact.payload, label: fact.label,
	})
	event := campaignEvent{id: campaignEventID(len(machine.state.trace) + 1), payload: fact.payload, label: fact.label}
	return Machine{state: state}, transitionFrom(machine.state, state, event, effects)
}

// Event returns the accepted campaign event.
func (transition Transition) Event() Event { return transition.event }

// Effects returns the ordered immutable campaign effects.
func (transition Transition) Effects() []Effect {
	result := make([]Effect, len(transition.effects))
	for index, effect := range transition.effects {
		result[index] = Effect{value: effect}
	}
	return result
}

// Projection returns the state after the accepted campaign event.
func (transition Transition) Projection() Projection { return transition.projection }

// Outcome returns terminal campaign evidence when available.
func (machine Machine) Outcome() Outcome {
	switch machine.state.outcome.(type) {
	case noMutantsOutcome:
		return Outcome{kind: NoMutantsOutcome}
	case completedOutcome:
		return Outcome{kind: CompletedOutcome}
	case abortedOutcome:
		return Outcome{kind: AbortedOutcome}
	default:
		return Outcome{}
	}
}

// Mutations returns detached terminal mutation evidence.
func (machine Machine) Mutations() []MutationEvidence {
	outcome, ok := machine.state.outcome.(completedOutcome)
	if !ok {
		return nil
	}
	result := make([]MutationEvidence, len(outcome.mutants))
	for index, mutant := range outcome.mutants {
		result[index] = MutationEvidence{
			identity: string(mutant.mutant), result: presentManagedMutation(mutant.kind),
			primary: AttemptKind(mutant.primary.kind), confirmation: AttemptKind(mutant.confirmation.kind),
		}
	}
	return result
}

// Failed reports whether the campaign carries infrastructure failure evidence.
func (machine Machine) Failed() bool { return machine.state.failure != nil }

// CleanupUnconfirmed reports whether campaign failure retains unresolved custody.
func (machine Machine) CleanupUnconfirmed() bool {
	_, ok := machine.state.failure.(cleanupUnconfirmedFault)
	return ok
}

// CommandCount returns the number of accepted start commitments.
func (machine Machine) CommandCount() int { return machine.state.commandCount() }

// SingleAdmissionFallback reports whether capacity pressure reduced automatic admission.
func (machine Machine) SingleAdmissionFallback() bool { return machine.state.singleAdmissionFallback }

// Projection returns an immutable campaign-state view.
func (machine Machine) Projection() Projection {
	return Projection{state: machine.state.clone()}
}

// Accepts reports whether a fact is legal at the current campaign state.
func (machine Machine) Accepts(fact Fact) (accepted bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if _, rejected := recovered.(Violation); rejected {
				accepted = false
				return
			}
			panic(recovered)
		}
	}()
	_, _ = machine.Apply(fact)
	return true
}

func (machine Machine) effectPending(effect Effect) bool {
	if effect.value.attempt == "" {
		return true
	}
	attemptAt := machine.state.attemptIndex(effect.value.attempt)
	if attemptAt < 0 {
		return false
	}
	stage := machine.state.attempts[attemptAt].stage
	switch effect.value.kind {
	case campaignEffectCancelAdmission:
		return stage == campaignAttemptAdmissionWaiting || stage == campaignAttemptGranted
	case campaignEffectRequestStartCommitment:
		return stage == campaignAttemptGranted || stage == campaignAttemptReturningGrant
	default:
		return true
	}
}

// EmergencyRequested reports whether campaign cleanup awaits runtime-wide settlement.
func (machine Machine) EmergencyRequested() bool {
	_, requested := machine.state.runtimeEmergencySettlementRequest()
	return requested
}

func transitionFrom(previous, state campaignState, event campaignEvent, effects []campaignEffect) Transition {
	event = enrichCampaignEvent(previous, state, event)
	return Transition{
		event: Event{value: event, previous: previous.clone()}, effects: effects,
		projection: Projection{state: state.clone()},
	}
}

func enrichCampaignEvent(previous, state campaignState, event campaignEvent) campaignEvent {
	terminal, terminalEvent := event.payload.(attemptTerminalEvent)
	if terminalEvent {
		attemptAt := previous.attemptIndex(terminal.attempt)
		if attemptAt >= 0 {
			attempt := previous.attempts[attemptAt]
			if attempt.kind == campaignAttemptBaseline {
				event.baselinePassed = state.baselineEvidence.passed
			} else if mutantAt := state.mutantIndex(attempt.mutant); mutantAt >= 0 {
				previousAt := previous.mutantIndex(attempt.mutant)
				if previousAt >= 0 && previous.mutants[previousAt].result == 0 {
					event.mutationOutcome = state.mutants[mutantAt].result
				}
			}
		}
	}
	if _, committed := event.payload.(terminalCommittedEvent); committed {
		switch state.outcome.(type) {
		case noMutantsOutcome:
			event.terminalOutcome = campaignTerminalNoMutants
		case completedOutcome:
			event.terminalOutcome = campaignTerminalCompleted
		case abortedOutcome:
			event.terminalOutcome = campaignTerminalAborted
		}
	}
	return event
}

// Event returns the normalized event represented by this fact.
func (fact Fact) Event() Event {
	return Event{value: campaignEvent{payload: fact.payload, label: fact.label}}
}

// Registered records process-runtime campaign registration.
func Registered(registration processruntime.Registration) Fact {
	return Fact{payload: campaignRegisteredEvent{registration: campaignRegistration{
		decision: registration.Decision(), token: campaignTokenValue(registration.Campaign()),
	}}}
}

// SnapshotEstablished records one immutable repository snapshot.
func SnapshotEstablished(snapshot string) Fact {
	return Fact{payload: snapshotEstablishedEvent{snapshot: snapshotIdentity(snapshot)}}
}

// CatalogueDiscovered records the stable mutant catalogue.
func CatalogueDiscovered(snapshot string, mutants []string) Fact {
	identities := make([]mutantIdentity, len(mutants))
	for index, mutant := range mutants {
		identities[index] = mutantIdentity(mutant)
	}
	return Fact{payload: catalogueDiscoveredEvent{snapshot: snapshotIdentity(snapshot), mutants: identities}}
}

// ResourceSettled records authoritative campaign resource cleanup.
func ResourceSettled(kind ResourceKind, identity string) Fact {
	return Fact{payload: resourceSettledEvent{kind: campaignResourceKind(kind), identity: identity}}
}

// TerminalCommitted records process-runtime terminal authorization.
func TerminalCommitted(decision processruntime.TerminalDecision) Fact {
	return Fact{payload: terminalCommittedEvent{result: campaignTerminalResult{decision: decision}}}
}

// PreparationFailed records failure to establish the snapshot or catalogue.
func PreparationFailed(catalogue bool, cause string) Fact {
	stage := campaignPreparingSnapshot
	if catalogue {
		stage = campaignPreparingCatalogue
	}
	return Fact{payload: campaignPreparationFailedEvent{stage: stage, cause: cause}}
}

// ResourceSettlementFailed records failed authoritative resource cleanup.
func ResourceSettlementFailed(kind ResourceKind, identity, cause string) Fact {
	return Fact{payload: resourceSettlementFailedEvent{
		kind: campaignResourceKind(kind), identity: identity, cause: cause,
	}}
}

// WorkspaceMaterialized records one materialized mutation workspace.
func WorkspaceMaterialized(effect Effect, workspace string) Fact {
	return Fact{payload: workspaceMaterializedEvent{
		attempt: effect.value.attempt, workspace: workspace, snapshot: effect.value.snapshot,
	}}
}

// WorkspaceMaterializationFailed records failed workspace materialization.
func WorkspaceMaterializationFailed(effect Effect, cause string, residue []string) Fact {
	return Fact{payload: workspaceMaterializationFailedEvent{
		attempt: effect.value.attempt, cause: cause, artifactResidue: slices.Clone(residue),
	}}
}

// AdmissionGranted records a delivered process-runtime admission grant.
func AdmissionGranted(effect Effect, grant processruntime.Admission) Fact {
	return Fact{payload: admissionGrantedEvent{
		attempt: effect.value.attempt, grant: campaignAdmissionFact(grant),
	}}
}

// AdmissionDelivered records a process-runtime admission grant for its own attempt.
func AdmissionDelivered(grant processruntime.Admission) Fact {
	return Fact{payload: admissionGrantedEvent{
		attempt: supervision.Identity(grant.Attempt), grant: campaignAdmissionFact(grant),
	}}
}

// AdmissionRejected records a rejected process-runtime admission request.
func AdmissionRejected(effect Effect, result processruntime.AdmissionResult, cause string) Fact {
	return Fact{payload: admissionRejectedEvent{
		attempt: effect.value.attempt, result: campaignAdmissionEvidence(runtimeAdmissionResult(result)), cause: cause,
	}}
}

// AdmissionCancelled records process-runtime admission cancellation.
func AdmissionCancelled(effect Effect, result processruntime.AdmissionResult) Fact {
	return Fact{payload: admissionCancelledEvent{
		attempt: effect.value.attempt, request: effect.value.request,
		result: campaignAdmissionEvidence(runtimeAdmissionResult(result)),
	}}
}

// GrantReturnAcknowledged records a returned process-runtime admission grant.
func GrantReturnAcknowledged(effect Effect, result processruntime.AdmissionResult) Fact {
	return Fact{payload: grantReturnAcknowledgedEvent{
		grant: effect.value.grant, result: campaignAdmissionEvidence(runtimeAdmissionResult(result)),
	}}
}

// ConfirmationBarrierBound records a bound exclusive confirmation barrier.
func ConfirmationBarrierBound(effect Effect, result processruntime.BarrierResult) Fact {
	return Fact{payload: confirmationBarrierBoundEvent{
		attempt: effect.value.attempt, result: campaignBarrierEvidence(runtimeBarrierResult(result)),
	}}
}

// StartCommitted records a process-runtime start decision.
func StartCommitted(effect Effect, result processruntime.StartResult) Fact {
	return Fact{payload: startCommittedEvent{
		attempt: effect.value.attempt, grant: effect.value.grant, result: campaignStartEvidence(runtimeStartResult(result)),
	}}
}

// AttemptLaunched records supervision's launch result and its runtime receipt.
func AttemptLaunched(effect Effect, result supervision.LaunchResult, receipt processruntime.Receipt) Fact {
	observation := campaignLaunchObservation{}
	switch value := result.(type) {
	case supervision.Owned:
		observation.kind = campaignLaunchOwned
	case supervision.NotReleased:
		observation.kind, observation.failure = campaignLaunchNotReleased, value.Kind
	case supervision.LaunchUnconfirmed:
		observation.kind, observation.residual = campaignLaunchUnconfirmed, value.Residual
	default:
		panic("campaign launch result is unknown")
	}
	return Fact{payload: attemptLaunchEvent{
		attempt: effect.value.attempt, generation: effect.value.generation,
		result: observation, receipt: campaignReceipt(receipt),
	}}
}

// AttemptTerminal records supervision's terminal result and its runtime receipt.
func AttemptTerminal(effect Effect, terminal supervision.Terminal, receipt processruntime.Receipt, deadline time.Duration) Fact {
	event := attemptTerminalEvent{
		attempt: effect.value.attempt, generation: effect.value.generation,
		terminal: terminal, receipt: campaignReceipt(receipt),
	}
	if deadline != 0 {
		event.resolvedMutationDeadline = recordedMutationDeadline(deadline)
	}
	return Fact{payload: event}
}

// WithConfirmationQueueCompleted records the runtime's queue-completion result on a terminal fact.
func (fact Fact) WithConfirmationQueueCompleted(result processruntime.QueueResult) Fact {
	event, ok := fact.payload.(attemptTerminalEvent)
	if !ok {
		panic("campaign confirmation completion requires a terminal fact")
	}
	event.receipt.confirmationQueueDrained = result.Decision() == processruntime.ConfirmationQueueCompleted
	return Fact{payload: event, label: fact.label}
}

// WithRecordedEvidence preserves authoritative evidence carried only by the recorded fact.
func (fact Fact) WithRecordedEvidence(recorded Fact) Fact {
	derived, derivedOK := fact.payload.(attemptTerminalEvent)
	authority, recordedOK := recorded.payload.(attemptTerminalEvent)
	if derivedOK && recordedOK {
		derived.resolvedMutationDeadline = authority.resolvedMutationDeadline
		fact.payload = derived
	}
	fact.label = recorded.label
	return fact
}

// ResolvedMutationDeadline returns the mutation deadline retained by terminal evidence.
func (fact Fact) ResolvedMutationDeadline() (time.Duration, bool) {
	event, ok := fact.payload.(attemptTerminalEvent)
	if !ok || event.resolvedMutationDeadline.duration == 0 {
		return 0, false
	}
	return event.resolvedMutationDeadline.duration, true
}

// RuntimeEmergencyStarted records one process-runtime fatal closure.
func RuntimeEmergencyStarted(closure processruntime.Closure) Fact {
	return Fact{payload: runtimeEmergencyStartedEvent{closure: campaignClosureValue(runtimeClosureValue(closure))}}
}

// RuntimeEmergencySettled records exact runtime-wide emergency settlement.
func RuntimeEmergencySettled(settlement processruntime.EmergencySettlement) Fact {
	value := runtimeEmergencySettlement(settlement)
	return Fact{payload: runtimeEmergencySettledEvent{
		epoch: value.epoch, settlement: campaignSettlementValue(value),
	}}
}
