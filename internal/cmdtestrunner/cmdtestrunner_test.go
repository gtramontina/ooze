package cmdtestrunner_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gtramontina/ooze/internal/cmdtestrunner"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	helperProcessModeEnvironmentVariable      = "OOZE_CMDTESTRUNNER_TEST_HELPER_MODE"
	descendantClosesOutputEnvironmentVariable = "OOZE_CMDTESTRUNNER_TEST_DESCENDANT_CLOSES_OUTPUT"
	helperProcessTimeout                      = 30 * time.Second
	testProcessTimeout                        = 10 * time.Second
)

const (
	descendantReadyFile    = "descendant-ready"
	descendantObservedFile = "descendant-observed"
	descendantReleaseFile  = "descendant-release"
	descendantWriteFile    = "descendant-write"
)

type processExitObservation func()

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
	case "spawn-descendant":
		spawnDescendant()
	case "delayed-write":
		writeAfterParentExits()
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

	t.Run("panics when the test process cannot be started", func(t *testing.T) {
		temporaryRepository := fakerepository.NewTemporaryAt(t.TempDir())
		missingExecutable := filepath.Join(t.TempDir(), "missing-test-command")

		assert.Panics(t, func() {
			cmdtestrunner.New(missingExecutable).Test(temporaryRepository)
		})
	})

	t.Run("does not return while a descendant holds its output handles", func(t *testing.T) {
		assertDoesNotReturnWhileDescendantCanWrite(t, false)
	})

	t.Run("does not return while a descendant can write after closing its output handles", func(t *testing.T) {
		assertDoesNotReturnWhileDescendantCanWrite(t, true)
	})
}

func assertDoesNotReturnWhileDescendantCanWrite(t *testing.T, closesOutput bool) {
	t.Helper()
	if !descendantSupervisionSupported {
		t.Skip("process-tree supervision is unavailable on this operating system")
	}
	t.Setenv(descendantClosesOutputEnvironmentVariable, strconv.FormatBool(closesOutput))
	dir := t.TempDir()
	temporaryRepository := fakerepository.NewTemporaryAt(dir)
	runner := newHelperCommand(t, "spawn-descendant")

	outputChannel := make(chan result.Result[string], 1)
	go func() {
		outputChannel <- runner.Test(temporaryRepository)
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(dir, descendantObservedFile), nil, 0o600)
		_ = os.WriteFile(filepath.Join(dir, descendantReleaseFile), nil, 0o600)
	})

	var descendantProcessID int
	require.Eventually(t, func() bool {
		readyFileContents, err := os.ReadFile(filepath.Join(dir, descendantReadyFile))
		if err != nil {
			return false
		}

		descendantProcessID, err = strconv.Atoi(string(readyFileContents))

		return err == nil
	}, testProcessTimeout, 10*time.Millisecond)
	assertDescendantExited, err := observeProcessExit(t, descendantProcessID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, descendantObservedFile), nil, 0o600))

	var output result.Result[string]
	select {
	case output = <-outputChannel:
	case <-time.After(testProcessTimeout):
		require.NoError(t, os.WriteFile(filepath.Join(dir, descendantReleaseFile), nil, 0o600))
		select {
		case <-outputChannel:
		case <-time.After(testProcessTimeout):
		}
		require.FailNow(t, "Test remained blocked while supervising a descendant")
	}
	assert.Equal(t, result.Err[string](""), output)
	assertDescendantExited()
	assert.NoFileExists(t, filepath.Join(dir, descendantWriteFile))
}

func spawnDescendant() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}

	err = os.Setenv(helperProcessModeEnvironmentVariable, "delayed-write")
	if err != nil {
		os.Exit(2)
	}

	command := exec.Command(executable) //nolint:noctx // Test helper has its own bounded lifecycle.
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Start()
	if err != nil {
		os.Exit(2)
	}

	if !waitForFile(descendantReadyFile) {
		os.Exit(2)
	}
	if !waitForFile(descendantObservedFile) {
		os.Exit(2)
	}

	os.Exit(0)
}

func writeAfterParentExits() {
	err := os.WriteFile(descendantReadyFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	if err != nil {
		os.Exit(2)
	}
	if os.Getenv(descendantClosesOutputEnvironmentVariable) == "true" {
		if errors.Join(os.Stdout.Close(), os.Stderr.Close()) != nil {
			os.Exit(2)
		}
	}

	if !waitForFile(descendantReleaseFile) {
		os.Exit(2)
	}

	err = os.WriteFile(descendantWriteFile, []byte("written by descendant"), 0o600)
	if err != nil {
		os.Exit(2)
	}

	os.Exit(0)
}

func waitForFile(path string) bool {
	deadline := time.Now().Add(helperProcessTimeout)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newHelperCommand(t *testing.T, mode string) *cmdtestrunner.CMDTestRunner {
	t.Helper()

	executable, err := os.Executable()
	require.NoError(t, err)
	t.Setenv(helperProcessModeEnvironmentVariable, mode)

	return cmdtestrunner.New(executable)
}
