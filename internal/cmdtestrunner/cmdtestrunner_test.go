package cmdtestrunner_test

import (
	"os"
	"path"
	"testing"

	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
)

func TestCMDTestRunner(t *testing.T) {
	t.Parallel()

	t.Run("has a positive result when subprocess exists unsuccessfully", func(t *testing.T) {
		t.Parallel()
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())

		diagnostic := cmdtestrunner.New("sh", "-c", "printf 'tests failed'; exit 1").Test(temporaryRepository)
		assert.Equal(t, result.Ok("tests failed"), diagnostic)
	})

	t.Run("has a negative result when subprocess exists successfully", func(t *testing.T) {
		t.Parallel()
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())

		diagnostic := cmdtestrunner.New("sh", "-c", "printf 'tests passed'; exit 0").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("tests passed"), diagnostic)
	})

	t.Run("runs within the given directory context", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		temporaryRepository := fakerepository.NewTemporaryAt(dir)

		diagnostic := cmdtestrunner.New("sh", "-c", "basename $(pwd)").Test(temporaryRepository)
		assert.Equal(t, result.Err[string](path.Base(dir)+"\n"), diagnostic)
	})

	t.Run("makes all environment variables available to the subprocess", func(t *testing.T) {
		t.Parallel()
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())

		assert.NoError(t, os.Setenv("TEST_VAR_1", "test_value_1"))
		diagnostic := cmdtestrunner.New("sh", "-c", "printf $TEST_VAR_1").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("test_value_1"), diagnostic)

		assert.NoError(t, os.Setenv("TEST_VAR_2", "test_value_2"))
		diagnostic = cmdtestrunner.New("sh", "-c", "printf $TEST_VAR_2").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("test_value_2"), diagnostic)
	})
}
