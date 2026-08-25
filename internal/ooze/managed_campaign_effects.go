package ooze

import (
	"fmt"
	"strconv"
)

func (runner *managedCampaignRunner) execute(
	effect campaignEffect,
	request managedCampaignRequest,
) []campaignEffect {
	switch effect.kind {
	case campaignEffectRegister:
		registration := runner.runtime.registerCampaign(campaignProvenance{lineage: request.lineage})
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
		await := runner.runtime.requestAdmission(runtimeAdmissionRequest(effect.request))
		runner.rememberAuthority(await.request)
		if await.decision != admissionAccepted {
			return runner.advance(admissionRejectedEvent{
				attempt: effect.attempt,
				result: campaignAdmissionResult{
					decision: await.decision, request: campaignAdmissionFact(await.request), fatalEpoch: await.fatal,
				},
				cause: "managed admission rejected",
			})
		}
		grant, ok := <-await.delivery
		if !ok {
			return runner.advance(admissionRejectedEvent{
				attempt: effect.attempt,
				result: campaignAdmissionResult{
					decision: admissionRejectedClosed, request: campaignAdmissionFact(await.request),
					fatalEpoch: runner.runtime.fatalEpoch(),
				},
				cause: "process runtime entered a fatal epoch while admission waited",
			})
		}
		runner.rememberAuthority(grant)

		return runner.advance(admissionGrantedEvent{attempt: effect.attempt, grant: campaignAdmissionFact(grant)})
	case campaignEffectRequestStartCommitment:
		grant := runner.authority(effect.grant)
		cell := &pendingStartCell{}
		runner.attempts.reserveLaunch(cell, effect.spec)
		prepared := runner.runtime.startCommitted(grant, startInstallation{
			grant: grant, cell: cell,
		})
		if prepared.result.decision == startCommittedAccepted {
			runner.starts[prepared.result.generation] = prepared.start
		}

		return runner.advance(startCommittedEvent{
			attempt: effect.attempt, grant: effect.grant, result: campaignStartEvidence(prepared.result),
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
		cancelled := runner.runtime.cancelAdmission(runner.authority(effect.request))

		return runner.advance(admissionCancelledEvent{
			attempt: effect.attempt, request: effect.request, result: campaignAdmissionEvidence(cancelled),
		})
	case campaignEffectReturnAdmission:
		returned := runner.runtime.acknowledgeGrantReturn(runner.authority(effect.grant))

		return runner.advance(grantReturnAcknowledgedEvent{
			grant: effect.grant, result: campaignAdmissionEvidence(returned),
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
		await := runner.runtime.sealAndBindConfirmationBarrier(runtimeBarrierBinding(effect.binding))
		if await.decision != barrierBound {
			panic("managed confirmation barrier was rejected")
		}
		grant, ok := <-await.delivery
		if !ok {
			panic("managed confirmation barrier closed without a grant")
		}
		runner.rememberAuthority(await.request)
		runner.rememberAuthority(grant)

		return runner.advance(confirmationBarrierBoundEvent{
			attempt: effect.attempt,
			result: campaignBarrierEvidence(barrierResult{
				decision: await.decision, request: await.request, deliveries: []admissionGrant{grant},
			}),
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
		var committed terminalResult
		if effect.fatalEpoch != 0 {
			committed = runner.runtime.authorizeForcedAbort(runner.runtimeToken, effect.fatalEpoch)
		} else {
			committed = runner.runtime.commitTerminal(runner.runtimeToken)
		}

		return runner.advance(terminalCommittedEvent{result: campaignTerminalEvidence(committed)})
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
	if facts.completesConfirmationQueue {
		completed := runner.runtime.completeConfirmationQueue(runner.runtimeToken)
		if completed.decision != confirmationQueueCompleted {
			panic("managed confirmation queue completion was rejected")
		}
		observed.receipt.confirmationQueueDrained = true
	}
	terminalEvent := attemptTerminalEvent{
		attempt: terminal.attempt, generation: terminal.generation,
		terminal: observed.terminal, receipt: campaignReceipt(observed.receipt),
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
