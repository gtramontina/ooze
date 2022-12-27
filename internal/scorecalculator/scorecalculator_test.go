package scorecalculator_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/scorecalculator"
	"github.com/stretchr/testify/assert"
)

func TestScoreCalculator(t *testing.T) {
	calculator := scorecalculator.New()

	t.Run("returns -1 when the total number of mutants is less than or equal to zero", func(t *testing.T) {
		assert.Equal(t, float32(-1), calculator(-1, 0))
		assert.Equal(t, float32(-1), calculator(0, 0))
	})

	t.Run("returns the ratio of killed mutants", func(t *testing.T) {
		assert.Equal(t, float32(0), calculator(1, 0))
		assert.Equal(t, float32(1), calculator(1, 1))
		assert.Equal(t, float32(0.5), calculator(2, 1))
		assert.Equal(t, float32(0.25), calculator(4, 1))
		assert.Equal(t, float32(0.15), calculator(140, 21))
	})
}
