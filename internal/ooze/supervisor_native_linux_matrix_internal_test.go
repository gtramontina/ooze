//go:build linux

package ooze

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
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

		// Exact subject visibility: [root alive, root exited]. The subreaper
		// column is deliberately depth one; repeated sweeps reveal deeper
		// subjects after their adopted ancestor is reaped.
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
			walkFromRoot: [2]bool{false, false}, waitable: [2]bool{true, true}, postRootSeed: "subject",
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
			if !ok || owned.Attempt == nil {
				t.Fatalf("launch = %#v, want Owned", launched)
			}

			awaitLinuxMatrixFile(t, filepath.Join(directory, "ready"), 5*time.Second)
			root := readLinuxMatrixPID(t, filepath.Join(directory, "root.pid"))
			subject := readLinuxMatrixPID(t, filepath.Join(directory, "subject.pid"))
			middle := readOptionalLinuxMatrixPID(t, filepath.Join(directory, "middle.pid"))
			guardian := linuxMatrixGuardianPID(t, executor)

			aliveWalk := linuxMatrixDescendants(t, root)
			aliveWaitable := linuxMatrixDirectChildren(t, guardian)
			assertLinuxMatrixVisibility(t, "root alive", subject,
				shape.walkFromRoot[0], aliveWalk, shape.waitable[0], aliveWaitable)
			assertLinuxMatrixParentage(t, shape.role, guardian, root, middle, subject)

			if err := os.WriteFile(filepath.Join(directory, "release"), []byte("release"), 0o600); err != nil {
				t.Fatal(err)
			}
			var exitedWaitable []int
			select {
			case exitedWaitable = <-postRootChildren:
			case <-time.After(5 * time.Second):
				t.Fatal("Linux matrix did not observe the post-root subreaper state")
			}
			assertLinuxMatrixVisibility(t, "root exited", subject,
				shape.walkFromRoot[1], map[int]bool{}, shape.waitable[1], pidSet(exitedWaitable))
			seed := subject
			if shape.postRootSeed == "middle" {
				seed = middle
			}
			if seed <= 0 || !pidSet(exitedWaitable)[seed] {
				t.Fatalf("post-root subreaper children = %v, want exact %s identity %d",
					exitedWaitable, shape.postRootSeed, seed)
			}

			terminal := owned.Attempt.Wait()
			if settled, settledOK := terminal.(Settled); !settledOK || settled.Exit.Code != 0 {
				t.Fatalf("terminal = %#v, want successful root after repeated subreaper sweeps", terminal)
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
) *Supervisor {
	t.Helper()
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: lineage})
	requested := shell.requestAdmission(admissionRequest{
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
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
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
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		_ = command.Process.Release()
		awaitLinuxMatrixFile(t, filepath.Join(directory, "subject.pid"), 5*time.Second)
		writeLinuxMatrixReady(t, directory)
		awaitLinuxMatrixFile(t, filepath.Join(directory, "release"), 8*time.Second)
	case "exiting-middle", "lingering-middle":
		writeLinuxMatrixPID(t, filepath.Join(directory, "middle.pid"), os.Getpid())
		startLinuxMatrixChild(t, directory, "subject", true)
		if role == "lingering-middle" {
			runBoundedLinuxMatrixSubject()
		}
	case "subject":
		if os.Getenv("OOZE_LINUX_MATRIX_SETSID") == "1" {
			if _, err := syscall.Setsid(); err != nil {
				t.Fatal(err)
			}
		}
		writeLinuxMatrixPID(t, filepath.Join(directory, "subject.pid"), os.Getpid())
		runBoundedLinuxMatrixSubject()
	default:
		t.Fatalf("unknown Linux matrix fixture role %q", role)
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
	if err := command.Start(); err != nil {
		t.Fatal(err)
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
	t.Fatal("Linux matrix has no native attempt")

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
	if err != nil {
		t.Fatalf("inspect direct children of %d: %v", parent, err)
	}

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
	if walk[subject] != wantWalk {
		t.Fatalf("%s parent walk visibility for subject %d = %t, want %t (walk=%v)",
			state, subject, walk[subject], wantWalk, walk)
	}
	if waitable[subject] != wantWaitable {
		t.Fatalf("%s subreaper wait4 visibility for subject %d = %t, want %t (children=%v)",
			state, subject, waitable[subject], wantWaitable, waitable)
	}
}

func assertLinuxMatrixParentage(t *testing.T, role string, guardian, root, middle, subject int) {
	t.Helper()
	subjectParent := linuxMatrixParent(t, subject)
	switch role {
	case "plain-root", "escape-root":
		if subjectParent != root {
			t.Fatalf("subject %d parent = %d, want live root %d", subject, subjectParent, root)
		}
	case "orphan-root":
		if subjectParent != guardian {
			t.Fatalf("orphan %d parent = %d, want subreaper %d", subject, subjectParent, guardian)
		}
	case "escape-middle-root":
		if middle <= 0 || subjectParent != middle || linuxMatrixParent(t, middle) != root {
			t.Fatalf("subject/middle/root parentage = %d/%d/%d, want subject %d behind live middle %d behind root %d",
				subjectParent, linuxMatrixParent(t, middle), linuxMatrixParent(t, root), subject, middle, root)
		}
	}
}

func linuxMatrixParent(t *testing.T, process int) int {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process), "status"))
	if err != nil {
		t.Fatalf("inspect Linux process %d identity: %v", process, err)
	}
	for line := range strings.Lines(string(contents)) {
		value, found := strings.CutPrefix(line, "PPid:")
		if found {
			parent, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr != nil {
				t.Fatal(parseErr)
			}

			return parent
		}
	}
	t.Fatalf("Linux process %d has no parent identity", process)

	return 0
}

func assertLinuxMatrixProcessGone(t *testing.T, process int) {
	t.Helper()
	if err := syscall.Kill(process, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("Linux matrix subject %d remains executable or unobservable after repeated sweeps: %v", process, err)
	}
}

func writeLinuxMatrixReady(t *testing.T, directory string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "ready"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeLinuxMatrixPID(t *testing.T, path string, process int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strconv.Itoa(process)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readLinuxMatrixPID(t *testing.T, path string) int {
	t.Helper()
	contents := awaitLinuxMatrixFile(t, path, 5*time.Second)
	process, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || process <= 0 {
		t.Fatalf("Linux matrix identity %q = %q: %v", path, contents, err)
	}

	return process
}

func readOptionalLinuxMatrixPID(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	process, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil {
		t.Fatal(err)
	}

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
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Linux matrix file %q was not populated within %s", path, bound)

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
