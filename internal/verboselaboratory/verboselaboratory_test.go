package verboselaboratory_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelogger"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/verboselaboratory"
	"github.com/stretchr/testify/assert"
)

func TestVerboseLaboratory(t *testing.T) {
	t.Parallel()

	t.Run("logs when running tests", func(t *testing.T) {
		t.Parallel()

		logger := fakelogger.New()

		dummyRepository := fakerepository.New(fakerepository.FS{})
		dummyInfectedFile := goinfectedfile.New("some-path.go", nil, nil, nil)
		verboselaboratory.New(
			logger,
			fakelaboratory.NewAlways(result.Ok("dummy result")),
		).Test(dummyRepository, dummyInfectedFile)

		assert.Equal(t, []string{
			"running laboratory tests for 'some-path.go'",
			"laboratory diagnostic for 'some-path.go': Ok[string](dummy result)",
		}, logger.LoggedLines())
	})
}
