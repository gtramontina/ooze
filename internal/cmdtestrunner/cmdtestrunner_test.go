package cmdtestrunner_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const helperProcessModeEnvironmentVariable = "OOZE_CMDTESTRUNNER_TEST_HELPER_MODE"

func TestMain(m *testing.M) {
	switch os.Getenv(helperProcessModeEnvironmentVariable) {
	case "fail":
		_, _ = os.Stdout.WriteString("tests failed")
		os.Exit(1)
	case "pass":
		_, _ = os.Stdout.WriteString("tests passed")
		os.Exit(0)
	case "working-directory":
		_, err := os.Stat("working-directory-marker")
		if err != nil {
			os.Exit(2)
		}

		_, _ = os.Stdout.WriteString("marker found")
		os.Exit(0)
	case "environment":
		_, _ = os.Stdout.WriteString(os.Getenv("TEST_VAR"))
		os.Exit(0)
	default:
		os.Exit(m.Run())
	}
}

func TestCMDTestRunner(t *testing.T) {
	t.Run("has a positive result when subprocess exists unsuccessfully", func(t *testing.T) {
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())

		output := newHelperCommand(t, "fail").Test(temporaryRepository)
		assert.Equal(t, result.Ok("tests failed"), output)
	})

	t.Run("has a negative result when subprocess exists successfully", func(t *testing.T) {
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())

		output := newHelperCommand(t, "pass").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("tests passed"), output)
	})

	t.Run("runs within the given directory context", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "working-directory-marker"), nil, 0o600))
		temporaryRepository := fakerepository.NewTemporaryAt(dir)

		output := newHelperCommand(t, "working-directory").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("marker found"), output)
	})

	t.Run("makes all environment variables available to the subprocess", func(t *testing.T) {
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())

		t.Setenv("TEST_VAR", "test_value_1")
		output := newHelperCommand(t, "environment").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("test_value_1"), output)

		t.Setenv("TEST_VAR", "test_value_2")
		output = newHelperCommand(t, "environment").Test(temporaryRepository)
		assert.Equal(t, result.Err[string]("test_value_2"), output)
	})
}

func newHelperCommand(t *testing.T, mode string) *cmdtestrunner.CMDTestRunner {
	t.Helper()

	executable, err := os.Executable()
	require.NoError(t, err)
	t.Setenv(helperProcessModeEnvironmentVariable, mode)

	return cmdtestrunner.New(executable)
}
