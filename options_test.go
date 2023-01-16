package ooze_test

import (
	"regexp"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/fsrepository"
	"github.com/gtramontina/ooze/internal/viruses"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
	"github.com/gtramontina/ooze/internal/viruses/loopbreak"
	"github.com/stretchr/testify/assert"
)

//nolint:exhaustruct
func TestOptions(t *testing.T) {
	t.Run("can configure repository root", func(t *testing.T) {
		options := ooze.WithRepositoryRoot(".")(ooze.Options{})
		assert.Equal(t, fsrepository.New("."), options.Repository)
	})

	t.Run("can configure test command to run", func(t *testing.T) {
		{
			options := ooze.WithTestCommand("yes")(ooze.Options{})
			assert.Equal(t, cmdtestrunner.New("yes", []string{}...), options.TestRunner)
		}
		{
			options := ooze.WithTestCommand("echo some value")(ooze.Options{})
			assert.Equal(t, cmdtestrunner.New("echo", "some", "value"), options.TestRunner)
		}
	})

	t.Run("can configure minimum threshold", func(t *testing.T) {
		{
			options := ooze.WithMinimumThreshold(0.25)(ooze.Options{})
			assert.Equal(t, float32(0.25), options.MinimumThreshold)
		}

		{
			options := ooze.WithMinimumThreshold(0.75)(ooze.Options{})
			assert.Equal(t, float32(0.75), options.MinimumThreshold)
		}
	})

	t.Run("can configure parallel", func(t *testing.T) {
		options := ooze.Parallel()(ooze.Options{})
		assert.Equal(t, true, options.Parallel)
	})

	t.Run("can configure source files to ignore", func(t *testing.T) {
		{
			options := ooze.IgnoreSourceFiles(".*")(ooze.Options{})
			assert.Equal(t, regexp.MustCompile(".*"), options.IgnoreSourceFilesPattern)
		}

		{
			options := ooze.IgnoreSourceFiles(`.*\.go`)(ooze.Options{})
			assert.Equal(t, regexp.MustCompile(`.*\.go`), options.IgnoreSourceFilesPattern)
		}
	})

	t.Run("can configure viruses to infect source files", func(t *testing.T) {
		{
			options := ooze.WithViruses(loopbreak.New())(ooze.Options{})
			assert.Equal(t, []viruses.Virus{loopbreak.New()}, options.Viruses)
		}

		{
			options := ooze.WithViruses(loopbreak.New(), integerincrement.New())(ooze.Options{})
			assert.Equal(t, []viruses.Virus{loopbreak.New(), integerincrement.New()}, options.Viruses)
		}
	})
}
