package fakescorecalculator

import (
	"github.com/gtramontina/ooze/internal/ooze"
)

func Always(score float32) ooze.ScoreCalculator {
	return func(_total, _killed int) float32 { //nolint:revive
		return score
	}
}
