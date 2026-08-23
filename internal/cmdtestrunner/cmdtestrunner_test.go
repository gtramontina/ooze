package cmdtestrunner_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	descendantEscapeEnvironmentVariable       = "OOZE_CMDTESTRUNNER_TEST_DESCENDANT_ESCAPE"
	helperProcessTimeout                      = 30 * time.Second
	testProcessTimeout                        = 10 * time.Second
	fixtureCleanupTimeout                     = 5 * time.Second
)

const (
	descendantRootFile     = "descendant-root"
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
		assertDoesNotReturnWhileDescendantCanWrite(t, false, descendantEscapesNothing)
	})

	t.Run("does not return while a descendant can write after closing its output handles", func(t *testing.T) {
		assertDoesNotReturnWhileDescendantCanWrite(t, true, descendantEscapesNothing)
	})
}

// The containment handle a spawned descendant drops before trying to outlive
// supervision. Escaping is only meaningful for a descendant that also closes
// its output handles: one still holding them blocks the runner on its output
// pipe, which would hide whatever the supervisor's census does or does not
// observe.
const (
	descendantEscapesNothing      = ""
	descendantEscapesProcessGroup = "process-group"
)

// supervisedDescendant is a descendant of a running supervised command, already
// observed by the fixture and released to outlive its root.
type supervisedDescendant struct {
	directory string
	processID int
	awaitExit processExitObservation
	output    <-chan result.Result[string]
}

// superviseDescendant starts a supervised command that spawns one descendant
// which escapes as asked, waits until that descendant has announced itself and
// is being observed, then lets the supervised root exit. Every bound and every
// teardown step here belongs to the fixture: the supervisor under test is not
// asked to terminate anything, because the descendants these fixtures build are
// exactly the ones it may fail to reach.
func superviseDescendant(t *testing.T, closesOutput bool, escape string) supervisedDescendant {
	t.Helper()
	if !descendantSupervisionSupported {
		t.Skip("process-tree supervision is unavailable on this operating system")
	}
	if escape != descendantEscapesNothing && !descendantCanEscapeSupervision {
		t.Skip("a descendant cannot drop its supervisor's containment handle on this operating system")
	}
	t.Setenv(descendantClosesOutputEnvironmentVariable, strconv.FormatBool(closesOutput))
	t.Setenv(descendantEscapeEnvironmentVariable, escape)
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

	// Registered before the first assertion that can fail, and after the marker
	// cleanup above so that it runs first: releasing the descendant through a
	// marker file cannot be the fixture's guarantee, because t.TempDir removes
	// the directory holding it as soon as this test ends, which can happen
	// before the descendant's next poll observes it.
	var descendantProcessID int
	t.Cleanup(func() { terminateDescendant(t, descendantProcessID) })

	rootProcessID := awaitAnnouncedIdentity(t, filepath.Join(dir, descendantRootFile), 1)[0]
	announced := awaitAnnouncedIdentity(t, filepath.Join(dir, descendantReadyFile), 2)
	descendantProcessID = announced[0]
	requireEscapeHappened(t, rootProcessID, descendantProcessID, announced[1])

	awaitExit, err := observeProcessExit(t, descendantProcessID)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, descendantObservedFile), nil, 0o600))

	return supervisedDescendant{
		directory: dir,
		processID: descendantProcessID,
		awaitExit: awaitExit,
		output:    outputChannel,
	}
}

func assertDoesNotReturnWhileDescendantCanWrite(t *testing.T, closesOutput bool, escape string) {
	t.Helper()
	descendant := superviseDescendant(t, closesOutput, escape)

	var output result.Result[string]
	select {
	case output = <-descendant.output:
	case <-time.After(testProcessTimeout):
		require.NoError(t, os.WriteFile(filepath.Join(descendant.directory, descendantReleaseFile), nil, 0o600))
		select {
		case <-descendant.output:
		case <-time.After(testProcessTimeout):
		}
		require.FailNow(t, "Test remained blocked while supervising a descendant")
	}
	assert.Equal(t, result.Err[string](""), output)
	descendant.awaitExit()
	assert.NoFileExists(t, filepath.Join(descendant.directory, descendantWriteFile))
}

// awaitAnnouncedIdentity waits for a process to announce, from inside itself,
// the identity fields a fixture needs. All of them must be present, so a
// half-written file is treated as not yet written.
//
// It polls on the caller's goroutine rather than through require.Eventually,
// which evaluates its condition on another one. Fixture teardown reads the
// process ID after a failed assertion has unwound the calling goroutine, and
// Eventually's timeout path establishes no ordering against a condition still
// in flight, so the two could race on the value teardown depends on.
func awaitAnnouncedIdentity(t *testing.T, path string, fields int) []int {
	t.Helper()

	deadline := time.Now().Add(testProcessTimeout)
	for {
		values := readAnnouncedIdentity(path, fields)
		if values != nil {
			return values
		}
		if time.Now().After(deadline) {
			require.FailNow(t, "A supervised process did not announce its identity: "+filepath.Base(path))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func readAnnouncedIdentity(path string, fields int) []int {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	announced := strings.Fields(string(contents))
	if len(announced) != fields {
		return nil
	}

	values := make([]int, 0, fields)
	for _, field := range announced {
		value, convertErr := strconv.Atoi(field)
		if convertErr != nil || value <= 0 {
			return nil
		}
		values = append(values, value)
	}

	return values
}

// requireEscapeHappened checks that the descendant really is a child of the
// supervised root, rather than inferring its relationship from the outcome. A
// descendant that is reachable by a walk of parent identity from a live root is
// one a census can count, which is what separates a fixable census defect from a
// limit of the platform.
func requireEscapeHappened(t *testing.T, rootProcessID, descendantProcessID, announcedParent int) {
	t.Helper()

	// Ties the announcement to a real process: the kernel must agree about the
	// parent the announcing process claimed, so a fabricated process ID cannot
	// stand in for a descendant that was never created.
	observedParent, _, err := descendantIdentity(descendantProcessID)
	require.NoError(t, err)
	require.Equal(t, announcedParent, observedParent,
		"process %d announced parent %d, but the kernel reports %d",
		descendantProcessID, announcedParent, observedParent)
	require.Equal(t, rootProcessID, announcedParent,
		"descendant %d is not a child of the supervised root", descendantProcessID)
}

func spawnDescendant() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}

	// Published before anything is spawned, so a fixture can require that a
	// descendant does, or does not, name this process as its parent.
	err = os.WriteFile(descendantRootFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
	if err != nil {
		os.Exit(2)
	}

	escape := os.Getenv(descendantEscapeEnvironmentVariable)

	err = os.Setenv(helperProcessModeEnvironmentVariable, "delayed-write")
	if err != nil {
		os.Exit(2)
	}

	command := exec.Command(executable) //nolint:noctx // Test helper has its own bounded lifecycle.
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if escape == descendantEscapesProcessGroup {
		detachDescendantProcessGroup(command)
	}
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
	err := os.WriteFile(
		descendantReadyFile,
		[]byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(os.Getppid())),
		0o600,
	)
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
