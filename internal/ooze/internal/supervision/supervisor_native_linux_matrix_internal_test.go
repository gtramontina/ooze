//go:build linux

package supervision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const linuxMatrixFixtureRole = "OOZE_LINUX_MATRIX_FIXTURE_ROLE"

func TestLinuxSubreaperVisibilityPerDescendantShapeAndRootState(t *testing.T) {
	if role := os.Getenv(linuxMatrixFixtureRole); role != "" {
		runLinuxMatrixFixture(t, role)

		return
	}

	for index, shape := range []struct {
		name string
		role string

		walkFromRoot [2]bool
		waitable     [2]bool
		postRootSeed string
	}{
		{
			name: "plain child", role: "plain-root",
			walkFromRoot: [2]bool{true, false}, waitable: [2]bool{false, true}, postRootSeed: "subject",
		},
		{
			name: "double-forked session orphan", role: "orphan-root",
			walkFromRoot: [2]bool{true, false}, waitable: [2]bool{false, true}, postRootSeed: "subject",
		},
		{
			name: "session escapee whose parent is the root", role: "escape-root",
			walkFromRoot: [2]bool{true, false}, waitable: [2]bool{false, true}, postRootSeed: "subject",
		},
		{
			name: "session escapee behind a live middle", role: "escape-middle-root",
			walkFromRoot: [2]bool{true, false}, waitable: [2]bool{false, false}, postRootSeed: "middle",
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			directory := t.TempDir()
			postRootChildren := make(chan []int, 1)
			executor := newLinuxMatrixExecutor(postRootChildren)
			supervisor := newLinuxMatrixSupervisor(t, executor, attemptIdentity(shape.role),
				campaignLineage(600+index))
			launched := supervisor.Launch(Spec{
				Attempt: shape.role,
				Command: []string{os.Args[0],
					"-test.run=^TestLinuxSubreaperVisibilityPerDescendantShapeAndRootState$"},
				Dir: directory,
				Env: linuxMatrixEnvironment(shape.role,
					"OOZE_LINUX_MATRIX_DIRECTORY="+directory),
				Profile: SerialProfile, Deadline: 10 * time.Second,
			})
			owned, ok := launched.(Owned)
			require.True(t, ok, "launch = %#v, want Owned", launched)
			require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)

			awaitLinuxMatrixFile(t, filepath.Join(directory, "ready"), 5*time.Second)
			root := readLinuxMatrixPID(t, filepath.Join(directory, "root.pid"))
			subject := readLinuxMatrixPID(t, filepath.Join(directory, "subject.pid"))
			middle := readOptionalLinuxMatrixPID(t, filepath.Join(directory, "middle.pid"))
			guardian := linuxMatrixGuardianPID(t, executor)

			aliveWalk := linuxMatrixDescendants(t, root)
			aliveWaitable := linuxMatrixDirectChildren(t, guardian)
			assertLinuxMatrixProductionCensus(t, "root alive", root, true, uint64(len(aliveWalk)))
			assertLinuxMatrixVisibility(t, "root alive", subject,
				shape.walkFromRoot[0], aliveWalk, shape.waitable[0], aliveWaitable)
			assertLinuxMatrixParentage(t, shape.role, guardian, root, middle, subject)
			if shape.role == "orphan-root" {
				{
					err := os.WriteFile(filepath.Join(directory, "adopt"), []byte("adopt"), 0o600)
					require.NoError(t, err)
				}
				awaitLinuxMatrixParent(t, subject, guardian, 5*time.Second)
			}

			{
				err := os.WriteFile(filepath.Join(directory, "release"), []byte("release"), 0o600)
				require.NoError(t, err)
			}
			var exitedWaitable []int
			select {
			case exitedWaitable = <-postRootChildren:
			case <-time.After(5 * time.Second):
				require.FailNow(t, "Linux matrix did not observe the post-root subreaper state")
			}
			assertLinuxMatrixProductionCensus(t, "root exited", root, false, 0)
			assertLinuxMatrixVisibility(t, "root exited", subject,
				shape.walkFromRoot[1], map[int]bool{}, shape.waitable[1], pidSet(exitedWaitable))
			seed := subject
			if shape.postRootSeed == "middle" {
				seed = middle
			}
			assert.False(t, seed <= 0, "post-root subreaper children = %v, want exact %s identity %d", exitedWaitable, shape.postRootSeed, seed)
			assert.True(t, pidSet(exitedWaitable)[seed], "post-root subreaper children = %v, want exact %s identity %d", exitedWaitable, shape.postRootSeed, seed)

			terminal := owned.Attempt.Wait()
			{
				settled, settledOK := terminal.(Settled)
				assert.True(t, settledOK, "terminal = %#v, want successful root after repeated subreaper sweeps", terminal)
				assert.EqualValues(t, 0, settled.Exit.Code, "terminal = %#v, want successful root after repeated subreaper sweeps", terminal)
			}
			assertLinuxMatrixProcessGone(t, subject)
			if middle > 0 {
				assertLinuxMatrixProcessGone(t, middle)
			}
		})
	}
}

