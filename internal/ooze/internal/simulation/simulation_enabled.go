package simulation

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
	orderedCatalogue := make([]string, len(catalogue))
	for index, mutant := range catalogue {
		orderedCatalogue[index] = string(mutant)
	}
	moves := make([]simulationEnabledMove, 0, len(effects)+len(actions))
	for _, effect := range effects {
		moves = append(moves, simulationEnabledMove{
			authority: simulationEffectAuthority(effect.Owner()), effect: effect,
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
		if !first.effect.IsZero() || !second.effect.IsZero() {
			if first.effect.IsZero() {
				return false
			}
			if second.effect.IsZero() {
				return true
			}
			return first.effect.Less(second.effect, orderedCatalogue)
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

func simulationEffectAuthority(owner campaignmodule.Owner) simulationAuthority {
	switch owner {
	case campaignmodule.ArtifactOwner:
		return simulationCampaignAuthority
	case campaignmodule.RuntimeOwner:
		return simulationRuntimeAuthority
	case campaignmodule.SupervisionOwner:
		return supervisionAuthority
	default:
		panic("simulation campaign effect owner is invalid")
	}
}
