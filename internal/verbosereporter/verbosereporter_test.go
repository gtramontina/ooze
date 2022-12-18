package verbosereporter_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/oozetesting/fakelogger"
	"github.com/gtramontina/ooze/internal/oozetesting/fakereporter"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/verbosereporter"
	"github.com/stretchr/testify/assert"
)

func TestVerboseReporter(t *testing.T) {
	t.Parallel()

	t.Run("logs when adding a diagnostic", func(t *testing.T) {
		t.Parallel()

		logger := fakelogger.New()

		verbosereporter.New(
			logger,
			fakereporter.New(),
		).AddDiagnostic(result.Ok("dummy"))

		assert.Equal(t, []string{
			"registering diagnostic…",
		}, logger.LoggedLines())
	})

	t.Run("logs when summarizing", func(t *testing.T) {
		t.Parallel()

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