func newLinuxMatrixExecutor(postRootChildren chan<- []int) *supervisorNativeExecutor {
	var once sync.Once
	return &supervisorNativeExecutor{
		drainEpoch: 5 * time.Second,
		attempts:   make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:    make(map[supervisorOutputRef]string), diagnostics: make(map[supervisorDiagnosticRef]error),
		createOutputFile: createNativeOutputFile,
		readOutputFile:   readNativeOutput,
		forceDomain: func(state nativePlatformState, root int, drainBy time.Time) error {
			children, err := linuxGuardianChildProcessIDs(state.guardian.command.Process.Pid)
			if err == nil {
				once.Do(func() { postRootChildren <- children })
			}
			if err != nil {
				return err
			}

			return forceNativeDomain(state, root, drainBy)
		},
	}
}

func newLinuxMatrixSupervisor(
	t *testing.T,
	executor *supervisorNativeExecutor,
	attempt attemptIdentity,
	lineage campaignLineage,
) *supervisor {
	t.Helper()
	shell := newProcessRuntimeShell(1)
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: lineage})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token, attempt: attempt, class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: time.Now,
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		prepare: executor.prepare, execute: executor.execute,
		recheckRoot: executor.recheckRoot, sampleRunning: executor.sampleRunning,
		readOutput: executor.readOutput, readDiagnostic: executor.readDiagnostic,
		recordDiagnostic: executor.recordDiagnostic,
	})

	return newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
}

func runLinuxMatrixFixture(t *testing.T, role string) {
	t.Helper()
	directory := os.Getenv("OOZE_LINUX_MATRIX_DIRECTORY")
	switch role {
	case "plain-root", "escape-root":
		writeLinuxMatrixPID(t, filepath.Join(directory, "root.pid"), os.Getpid())
		startLinuxMatrixChild(t, directory, "subject", role == "escape-root")
		writeLinuxMatrixReady(t, directory)
		awaitLinuxMatrixFile(t, filepath.Join(directory, "release"), 8*time.Second)
	case "orphan-root", "escape-middle-root":
		writeLinuxMatrixPID(t, filepath.Join(directory, "root.pid"), os.Getpid())
		middleRole := "lingering-middle"
		if role == "orphan-root" {
			middleRole = "exiting-middle"
		}
		command := exec.Command(os.Args[0],
			"-test.run=^TestLinuxSubreaperVisibilityPerDescendantShapeAndRootState$")
		command.Env = linuxMatrixEnvironment(middleRole,
			"OOZE_LINUX_MATRIX_DIRECTORY="+directory)
		{
			err := command.Start()
			require.NoError(t, err)
		}
		_ = command.Process.Release()
		awaitLinuxMatrixFile(t, filepath.Join(directory, "subject.pid"), 5*time.Second)
		writeLinuxMatrixReady(t, directory)
		awaitLinuxMatrixFile(t, filepath.Join(directory, "release"), 8*time.Second)
	case "exiting-middle", "lingering-middle":
		writeLinuxMatrixPID(t, filepath.Join(directory, "middle.pid"), os.Getpid())
		startLinuxMatrixChild(t, directory, "subject", true)
		if role == "exiting-middle" {
			awaitLinuxMatrixFile(t, filepath.Join(directory, "adopt"), 8*time.Second)
		} else {
			runBoundedLinuxMatrixSubject()
		}
	case "subject":
		if os.Getenv("OOZE_LINUX_MATRIX_SETSID") == "1" {
			{
				_, err := syscall.Setsid()
				require.NoError(t, err)
			}
		}
		writeLinuxMatrixPID(t, filepath.Join(directory, "subject.pid"), os.Getpid())
		runBoundedLinuxMatrixSubject()
	default:
		require.FailNowf(t, "unknown Linux matrix fixture role", "role: %q", role)
	}
}

