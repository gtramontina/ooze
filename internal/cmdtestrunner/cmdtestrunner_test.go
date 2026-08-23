package cmdtestrunner_test

import (
	"errors"
	"fmt"
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

	// A wide spawner has to be bounded before it runs, not after: a trivial
	// spawner was recorded on this project at 1747 processes per second. So the
	// breadth is a fixed compile-time count, never derived at runtime; every
	// descendant self-exits on its own timer even if nothing cleans up after it;
	// and the fixture terminates each descendant it has verified.
	//
	// Sixteen is chosen to sit clear of the resolved 64-descendant ceiling for
	// automatic attempts, so that a legitimately contained fan-out is never near
	// the threshold a fuse would judge. No fuse exists in this package yet, so
	// that is the reason for the number rather than a protection this fixture
	// currently relies on.
	descendantFanoutBreadth = 16
	descendantLingerTimeout = 20 * time.Second
)

const (
	descendantRootFile      = "descendant-root"
	descendantReadyFile     = "descendant-ready"
	descendantFanoutFile    = "descendant-fanout"
	descendantAlivePrefix   = "descendant-alive-"
	descendantRetiredPrefix = "descendant-retired-"
	descendantObservedFile  = "descendant-observed"
	descendantReleaseFile   = "descendant-release"
	descendantWriteFile     = "descendant-write"
)

type processExitObservation func()

func TestMain(m *testing.M) {
	behaviour, isHelper := helperBehaviours()[os.Getenv(helperProcessModeEnvironmentVariable)]
	if !isHelper {
		os.Exit(m.Run())
	}

	behaviour()
	os.Exit(2) // Every behaviour exits on its own; arriving here is a defect.
}

// helperBehaviours are the roles this test binary can re-execute itself as.
// Fixtures drive a supervised command by re-running this same binary rather than
// compiling a helper, which keeps every fixture on one build. It also keeps them
// clear of the build-cache artifact that bites anything compiling Go at a fresh
// path: without -trimpath the absolute package directory enters the build action
// ID, so a new workspace recompiles every non-GOROOT package.
func helperBehaviours() map[string]func() {
	return map[string]func(){
		"fail":              reportFailingTests,
		"pass":              reportPassingTests,
		"working-directory": reportWorkingDirectoryMarker,
		"environment":       reportEnvironmentVariable,
		"spawn-descendant":  spawnDescendant,
		"relay-descendant":  relayDescendant,
		"lingering-relay":   lingerAsLiveGroupMember,
		"spawn-fanout":      spawnFanout,
		"linger":            lingerUntilContained,
		"delayed-write":     writeAfterParentExits,
	}
}

func reportFailingTests() {
	_, _ = os.Stdout.WriteString("tests failed")
	os.Exit(1)
}

func reportPassingTests() {
	_, _ = os.Stdout.WriteString("tests passed")
	os.Exit(0)
}

func reportWorkingDirectoryMarker() {
	_, err := os.Stat("working-directory-marker")
	if err != nil {
		os.Exit(2)
	}

	_, _ = os.Stdout.WriteString("marker found")
	os.Exit(0)
}

