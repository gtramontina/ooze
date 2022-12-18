package basicreporter_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/basicreporter"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
)

func TestBasicReporter(t *testing.T) {
	t.Parallel()

	t.Run("calculates score as -1 when there's no diagnostics", func(t *testing.T) {
		t.Parallel()

		reporter := basicreporter.New()
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    0,
			Survived: 0,
			Killed:   0,
			Score:    -1,
		}, reporter.GetSummary())
	})

	t.Run("calculates score as 0 when all mutants survived", func(t *testing.T) {
		t.Parallel()

		reporter := basicreporter.New()

		reporter.AddDiagnostic(result.Err[string]("mutant #1 survived"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    1,
			Survived: 1,
			Killed:   0,
			Score:    0,
		}, reporter.GetSummary())

		reporter.AddDiagnostic(result.Err[string]("mutant #2 survived"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    2,
			Survived: 2,
			Killed:   0,
			Score:    0,
		}, reporter.GetSummary())
	})

	t.Run("calculates score as 1 when all mutants are killed", func(t *testing.T) {
		t.Parallel()

		reporter := basicreporter.New()

		reporter.AddDiagnostic(result.Ok("mutant #1 killed"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    1,
			Survived: 0,
			Killed:   1,
			Score:    1,
		}, reporter.GetSummary())

		reporter.AddDiagnostic(result.Ok("mutant #1 killed"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    2,
			Survived: 0,
			Killed:   2,
			Score:    1,
		}, reporter.GetSummary())
	})

	t.Run("calculates score accordingly upon multiple different diagnostics", func(t *testing.T) {
		t.Parallel()

		reporter := basicreporter.New()

		reporter.AddDiagnostic(result.Ok("mutant #1 killed"))
		reporter.AddDiagnostic(result.Err[string]("mutant #2 survived"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    2,
			Survived: 1,
			Killed:   1,
			Score:    1.0 / 2.0,
		}, reporter.GetSummary())

		reporter.AddDiagnostic(result.Err[string]("mutant #3 survived"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    3,
			Survived: 2,
			Killed:   1,
			Score:    1.0 / 3.0,
		}, reporter.GetSummary())

		reporter.AddDiagnostic(result.Ok("mutant #4 killed"))
		reporter.AddDiagnostic(result.Ok("mutant #4 killed"))
		reporter.Summarize()
		assert.Equal(t, &basicreporter.Summary{
			Total:    5,
			Survived: 2,
			Killed:   3,
			Score:    3.0 / 5.0,
		}, reporter.GetSummary())
	})
}
