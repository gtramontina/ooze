package ooze

import "sort"

type simulationEnabledMove struct {
	authority simulationAuthority
	effect    campaignEffect
	action    supervisorAction
}

func simulationEnabledMoves(
	effects []campaignEffect,
	actions []supervisorAction,
	catalogue []mutantIdentity,
) []simulationEnabledMove {
	ranks := make(map[mutantIdentity]int, len(catalogue))
	for rank, mutant := range catalogue {
		ranks[mutant] = rank + 1
	}
	moves := make([]simulationEnabledMove, 0, len(effects)+len(actions))
	for _, effect := range effects {
		moves = append(moves, simulationEnabledMove{
			authority: simulationEffectAuthority(effect.kind), effect: effect,
		})
	}
	for _, action := range actions {
		moves = append(moves, simulationEnabledMove{
			authority: simulationSupervisorAuthority, action: action,
		})
	}
	sort.SliceStable(moves, func(left, right int) bool {
		first, second := moves[left], moves[right]
		if first.authority != second.authority {
			return first.authority < second.authority
		}
		if first.effect.kind != 0 || second.effect.kind != 0 {
			if first.effect.kind == 0 {
				return false
			}
			if second.effect.kind == 0 {
				return true
			}
			firstRank, secondRank := ranks[first.effect.mutant], ranks[second.effect.mutant]
			if firstRank != secondRank {
				if firstRank == 0 {
					return false
				}
				if secondRank == 0 {
					return true
				}
				return firstRank < secondRank
			}
			if first.effect.attempt != second.effect.attempt {
				return first.effect.attempt < second.effect.attempt
			}
			if first.effect.generation != second.effect.generation {
				return first.effect.generation < second.effect.generation
			}
			if first.effect.id != second.effect.id {
				return first.effect.id < second.effect.id
			}
			return first.effect.kind < second.effect.kind
		}
		if first.action.generation != second.action.generation {
			return first.action.generation < second.action.generation
		}
		if first.action.token != second.action.token {
			return first.action.token < second.action.token
		}
		return first.action.kind < second.action.kind
	})

	return moves
}

func simulationEffectAuthority(kind campaignEffectKind) simulationAuthority {
	switch kind {
	case campaignEffectEstablishSnapshot, campaignEffectDiscoverCatalogue,
		campaignEffectReleaseSnapshot, campaignEffectMaterializeWorkspace,
		campaignEffectReleaseWorkspace:
		return simulationCampaignAuthority
	case campaignEffectRegister, campaignEffectRequestAdmission,
		campaignEffectRequestStartCommitment, campaignEffectCancelAdmission,
		campaignEffectReturnAdmission, campaignEffectBindConfirmationBarrier,
		campaignEffectProposeTerminal:
		return simulationRuntimeAuthority
	case campaignEffectLaunchAttempt, campaignEffectStopAttempt:
		return simulationSupervisorAuthority
	default:
		panic("simulation campaign effect kind is invalid")
	}
}
