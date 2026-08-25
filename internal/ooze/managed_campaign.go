package ooze

import (
	"strconv"
	"strings"
	"time"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/viruses"
)

type managedTemporaryDirectoryFactory interface{ New() string }

type managedObservedLaunch struct {
	result  LaunchResult
	receipt observationResult
}

type managedObservedTerminal struct {
	terminal Terminal
	receipt  observationResult
}

type managedObservedEmergency struct {
	epoch      fatalEpochID
	settlement emergencySettlement
}

type managedAttemptSystem interface {
	launch(installedStart, Spec) managedObservedLaunch
	wait(attemptGeneration, *OwnedAttempt) managedObservedTerminal
	stop(attemptGeneration)
	emergency(fatalEpochID) managedObservedEmergency
}

type managedCampaignConstruction struct {
	runtime            *processRuntimeShell
	repository         Repository
	temporaryDirectory managedTemporaryDirectoryFactory
	attempts           managedAttemptSystem
}

type managedCampaignRequest struct {
	identity        campaignIdentity
	lineage         campaignLineage
	command         []string
	env             []string
	profile         Profile
	peers           int
	mutationTimeout time.Duration
	viruses         []viruses.Virus
}

type managedCampaignResult struct {
	outcome   campaignOutcome
	failure   campaignFailure
	mutations map[mutantIdentity]*gomutatedfile.GoMutatedFile
}

type managedCampaignRunner struct {
	managedCampaignConstruction
	state      campaignState
	nextEvent  campaignEventID
	snapshot   TemporaryRepository
	mutations  map[mutantIdentity]*gomutatedfile.GoMutatedFile
	workspaces map[string]TemporaryRepository
	starts     map[attemptGeneration]installedStart
	owned      map[attemptGeneration]*OwnedAttempt
}

func newManagedCampaignRunner(construction managedCampaignConstruction) *managedCampaignRunner {
	if construction.runtime == nil || construction.repository == nil || construction.temporaryDirectory == nil ||
		construction.attempts == nil {
		panic("managed campaign construction is incomplete")
	}

	return &managedCampaignRunner{
		managedCampaignConstruction: construction,
		mutations:                   make(map[mutantIdentity]*gomutatedfile.GoMutatedFile),
		workspaces:                  make(map[string]TemporaryRepository),
		starts:                      make(map[attemptGeneration]installedStart),
		owned:                       make(map[attemptGeneration]*OwnedAttempt),
	}
}

func (runner *managedCampaignRunner) run(request managedCampaignRequest) managedCampaignResult {
	request.env = managedExecutionEnvironment(request.env, request.profile, request.peers)
	definition := campaignDefinition{
		identity: request.identity, lineage: request.lineage,
		command: request.command, env: request.env, profile: request.profile, peers: request.peers,
	}
	var effects []campaignEffect
	runner.state, effects = beginCampaign(definition)
	for len(effects) != 0 {
		var next []campaignEffect
		for _, effect := range effects {
			next = append(next, runner.execute(effect, request)...)
		}
		effects = next
	}

	return managedCampaignResult{
		outcome: runner.state.outcome, failure: runner.state.failure, mutations: runner.mutations,
	}
}

func (runner *managedCampaignRunner) execute(
	effect campaignEffect,
	request managedCampaignRequest,
) []campaignEffect {
	switch effect.kind {
	case campaignEffectRegister:
		registration := runner.runtime.registerCampaign(campaignProvenance{lineage: request.lineage})

		return runner.advance(campaignRegisteredEvent{registration: registration})
	case campaignEffectEstablishSnapshot:
		runner.snapshot = runner.repository.MaterializeTemporaryRepository(runner.temporaryDirectory.New())

		return runner.advance(snapshotEstablishedEvent{snapshot: snapshotIdentity(runner.snapshot.Root())})
	case campaignEffectDiscoverCatalogue:
		return runner.discover(effect, request)
	case campaignEffectMaterializeWorkspace:
		workspace := runner.snapshot.MaterializeTemporaryRepository(runner.temporaryDirectory.New())
		if mutation := runner.mutations[effect.mutant]; mutation != nil {
			mutation.WriteTo(workspace)
		}
		runner.workspaces[workspace.Root()] = workspace

		return runner.advance(workspaceMaterializedEvent{
			attempt: effect.attempt, workspace: workspace.Root(), snapshot: effect.snapshot,
		})
	case campaignEffectRequestAdmission:
		await := runner.runtime.requestAdmission(effect.request)
		if await.decision != admissionAccepted {
			return runner.advance(admissionRejectedEvent{
				attempt: effect.attempt,
				result:  admissionResult{decision: await.decision, request: await.request},
				cause:   "managed admission rejected",
			})
		}
		grant, ok := <-await.delivery
		if !ok {
			panic("managed admission closed without a grant")
		}

		return runner.advance(admissionGrantedEvent{attempt: effect.attempt, grant: grant})
	case campaignEffectRequestStartCommitment:
		prepared := runner.runtime.startCommitted(effect.grant, startInstallation{
			grant: effect.grant, cell: &pendingStartCell{},
		})
		if prepared.result.decision == startCommittedAccepted {
			runner.starts[prepared.result.generation] = prepared.start
		}

		return runner.advance(startCommittedEvent{
			attempt: effect.attempt, grant: effect.grant, result: prepared.result,
		})
	case campaignEffectLaunchAttempt:
		return runner.launch(effect, request)
	case campaignEffectCancelAdmission:
		cancelled := runner.runtime.cancelAdmission(effect.request)

		return runner.advance(admissionCancelledEvent{
			attempt: effect.attempt, request: effect.request, result: cancelled,
		})
	case campaignEffectReturnAdmission:
		returned := runner.runtime.acknowledgeGrantReturn(effect.grant)

		return runner.advance(grantReturnAcknowledgedEvent{grant: effect.grant, result: returned})
	case campaignEffectReleaseWorkspace:
		workspace := runner.workspaces[effect.workspace]
		if workspace == nil {
			panic("managed workspace is missing")
		}
		workspace.Remove()
		delete(runner.workspaces, effect.workspace)

		return runner.advance(resourceSettledEvent{
			kind: campaignResourceWorkspace, identity: effect.workspace,
		})
	case campaignEffectBindConfirmationBarrier:
		await := runner.runtime.sealAndBindConfirmationBarrier(effect.binding)
		if await.decision != barrierBound {
			panic("managed confirmation barrier was rejected")
		}
		grant, ok := <-await.delivery
		if !ok {
			panic("managed confirmation barrier closed without a grant")
		}

		return runner.advance(confirmationBarrierBoundEvent{
			attempt: effect.attempt,
			result: barrierResult{
				decision: await.decision, request: await.request, deliveries: []admissionGrant{grant},
			},
		})
	case campaignEffectReleaseSnapshot:
		runner.snapshot.Remove()

		return runner.advance(resourceSettledEvent{
			kind: campaignResourceSnapshot, identity: string(effect.snapshot),
		})
	case campaignEffectProposeTerminal:
		committed := runner.runtime.commitTerminal(runner.state.runtimeToken)

		return runner.advance(terminalCommittedEvent{result: committed})
	default:
		panic("managed campaign effect is not implemented")
	}
}

