package campaign

import (
	"fmt"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
	"strconv"

	"github.com/gtramontina/ooze/internal/ooze/internal/processruntime"
)

func (runner *managedCampaignRunner) execute(
	effect campaignEffect,
	request managedCampaignRequest,
) []campaignEffect {
	switch effect.kind {
	case campaignEffectRegister:
		registered := runner.runtime.RegisterCampaign(request.lineage)
		registration := campaignRegistrationEvidence(registered)
		runner.runtimeToken = registration.token

		return runner.advance(campaignRegisteredEvent{registration: registration})
	case campaignEffectEstablishSnapshot:
		snapshot, cause := managedBoundary(func() TemporaryRepository {
			return runner.repository.MaterializeTemporaryRepository(runner.temporaryDirectory.New())
		})
		if cause != "" {
			return runner.advance(campaignPreparationFailedEvent{
				stage: campaignPreparingSnapshot, cause: "repository snapshot could not be materialized",
			})
		}
		runner.snapshot = snapshot

		return runner.advance(snapshotEstablishedEvent{snapshot: snapshotIdentity(runner.snapshot.Root())})
	case campaignEffectDiscoverCatalogue:
		return runner.discover(effect, request)
	case campaignEffectMaterializeWorkspace:
		var workspace TemporaryRepository
		_, cause := managedBoundary(func() struct{} {
			workspace = runner.snapshot.MaterializeTemporaryRepository(runner.temporaryDirectory.New())
			if mutation := runner.mutations[effect.mutant]; mutation != nil {
				mutation.WriteTo(workspace)
			}
			return struct{}{}
		})
		if cause != "" {
			var artifactResidue []string
			cleanupUnconfirmed := false
			if workspace != nil {
				root, rootCause := managedBoundary(workspace.Root)
				_, cleanupCause := managedBoundary(func() struct{} { workspace.Remove(); return struct{}{} })
				if cleanupCause != "" {
					cleanupUnconfirmed = true
					if rootCause == "" && root != "" {
						artifactResidue = append(artifactResidue, root)
					}
				}
			}
			publicCause := "mutation workspace could not be materialized"
			if cleanupUnconfirmed {
				publicCause += "; workspace cleanup could not be confirmed"
			}
			return runner.advance(workspaceMaterializationFailedEvent{
				attempt: effect.attempt, cause: publicCause, artifactResidue: artifactResidue,
			})
		}
		runner.workspaces[workspace.Root()] = workspace

		return runner.advance(workspaceMaterializedEvent{
			attempt: effect.attempt, workspace: workspace.Root(), snapshot: effect.snapshot,
		})
	case campaignEffectRequestAdmission:
		await := runner.runtime.RequestAdmission(processRuntimeAdmission(effect.request))
		if await.Decision() != processruntime.AdmissionAccepted {
			return runner.advance(admissionRejectedEvent{
				attempt: effect.attempt,
				result: campaignAdmissionResult{
					decision: await.Decision(), request: campaignAdmissionFact(await.Request()),
					fatalEpoch: fatalEpochID(runner.runtime.FatalEpoch()),
				},
				cause: "managed admission rejected",
			})
		}
		grant, ok := await.Receive()
		if !ok {
			return runner.advance(admissionRejectedEvent{
				attempt: effect.attempt,
				result: campaignAdmissionResult{
					decision: processruntime.AdmissionRejectedClosed, request: campaignAdmissionFact(await.Request()),
					fatalEpoch: fatalEpochID(runner.runtime.FatalEpoch()),
				},
				cause: "process runtime entered a fatal epoch while admission waited",
			})
		}
		runner.rememberAuthority(grant)

		return runner.advance(admissionGrantedEvent{attempt: effect.attempt, grant: campaignAdmissionFact(grant.Admission())})
	case campaignEffectRequestStartCommitment:
		grant := runner.authority(effect.grant)
		cell := processruntime.NewStartCell()
		runner.attempts.reserveLaunch(cell, effect.spec)
		prepared := runner.runtime.CommitStart(grant, cell)
		result := campaignStartResult{decision: prepared.Decision(), generation: prepared.Generation()}
		if prepared.Decision() == processruntime.StartAccepted {
			runner.starts[prepared.Generation()] = prepared
		} else {
			runner.attempts.discardLaunch(cell)
		}

		return runner.advance(startCommittedEvent{
			attempt: effect.attempt, grant: effect.grant, result: result,
		})
	case campaignEffectLaunchAttempt:
		runner.attemptFacts[effect.generation] = managedAttemptFacts{
			kind: effect.attemptKind, completesConfirmationQueue: effect.completesConfirmationQueue,
		}
		return runner.launch(effect, request)
	case campaignEffectStopAttempt:
		owned := runner.owned[effect.generation]
		if owned == nil {
			panic("managed stop attempt is not owned")
		}
		runner.attempts.stop(owned)

		return nil
	case campaignEffectCancelAdmission:
		decision := runner.runtime.CancelAdmission(processRuntimeAdmission(effect.request))

		return runner.advance(admissionCancelledEvent{
			attempt: effect.attempt, request: effect.request,
			result: campaignAdmissionResult{decision: decision, request: effect.request},
		})
	case campaignEffectReturnAdmission:
		decision := runner.runtime.ReturnGrant(runner.authority(effect.grant))

		return runner.advance(grantReturnAcknowledgedEvent{
			grant: effect.grant, result: campaignAdmissionResult{decision: decision, request: effect.grant},
		})
	case campaignEffectReleaseWorkspace:
		workspace := runner.workspaces[effect.workspace]
		if workspace == nil {
			panic("managed workspace is missing")
		}
		_, cause := managedBoundary(func() struct{} { workspace.Remove(); return struct{}{} })
		delete(runner.workspaces, effect.workspace)
		if cause != "" {
			return runner.advance(resourceSettlementFailedEvent{
				kind: campaignResourceWorkspace, identity: effect.workspace,
				cause: "mutation workspace cleanup could not be confirmed",
			})
		}

		return runner.advance(resourceSettledEvent{
			kind: campaignResourceWorkspace, identity: effect.workspace,
		})
	case campaignEffectBindConfirmationBarrier:
		await := runner.runtime.BindConfirmationBarrier(processruntime.Barrier{
			Campaign: effect.binding.campaign, Attempt: string(effect.binding.attempt),
			Profile: effect.binding.profile, Deadline: effect.binding.deadline,
		})
		if await.Decision() != processruntime.BarrierBound {
			panic("managed confirmation barrier was rejected")
		}
		grant, ok := await.Receive()
		if !ok {
			panic("managed confirmation barrier closed without a grant")
		}
		runner.rememberAuthority(grant)

		return runner.advance(confirmationBarrierBoundEvent{
			attempt: effect.attempt,
			result: campaignBarrierResult{
				decision: await.Decision(), request: campaignAdmissionFact(await.Request()),
				deliveries: []campaignAdmission{campaignAdmissionFact(grant.Admission())},
			},
		})
	case campaignEffectReleaseSnapshot:
		_, cause := managedBoundary(func() struct{} { runner.snapshot.Remove(); return struct{}{} })
		if cause != "" {
			return runner.advance(resourceSettlementFailedEvent{
				kind: campaignResourceSnapshot, identity: string(effect.snapshot),
				cause: "repository snapshot cleanup could not be confirmed",
			})
		}

		return runner.advance(resourceSettledEvent{
			kind: campaignResourceSnapshot, identity: string(effect.snapshot),
		})
	case campaignEffectProposeTerminal:
		var committed processruntime.TerminalResult
		if effect.fatalEpoch != 0 {
			committed = runner.runtime.AuthorizeForcedAbort(runner.runtimeToken, uint64(effect.fatalEpoch))
		} else {
			committed = runner.runtime.CommitTerminal(runner.runtimeToken)
		}

		return runner.advance(terminalCommittedEvent{result: campaignTerminalResult{decision: committed.Decision()}})
	default:
		panic("managed campaign effect is not implemented")
	}
}

