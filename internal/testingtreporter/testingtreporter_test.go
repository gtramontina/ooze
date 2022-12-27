package testingtreporter_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/oozetesting/fakescorecalculator"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestingt"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/testingtreporter"
	"github.com/stretchr/testify/assert"
)

func TestName(t *testing.T) {
	t.Run("summary is a test helper", func(t *testing.T) {
		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0), 0)
		reporter.Summarize()

		assert.Equal(t, 1, fakeT.HelperCalls())
	})

	t.Run("reports summary when there are no diagnostics", func(t *testing.T) {
		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0), 0)
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        0",
			"• Killed:       0",
			"• Survived:     0",
			"• Score:     0.00",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})

	t.Run("reports summary when there is one positive diagnostic", func(t *testing.T) {
		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0), 0)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        1",
			"• Killed:       1",
			"• Survived:     0",
			"• Score:     0.00",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})

	t.Run("reports summary when there is one negative diagnostic", func(t *testing.T) {
		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0), 0)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Err[string]("mutant survived")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
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
		fakeT := faketestingt.New()
		reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0), 0)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Err[string]("mutant survived")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Err[string]("mutant survived")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Err[string]("mutant survived")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.Summarize()

		assert.Equal(t, []string{
			"********************************************************************************",
			"• Total:        8",
			"• Killed:       5",
			"• Survived:     3",
			"• Score:     0.00",
			"********************************************************************************",
		}, fakeT.LogOutput())
	})

	t.Run("reports the score calculated by the given score calculator", func(t *testing.T) {
		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0.32), 0)
			reporter.Summarize()

			assert.Equal(t, []string{
				"********************************************************************************",
				"• Total:        0",
				"• Killed:       0",
				"• Survived:     0",
				"• Score:     0.32",
				"********************************************************************************",
			}, fakeT.LogOutput())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0.99), 0)
			reporter.Summarize()

			assert.Equal(t, []string{
				"********************************************************************************",
				"• Total:        0",
				"• Killed:       0",
				"• Survived:     0",
				"• Score:     0.99",
				"********************************************************************************",
			}, fakeT.LogOutput())
		}
	})

	t.Run("fails the given T when below the score is below the configured threshold", func(t *testing.T) {
		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0.99), 1.0)
			reporter.Summarize()

			assert.True(t, fakeT.FailedNow())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(1.0), 1.0)
			reporter.Summarize()

			assert.False(t, fakeT.FailedNow())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0.005), 0.01)
			reporter.Summarize()

			assert.True(t, fakeT.FailedNow())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(fakeT, fakescorecalculator.Always(0.5), 0.5)
			reporter.Summarize()

			assert.False(t, fakeT.FailedNow())
		}
	})
}