func reportEnvironmentVariable() {
	_, _ = os.Stdout.WriteString(os.Getenv("TEST_VAR"))
	os.Exit(0)
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
	descendantEscapesSession      = "session"
	descendantEscapesBehindParent = "process-group-behind-a-live-parent"
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
	requireEscapeHappened(t, escape, rootProcessID, descendantProcessID, announced[1])

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

// assertSupervisionLeavesDescendantRunning records a boundary of the platform
// contract, not a defect to be fixed. Where the operating system offers no
// primitive that reaches the descendant, the honest fixture is one that states
// where containment stops and fails if that ever silently changes.
func assertSupervisionLeavesDescendantRunning(t *testing.T, closesOutput bool, escape string) {
	t.Helper()
	descendant := superviseDescendant(t, closesOutput, escape)

	select {
	case output := <-descendant.output:
		assert.Equal(t, result.Err[string](""), output)
	case <-time.After(testProcessTimeout):
		require.FailNow(t, "Test did not return after its supervised root exited")
	}

	canExecute, err := descendantCanStillExecute(descendant.processID)
	require.NoError(t, err)
	assert.True(t, canExecute,
		"this platform is documented as unable to reach such a descendant; "+
			"if it now does, the platform contract has changed and must be updated deliberately")
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

// requireEscapeHappened checks that the descendant really performed the escape
// the fixture asked for, rather than inferring it from the outcome. Without it, a
// session escape that silently degraded into a mere process-group escape would
// still look correct -- and those two carry opposite dispositions, one a census
// defect to be fixed and the other a platform limit to be recorded.
func requireEscapeHappened(t *testing.T, escape string, rootProcessID, descendantProcessID, announcedParent int) {
	t.Helper()

	// Ties the announcement to a real process: the kernel must agree about the
	// parent the announcing process claimed, so a fabricated process ID cannot
	// stand in for a descendant that was never created.
	observedParent, observedSession, err := descendantIdentity(descendantProcessID)
	require.NoError(t, err)
	require.Equal(t, announcedParent, observedParent,
		"process %d announced parent %d, but the kernel reports %d",
		descendantProcessID, announcedParent, observedParent)

	if escape == descendantEscapesBehindParent {
		require.NotEqual(t, rootProcessID, announcedParent,
			"this escapee must sit behind an intermediate rather than be a child of the supervised root")
		require.NotEqual(t, 1, announcedParent,
			"this escapee's parent must still be alive inside the supervised group, not already reaped away")

		return
	}

	if escape != descendantEscapesSession {
		require.Equal(t, rootProcessID, announcedParent,
			"this descendant must remain a child of the supervised root: being reachable by a walk of parent "+
				"identity from a live root is what separates a fixable census defect from a platform limit")

		return
	}

	require.NotEqual(t, rootProcessID, announcedParent,
		"a session escapee must not be a child of the supervised root: leaving the parent walk as well as the "+
			"process group is what makes it a platform limit rather than a defect")

	_, fixtureSession, err := descendantIdentity(os.Getpid())
	require.NoError(t, err)
	require.NotEqual(t, fixtureSession, observedSession,
		"the descendant never left this process's session, so it performed no session escape; a "+
			"process-group escape alone is a different case, recorded as a defect elsewhere")
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

	command, err := descendantCommandFor(executable, os.Getenv(descendantEscapeEnvironmentVariable))
	if err != nil {
		os.Exit(2)
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

// relayDescendant performs the second of the two forks that detach a session
// escapee. It is already the leader of a new session, so the writer it spawns
// inherits that session, and relaying exits immediately so the writer is
// reparented away from the supervised tree.
// descendantCommandFor builds the command that carries out one escape: the role
// the next process plays, and the containment handle it drops on the way in.
//
// Which handle is dropped is the whole difference between the escapes. A process
// group leaves the census; a session leaves the census and every ancestry the
// supervisor could walk. Dropping nothing, while lingering inside the group, is
// how an escapee stays reachable right up to the moment the drain kills the
// thing that was making it reachable.
func descendantCommandFor(executable, escape string) (*exec.Cmd, error) {
	role := "delayed-write"
	switch escape {
	case descendantEscapesSession:
		// A session escape needs a second fork: the relay creates the new
		// session, spawns the writer into it, and exits, leaving that writer
		// with no ancestor the supervisor can walk back to.
		role = "relay-descendant"
	case descendantEscapesBehindParent:
		role = "lingering-relay"
	}

	err := os.Setenv(helperProcessModeEnvironmentVariable, role)
	if err != nil {
		return nil, fmt.Errorf("select descendant role %q: %w", role, err)
	}

	command := exec.Command(executable) //nolint:noctx // Test helper has its own bounded lifecycle.
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	switch escape {
	case descendantEscapesProcessGroup:
		detachDescendantProcessGroup(command)
	case descendantEscapesSession:
		detachDescendantSession(command)
	}

	return command, nil
}

func relayDescendant() {
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

	os.Exit(0)
}

// spawnFanout starts a fixed number of descendants at once, publishes their
// process IDs so the fixture can bound and verify them without asking the
// supervisor anything, and only then lets itself be released.
func spawnFanout() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}

	err = os.Setenv(helperProcessModeEnvironmentVariable, "linger")
	if err != nil {
		os.Exit(2)
	}

	// The published set opens with this process's own ID, so the fixture can
	// require that every descendant names it as their parent, rather than
	// trusting that whatever IDs appear here were really created here.
	processIDs := make([]string, 0, descendantFanoutBreadth+1)
	processIDs = append(processIDs, strconv.Itoa(os.Getpid()))
	for range descendantFanoutBreadth {
		command := exec.Command(executable) //nolint:noctx // Bounded by descendantLingerTimeout.
		command.Env = os.Environ()
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if command.Start() != nil {
			os.Exit(2)
		}

		processIDs = append(processIDs, strconv.Itoa(command.Process.Pid))
	}

	err = os.WriteFile(descendantFanoutFile, []byte(strings.Join(processIDs, "\n")), 0o600)
	if err != nil {
		os.Exit(2)
	}

	if !waitForFile(descendantObservedFile) {
		os.Exit(2)
	}

	os.Exit(0)
}

// lingerUntilContained holds a descendant alive long enough to be supervised and
// releases its output handles, so the runner's output pipe is not what keeps the
// execution domain open.
//
// It reports twice, and the fixture needs both. On entry it records that it
// reached its own code and names its parent: a process that was forked but died
// during runtime start-up never writes this, so the fixture cannot mistake a
// doomed process for a live descendant. If its own timer ever expires it records
// that too, before exiting, so a descendant that retired itself can never be
// read as one the supervisor contained.
func lingerUntilContained() {
	if errors.Join(os.Stdout.Close(), os.Stderr.Close()) != nil {
		os.Exit(2)
	}

	processID := os.Getpid()
	err := os.WriteFile(
		descendantAlivePrefix+strconv.Itoa(processID),
		[]byte(strconv.Itoa(os.Getppid())),
		0o600,
	)
	if err != nil {
		os.Exit(2)
	}

	time.Sleep(descendantLingerTimeout)

	_ = os.WriteFile(descendantRetiredPrefix+strconv.Itoa(processID), nil, 0o600)
	os.Exit(0)
}

// lingerAsLiveGroupMember spawns an escapee out of the supervised process group
// and then stays alive inside it. It is the live in-group ancestor through which
// that escapee can still be reached, and it is itself an ordinary member that a
// drain sweep will kill.
func lingerAsLiveGroupMember() {
	executable, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}

	err = os.Setenv(helperProcessModeEnvironmentVariable, "delayed-write")
	if err != nil {
		os.Exit(2)
	}

	command := exec.Command(executable) //nolint:noctx // Bounded by descendantLingerTimeout.
	command.Env = os.Environ()
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	detachDescendantProcessGroup(command)
	err = command.Start()
	if err != nil {
		os.Exit(2)
	}

	time.Sleep(descendantLingerTimeout)
	os.Exit(0)
}

func writeAfterParentExits() {
	err := os.WriteFile(
		descendantReadyFile,
		[]byte(strconv.Itoa(os.Getpid())+" "+strconv.Itoa(settledParentProcessID())),
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

// settledParentProcessID reports this process's parent once that answer has
// stopped changing.
//
// A relay exits immediately after spawning this process, so the parent reported
// here moves from the relay to whichever process adopts the orphan -- the
// guardian on Linux, process 1 on macOS. Announcing mid-transition would name a
// parent that no longer holds by the time the fixture reads the same fact from
// the kernel, and the fixture would fail on the mismatch rather than on
// anything it is testing.
func settledParentProcessID() int {
	parent := os.Getppid()
	for range 100 {
		time.Sleep(5 * time.Millisecond)
		settled := os.Getppid()
		if settled == parent {
			return parent
		}
		parent = settled
	}

	return parent
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