func managedBoundary[T any](operation func() T) (value T, cause string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			cause = fmt.Sprint(recovered)
		}
	}()

	return operation(), ""
}

func (runner *managedCampaignRunner) discover(
	effect campaignEffect,
	request managedCampaignRequest,
) []campaignEffect {
	mutants, cause := managedBoundary(func() []mutantIdentity {
		discovered := make([]mutantIdentity, 0)
		for _, source := range runner.snapshot.ListGoSourceFiles() {
			for _, virus := range request.viruses {
				for _, infected := range source.Incubate(virus) {
					mutation := infected.Mutate()
					identity := mutantIdentity("mutant-" + strconv.Itoa(len(discovered)+1))
					discovered = append(discovered, identity)
					runner.mutations[identity] = mutation
				}
			}
		}
		return discovered
	})
	if cause != "" {
		return runner.advance(campaignPreparationFailedEvent{
			stage: campaignPreparingCatalogue, cause: "mutation catalogue discovery failed",
		})
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
	case supervision.Owned:
		observation.kind = campaignLaunchOwned
		runner.owned[effect.generation] = result.Attempt
	case supervision.NotReleased:
		observation.kind = campaignLaunchNotReleased
		observation.failure = result.Kind
	case supervision.LaunchUnconfirmed:
		observation.kind = campaignLaunchUnconfirmed
		observation.residual = result.Residual
	default:
		panic("managed attempt returned an unknown launch result")
	}
	effects := runner.advance(attemptLaunchEvent{
		attempt: effect.attempt, generation: effect.generation,
		result: observation, receipt: campaignReceipt(launched.receipt),
	})
	owned := runner.owned[effect.generation]
	if owned == nil {
		delete(runner.attemptFacts, effect.generation)
		return effects
	}
	runner.pending++
	go func() {
		runner.terminals <- managedTerminalObservation{
			attempt: effect.attempt, generation: effect.generation,
			observed: runner.attempts.wait(effect.generation, owned),
		}
	}()

	return effects
}

func (runner *managedCampaignRunner) settle(
	terminal managedTerminalObservation,
	request managedCampaignRequest,
) []campaignEffect {
	delete(runner.owned, terminal.generation)
	facts, known := runner.attemptFacts[terminal.generation]
	delete(runner.attemptFacts, terminal.generation)
	if !known {
		panic("managed terminal attempt facts are missing")
	}
	observed := terminal.observed
	receipt := campaignReceipt(observed.receipt)
	if facts.completesConfirmationQueue {
		completed := runner.runtime.CompleteConfirmationQueue(runner.runtimeToken)
		if completed.Decision() != processruntime.ConfirmationQueueCompleted {
			panic("managed confirmation queue completion was rejected")
		}
		receipt.confirmationQueueDrained = true
	}
	terminalEvent := attemptTerminalEvent{
		attempt: terminal.attempt, generation: terminal.generation,
		terminal: observed.terminal, receipt: receipt,
	}
	if facts.kind == campaignAttemptBaseline {
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

	return runner.advance(terminalEvent)
}