func (runner *managedCampaignRunner) discover(
	effect campaignEffect,
	request managedCampaignRequest,
) []campaignEffect {
	mutants := make([]mutantIdentity, 0)
	for _, source := range runner.snapshot.ListGoSourceFiles() {
		for _, virus := range request.viruses {
			for _, infected := range source.Incubate(virus) {
				mutation := infected.Mutate()
				identity := mutantIdentity("mutant-" + strconv.Itoa(len(mutants)+1))
				mutants = append(mutants, identity)
				runner.mutations[identity] = mutation
			}
		}
	}

	return runner.advance(catalogueDiscoveredEvent{snapshot: effect.snapshot, mutants: mutants})
}

func (runner *managedCampaignRunner) launch(
	effect campaignEffect,
	request managedCampaignRequest,
) []campaignEffect {
	start := runner.starts[effect.generation]
	delete(runner.starts, effect.generation)
	launched := runner.attempts.launch(start, effect.spec)
	observation := campaignLaunchObservation{}
	switch result := launched.result.(type) {
	case Owned:
		observation.kind = campaignLaunchOwned
		runner.owned[effect.generation] = result.Attempt
	case NotReleased:
		observation.kind = campaignLaunchNotReleased
		observation.failure = result.Kind
	case LaunchUnconfirmed:
		observation.kind = campaignLaunchUnconfirmed
		observation.residual = result.Residual
	default:
		panic("managed attempt returned an unknown launch result")
	}
	effects := runner.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: effect.generation,
		result: observation, receipt: launched.receipt,
	})
	owned := runner.owned[effect.generation]
	if owned == nil {
		return effects
	}
	observed := runner.attempts.wait(effect.generation, owned)
	delete(runner.owned, effect.generation)
	attemptAt := runner.state.attemptIndex(effect.attempt)
	if attemptAt >= 0 && runner.state.attempts[attemptAt].kind == campaignAttemptConfirmation &&
		len(runner.state.drain.provisionals) == 1 {
		completed := runner.runtime.completeConfirmationQueue(runner.state.runtimeToken)
		if completed.decision != confirmationQueueCompleted {
			panic("managed confirmation queue completion was rejected")
		}
		observed.receipt.confirmationQueueDrained = true
	}
	terminalEvent := attemptTerminalEvent{
		attempt: effect.attempt, generation: effect.generation,
		terminal: observed.terminal, receipt: observed.receipt,
	}
	if attemptAt >= 0 && runner.state.attempts[attemptAt].kind == campaignAttemptBaseline {
		data := terminalExecutionData(observed.terminal)
		peers := request.peers
		if request.profile == SerialProfile {
			peers = 1
		}
		plan, err := NewMutationAttemptPlan(MutationAttemptPlanInput{
			BaselineDuration: data.CommandDuration, Peers: peers,
			Override: request.mutationTimeout, Profile: request.profile,
		})
		if err == nil {
			terminalEvent.resolvedMutationDeadline = recordedMutationDeadline(plan.Deadline())
		}
	}

	return append(effects, runner.advance(terminalEvent)...)
}

func (runner *managedCampaignRunner) advance(payload campaignEventPayload) []campaignEffect {
	runner.nextEvent++
	var effects []campaignEffect
	runner.state, effects = advanceCampaign(runner.state, campaignEvent{id: runner.nextEvent, payload: payload})

	return effects
}

func managedExecutionEnvironment(environment []string, profile Profile, capacity int) []string {
	value := 1
	if profile == SerialProfile {
		value = capacity
	}
	const prefix = "GOMAXPROCS="
	resolved := make([]string, 0, len(environment)+1)
	for _, variable := range environment {
		if !strings.HasPrefix(variable, prefix) {
			resolved = append(resolved, variable)
		}
	}

	return append(resolved, prefix+strconv.Itoa(value))
}
