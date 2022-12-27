package scorecalculator

import (
	"github.com/gtramontina/ooze/internal/ooze"
)

func New() ooze.ScoreCalculator {
	return func(total, killed int) float32 {
		var score float32 = -1
		if total > 0 {
			score = float32(killed) / float32(total)
		}

		return score
	}
}
