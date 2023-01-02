package testingtreporter_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/iologger"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/oozetesting/fakescorecalculator"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestingt"
	"github.com/gtramontina/ooze/internal/oozetesting/stubdiffer"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/testingtreporter"
	"github.com/stretchr/testify/assert"
)

func TestTestingTReporterSummary(t *testing.T) {
	t.Run("summary is a test helper", func(t *testing.T) {
		fakeT := faketestingt.New()
		reporter := testingtreporter.New(
			fakeT,
			iologger.New(&bytes.Buffer{}),
			stubdiffer.New("+dummy diff"),
			fakescorecalculator.Always(0),
			0,
		)
		reporter.Summarize()

		assert.Equal(t, 1, fakeT.HelperCalls())
	})

	t.Run("reports summary when there are no diagnostics", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := iologger.New(buffer)
		reporter := testingtreporter.New(
			faketestingt.New(),
			logger,
			stubdiffer.New("+dummy diff"),
			fakescorecalculator.Always(0),
			0,
		)
		reporter.Summarize()

		assert.Equal(t, []string{
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
			"┃ • Total:        0                    ┃",
			"┃ • Killed:       0                    ┃",
			"┃ • Survived:     0                    ┃",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
			"┃ ✓ Score:     0.00 (minimum: 0.00)    ┃",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
			"",
		}, strings.Split(buffer.String(), "\n"))
	})

	t.Run("reports summary when there is one positive diagnostic", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := iologger.New(buffer)
		reporter := testingtreporter.New(
			faketestingt.New(),
			logger,
			stubdiffer.New("+dummy diff"),
			fakescorecalculator.Always(0),
			0,
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Ok("mutant killed")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.Summarize()

		assert.Equal(t, []string{
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
			"┃ • Total:        1                    ┃",
			"┃ • Killed:       1                    ┃",
			"┃ • Survived:     0                    ┃",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
			"┃ ✓ Score:     0.00 (minimum: 0.00)    ┃",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
			"",
		}, strings.Split(buffer.String(), "\n"))
	})

	t.Run("reports summary when there is one negative diagnostic", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := iologger.New(buffer)
		reporter := testingtreporter.New(
			faketestingt.New(),
			logger,
			stubdiffer.New("+dummy diff"),
			fakescorecalculator.Always(0),
			0,
		)
		reporter.AddDiagnostic(ooze.NewDiagnostic(
			oozetesting.AsChannel(result.Err[string]("mutant survived")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil)),
		)
		reporter.Summarize()

		assert.Equal(t, []string{
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┃ 🧟 Mutant survived: dummy.go → dummy",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄",
			"┃ +dummy diff",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
			"┃ • Total:        1                    ┃",
			"┃ • Killed:       0                    ┃",
			"┃ • Survived:     1                    ┃",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
			"┃ ✓ Score:     0.00 (minimum: 0.00)    ┃",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
			"",
		}, strings.Split(buffer.String(), "\n"))
	})

	t.Run("reports summary when there are multiple mixed diagnostics", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		logger := iologger.New(buffer)
		reporter := testingtreporter.New(
			faketestingt.New(),
			logger,
			stubdiffer.New("+dummy diff"),
			fakescorecalculator.Always(0),
			0,
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
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┃ 🧟 Mutant survived: dummy.go → dummy",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄",
			"┃ +dummy diff",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┃ 🧟 Mutant survived: dummy.go → dummy",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄",
			"┃ +dummy diff",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┃ 🧟 Mutant survived: dummy.go → dummy",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄",
			"┃ +dummy diff",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╍┅",
			"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
			"┃ • Total:        8                    ┃",
			"┃ • Killed:       5                    ┃",
			"┃ • Survived:     3                    ┃",
			"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
			"┃ ✓ Score:     0.00 (minimum: 0.00)    ┃",
			"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
			"",
		}, strings.Split(buffer.String(), "\n"))
	})

	t.Run("reports the score calculated by the given score calculator", func(t *testing.T) {
		{
			buffer := &bytes.Buffer{}
			logger := iologger.New(buffer)
			reporter := testingtreporter.New(
				faketestingt.New(),
				logger,
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.32),
				0,
			)
			reporter.Summarize()

			assert.Equal(t, []string{
				"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
				"┃ • Total:        0                    ┃",
				"┃ • Killed:       0                    ┃",
				"┃ • Survived:     0                    ┃",
				"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
				"┃ ✓ Score:     0.32 (minimum: 0.00)    ┃",
				"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
				"",
			}, strings.Split(buffer.String(), "\n"))
		}

		{
			buffer := &bytes.Buffer{}
			logger := iologger.New(buffer)
			reporter := testingtreporter.New(
				faketestingt.New(),
				logger,
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.99),
				0,
			)
			reporter.Summarize()

			assert.Equal(t, []string{
				"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
				"┃ • Total:        0                    ┃",
				"┃ • Killed:       0                    ┃",
				"┃ • Survived:     0                    ┃",
				"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
				"┃ ✓ Score:     0.99 (minimum: 0.00)    ┃",
				"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
				"",
			}, strings.Split(buffer.String(), "\n"))
		}
	})

	t.Run("prints a colorful summary", func(t *testing.T) {
		defer func(original bool) { color.NoColor = original }(color.NoColor)
		color.NoColor = false

		t.Run("successful", func(t *testing.T) {
			buffer := &bytes.Buffer{}
			logger := iologger.New(buffer)
			reporter := testingtreporter.New(
				faketestingt.New(),
				logger,
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.99),
				0,
			)
			reporter.Summarize()

			assert.Equal(t, []string{
				"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
				"┃ • \033[1mTotal\033[0m:        0                    ┃",
				"┃ • \033[1mKilled\033[0m:       0                    ┃",
				"┃ • \033[1mSurvived\033[0m:     0                    ┃",
				"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
				"┃ \033[1;32m✓ Score:     0.99 (minimum: 0.00)\033[0m    ┃",
				"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
				"",
			}, strings.Split(buffer.String(), "\n"))
		})

		t.Run("failure", func(t *testing.T) {
			buffer := &bytes.Buffer{}
			logger := iologger.New(buffer)
			reporter := testingtreporter.New(
				faketestingt.New(),
				logger,
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.99),
				1.0,
			)
			reporter.Summarize()

			assert.Equal(t, []string{
				"┏━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓",
				"┃ • \033[1mTotal\033[0m:        0                    ┃",
				"┃ • \033[1mKilled\033[0m:       0                    ┃",
				"┃ • \033[1mSurvived\033[0m:     0                    ┃",
				"┠┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┨",
				"┃ \033[1;31m⨯ Score:     0.99 (minimum: 1.00)\033[0m    ┃",
				"┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛",
				"",
			}, strings.Split(buffer.String(), "\n"))
		})
	})
}

func TestTestingTReporter(t *testing.T) {
	t.Run("fails the given T when below the score is below the configured", func(t *testing.T) {
		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(
				fakeT,
				iologger.New(&bytes.Buffer{}),
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.99),
				1.0,
			)
			reporter.Summarize()

			assert.True(t, fakeT.FailedNow())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(
				fakeT,
				iologger.New(&bytes.Buffer{}),
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(1.0),
				1.0,
			)
			reporter.Summarize()

			assert.False(t, fakeT.FailedNow())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(
				fakeT,
				iologger.New(&bytes.Buffer{}),
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.005),
				0.01,
			)
			reporter.Summarize()

			assert.True(t, fakeT.FailedNow())
		}

		{
			fakeT := faketestingt.New()
			reporter := testingtreporter.New(
				fakeT,
				iologger.New(&bytes.Buffer{}),
				stubdiffer.New("+dummy diff"),
				fakescorecalculator.Always(0.5),
				0.5,
			)
			reporter.Summarize()

			assert.False(t, fakeT.FailedNow())
		}
	})
}
