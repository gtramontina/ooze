package verbosereporter_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/future"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelogger"
	"github.com/gtramontina/ooze/internal/oozetesting/fakereporter"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/verbosereporter"
	"github.com/stretchr/testify/assert"
)

func TestVerboseReporter(t *testing.T) {
	t.Run("logs when adding a diagnostic", func(t *testing.T) {
		logger := fakelogger.New()

		verbosereporter.New(
			logger,
			fakereporter.New(),
		).AddDiagnostic(ooze.NewDiagnostic(
			future.Resolved(result.Ok("dummy")),
			gomutatedfile.New("dummy", "dummy.go", nil, nil),
		))

		assert.Equal(t, []string{
			"registering diagnostic…",
		}, logger.LoggedLines())
	})

	t.Run("logs when summarizing", func(t *testing.T) {
		logger := fakelogger.New()

		verbosereporter.New(
			logger,
			fakereporter.New(),
		).Summarize()

		assert.Equal(t, []string{
			"summarizing report…",
		}, logger.LoggedLines())
	})
}