func runBoundedLinuxMatrixSubject() {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

func startLinuxMatrixChild(t *testing.T, directory, role string, setsid bool) {
	t.Helper()
	command := exec.Command(os.Args[0],
		"-test.run=^TestLinuxSubreaperVisibilityPerDescendantShapeAndRootState$")
	additions := []string{"OOZE_LINUX_MATRIX_DIRECTORY=" + directory}
	if setsid {
		additions = append(additions, "OOZE_LINUX_MATRIX_SETSID=1")
	}
	command.Env = linuxMatrixEnvironment(role, additions...)
	{
		err := command.Start()
		require.NoError(t, err)
	}
	_ = command.Process.Release()
}

func linuxMatrixGuardianPID(t *testing.T, executor *supervisorNativeExecutor) int {
	t.Helper()
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	for _, attempt := range executor.attempts {
		return attempt.platform.guardian.command.Process.Pid
	}
	require.FailNow(t, "Linux matrix has no native attempt")

	return 0
}

func linuxMatrixDescendants(t *testing.T, root int) map[int]bool {
	t.Helper()
	found := make(map[int]bool)
	frontier := []int{root}
	for len(frontier) > 0 {
		parent := frontier[0]
		frontier = frontier[1:]
		children := linuxMatrixDirectChildren(t, parent)
		for child := range children {
			if !found[child] {
				found[child] = true
				frontier = append(frontier, child)
			}
		}
	}

	return found
}

func linuxMatrixDirectChildren(t *testing.T, parent int) map[int]bool {
	t.Helper()
	children, err := linuxDirectChildren(parent)
	require.NoError(t, err, "inspect direct children of %d: %v", parent, err)

	return pidSet(children)
}

func assertLinuxMatrixVisibility(
	t *testing.T,
	state string,
	subject int,
	wantWalk bool,
	walk map[int]bool,
	wantWaitable bool,
	waitable map[int]bool,
) {
	t.Helper()
	assert.Equal(t, wantWalk, walk[subject], "%s parent walk visibility for subject %d = %t, want %t (walk=%v)", state, subject, walk[subject], wantWalk, walk)
	assert.Equal(t, wantWaitable, waitable[subject], "%s subreaper wait4 visibility for subject %d = %t, want %t (children=%v)", state, subject, waitable[subject], wantWaitable, waitable)
}

func assertLinuxMatrixProductionCensus(
	t *testing.T,
	state string,
	root int,
	wantRootLive bool,
	wantDescendants uint64,
) {
	t.Helper()
	rootLive, descendants, err := linuxDescendantCount(root)
	require.NoError(t, err, "%s production parent walk: %v", state, err)
	assert.Equal(t, wantRootLive, rootLive, "%s production parent walk = root live %t, descendants %d; want %t, %d", state, rootLive, descendants, wantRootLive, wantDescendants)
	assert.Equal(t, wantDescendants, descendants, "%s production parent walk = root live %t, descendants %d; want %t, %d", state, rootLive, descendants, wantRootLive, wantDescendants)
}

func assertLinuxMatrixParentage(t *testing.T, role string, guardian, root, middle, subject int) {
	t.Helper()
	subjectParent := linuxMatrixParent(t, subject)
	switch role {
	case "plain-root", "escape-root":
		assert.Equal(t, root, subjectParent, "subject %d parent = %d, want live root %d", subject, subjectParent, root)
	case "orphan-root":
		assert.False(t, middle <= 0, "subject/middle/root parentage = %d/%d/%d, want future orphan %d behind live middle %d behind root %d", subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
		assert.Equal(t, middle, subjectParent, "subject/middle/root parentage = %d/%d/%d, want future orphan %d behind live middle %d behind root %d", subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
		assert.Equal(t, root, linuxMatrixParent(t, middle), "subject/middle/root parentage = %d/%d/%d, want future orphan %d behind live middle %d behind root %d", subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
	case "escape-middle-root":
		assert.False(t, middle <= 0, "subject/middle/root parentage = %d/%d/%d, want subject %d behind live middle %d behind root %d", subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
		assert.Equal(t, middle, subjectParent, "subject/middle/root parentage = %d/%d/%d, want subject %d behind live middle %d behind root %d", subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
		assert.Equal(t, root, linuxMatrixParent(t, middle), "subject/middle/root parentage = %d/%d/%d, want subject %d behind live middle %d behind root %d", subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
	}
}

func linuxMatrixParent(t *testing.T, process int) int {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process), "status"))
	require.NoError(t, err, "inspect Linux process %d identity: %v", process, err)
	for line := range strings.Lines(string(contents)) {
		value, found := strings.CutPrefix(line, "PPid:")
		if found {
			parent, parseErr := strconv.Atoi(strings.TrimSpace(value))
			require.NoError(t, parseErr)

			return parent
		}
	}
	require.FailNowf(t, "Linux process has no parent identity", "process: %d", process)

	return 0
}

func awaitLinuxMatrixParent(t *testing.T, process, want int, bound time.Duration) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if linuxMatrixParent(t, process) == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	require.FailNowf(t, "Linux process was not adopted within the bound", "process %d, expected parent %d within %s", process, want, bound)
}

func assertLinuxMatrixProcessGone(t *testing.T, process int) {
	t.Helper()
	{
		err := syscall.Kill(process, 0)
		assert.ErrorIs(t, err, syscall.ESRCH, "Linux matrix subject %d remains executable or unobservable after repeated sweeps: %v", process, err)
	}
}

func writeLinuxMatrixReady(t *testing.T, directory string) {
	t.Helper()
	{
		err := os.WriteFile(filepath.Join(directory, "ready"), []byte("ready"), 0o600)
		require.NoError(t, err)
	}
}

func writeLinuxMatrixPID(t *testing.T, path string, process int) {
	t.Helper()
	{
		err := os.WriteFile(path, []byte(strconv.Itoa(process)), 0o600)
		require.NoError(t, err)
	}
}

func readLinuxMatrixPID(t *testing.T, path string) int {
	t.Helper()
	contents := awaitLinuxMatrixFile(t, path, 5*time.Second)
	process, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	require.NoError(t, err, "Linux matrix identity %q = %q: %v", path, contents, err)
	assert.False(t, process <= 0, "Linux matrix identity %q = %q: %v", path, contents, err)

	return process
}

func readOptionalLinuxMatrixPID(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	process, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	require.NoError(t, err)

	return process
}

func awaitLinuxMatrixFile(t *testing.T, path string, bound time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil && len(contents) > 0 {
			return contents
		}
		require.False(t, err != nil && !os.IsNotExist(err), err)
		time.Sleep(time.Millisecond)
	}
	require.FailNowf(t, "Linux matrix file was not populated within the bound", "path %q, bound %s", path, bound)

	return nil
}

func linuxMatrixEnvironment(role string, additions ...string) []string {
	rolePrefix := linuxMatrixFixtureRole + "="
	environment := make([]string, 0, len(os.Environ())+len(additions)+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, rolePrefix) &&
			!strings.HasPrefix(variable, "OOZE_LINUX_MATRIX_") {
			environment = append(environment, variable)
		}
	}
	environment = append(environment, rolePrefix+role)

	return append(environment, additions...)
}

func pidSet(processes []int) map[int]bool {
	set := make(map[int]bool, len(processes))
	for _, process := range processes {
		set[process] = true
	}

	return set
}
