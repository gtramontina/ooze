package ooze

import (
	"sort"

	campaignmodule "github.com/gtramontina/ooze/internal/ooze/internal/campaign"
	"github.com/gtramontina/ooze/internal/ooze/internal/supervision"
)

type simulationEnabledMove struct {
	authority simulationAuthority
	effect    campaignmodule.Effect
	action    supervision.Effect
}

func simulationEnabledMoves(
	effects []campaignmodule.Effect,
	actions []supervision.Effect,
	catalogue []mutantIdentity,
) []simulationEnabledMove {
	ranks := make(map[mutantIdentity]int, len(catalogue))
	for rank, mutant := range catalogue {
		ranks[mutant] = rank + 1
	}
	moves := make([]simulationEnabledMove, 0, len(effects)+len(actions))
	for _, effect := range effects {
		moves = append(moves, simulationEnabledMove{
			authority: simulationEffectAuthority(effect.Kind()), effect: effect,
		})
	}
	for _, action := range actions {
		moves = append(moves, simulationEnabledMove{
			authority: supervisionAuthority, action: action,
		})
	}
	sort.SliceStable(moves, func(left, right int) bool {
		first, second := moves[left], moves[right]
		if first.authority != second.authority {
			return first.authority < second.authority
		}
		if first.effect.Kind() != 0 || second.effect.Kind() != 0 {
			if first.effect.Kind() == 0 {
				return false
			}
			if second.effect.Kind() == 0 {
				return true
			}
			firstRank, secondRank := ranks[first.effect.Mutant()], ranks[second.effect.Mutant()]
			if firstRank != secondRank {
				if firstRank == 0 {
					return false
				}
				if secondRank == 0 {
					return true
				}
				return firstRank < secondRank
			}
			if first.effect.Attempt() != second.effect.Attempt() {
				return first.effect.Attempt() < second.effect.Attempt()
			}
			if first.effect.Generation() != second.effect.Generation() {
				return first.effect.Generation() < second.effect.Generation()
			}
			if first.effect.ID() != second.effect.ID() {
				return first.effect.ID() < second.effect.ID()
			}
			return first.effect.Kind() < second.effect.Kind()
		}
		if first.action.Generation() != second.action.Generation() {
			return first.action.Generation() < second.action.Generation()
		}
		if first.action.Token() != second.action.Token() {
			return first.action.Token() < second.action.Token()
		}
		return first.action.Kind() < second.action.Kind()
	})

	return moves
}

func simulationEffectAuthority(kind campaignmodule.EffectKind) simulationAuthority {
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
		return supervisionAuthority
	default:
		panic("simulation campaign effect kind is invalid")
	}
}
