package verbosetestrunner_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/oozetesting/fakelogger"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestrunner"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/verbosetestrunner"
	"github.com/stretchr/testify/assert"
)

func TestVerboseTestRunner(t *testing.T) {
	t.Run("logs a successful (mutant killed) test", func(t *testing.T) {
		logger := fakelogger.New()

		verbosetestrunner.New(
			logger,
			faketestrunner.New(
				faketestrunner.NewResult("some-path", result.Ok("dummy")),
			),
		).Test(fakerepository.NewTemporaryAt("some-path"))

		assert.Equal(t, []string{
			"running tests on 'some-path'…",
			"tests for 'some-path' failed; mutant killed",
		}, logger.LoggedLines())
	})

	t.Run("logs a unsuccessful (mutant survived) test", func(t *testing.T) {
		logger := fakelogger.New()

		verbosetestrunner.New(
			logger,
			faketestrunner.New(
				faketestrunner.NewResult("some-path", result.Err[string]("dummy")),
			),
		).Test(fakerepository.NewTemporaryAt("some-path"))

		assert.Equal(t, []string{
			"running tests on 'some-path'…",
			"tests for 'some-path' passed; mutant survived",
		}, logger.LoggedLines())
	})
}
