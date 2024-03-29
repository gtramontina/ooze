package verboselaboratory_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelogger"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/verboselaboratory"
	"github.com/stretchr/testify/assert"
)

func TestVerboseLaboratory(t *testing.T) {
	t.Run("logs when running tests", func(t *testing.T) {
		logger := fakelogger.New()

		dummyRepository := fakerepository.New(fakerepository.FS{})
		dummyMutatedFile := gomutatedfile.New("dummy-infection", "some-path.go", nil, nil)
		verboselaboratory.New(
			logger,
			fakelaboratory.NewAlways(result.Ok("dummy result")),
		).Test(dummyRepository, dummyMutatedFile)

		assert.Equal(t, []string{
			"running laboratory tests for 'some-path.go'",
			"laboratory result for 'some-path.go': Ok[string](dummy result)",
		}, logger.LoggedLines())
	})
}
