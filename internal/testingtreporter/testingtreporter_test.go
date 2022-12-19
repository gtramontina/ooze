package testingtreporter_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/oozetesting/faketestingt"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/testingtreporter"
	"github.com/stretchr/testify/assert"
)

func TestName(t *testing.T) {
	t.Parallel()

	t.Run("summary is a test helper", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT)
		reporter.Summarize()

		assert.Equal(t, 1, fakeT.HelperCalls())
	})

	t.Run("reports summary when there are no diagnostics", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT)
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        0",
			"• Killed:       0",
			"• Survived:     0",
			"• Score:    -1.00",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})

	t.Run("reports summary when there is one positive diagnostic", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT)
		reporter.AddDiagnostic(result.Ok("mutant killed"))
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        1",
			"• Killed:       1",
			"• Survived:     0",
			"• Score:     1.00",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})

	t.Run("reports summary when there is one negative diagnostic", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT)
		reporter.AddDiagnostic(result.Err[string]("mutant survived"))
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        1",
			"• Killed:       0",
			"• Survived:     1",
			"• Score:     0.00",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})

	t.Run("reports summary when there are multiple mixed diagnostics", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT)
		reporter.AddDiagnostic(result.Err[string]("mutant survived"))
		reporter.AddDiagnostic(result.Ok("mutant killed"))
		reporter.AddDiagnostic(result.Ok("mutant killed"))
		reporter.AddDiagnostic(result.Ok("mutant killed"))
		reporter.AddDiagnostic(result.Err[string]("mutant survived"))
		reporter.AddDiagnostic(result.Err[string]("mutant survived"))
		reporter.AddDiagnostic(result.Ok("mutant killed"))
		reporter.AddDiagnostic(result.Ok("mutant killed"))
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        8",
			"• Killed:       5",
			"• Survived:     3",
			"• Score:     0.62",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})
}
