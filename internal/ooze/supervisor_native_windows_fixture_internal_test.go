//go:build windows

package ooze

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const windowsJobFixtureRole = "OOZE_WINDOWS_JOB_FIXTURE_ROLE"

func TestWindowsJobVisibilityPerDescendantShapeAndRootState(t *testing.T) {
	role := os.Getenv(windowsJobFixtureRole)
	if role != "" {
		runWindowsJobFixture(t, role)

		return
	}

	for index, shape := range []struct {
		name string
		role string
	}{
		{name: "direct child", role: "matrix-direct-root"},
		{name: "deep descendant", role: "matrix-deep-root"},
		{name: "descendant in a nested job", role: "matrix-nested-root"},
	} {
		t.Run(shape.name, func(t *testing.T) {
			directory := t.TempDir()
			readyPath := filepath.Join(directory, "matrix.ready")
			releasePath := filepath.Join(directory, "matrix.release")
			postRootMembers := make(chan []uint32, 1)
			executor := newWindowsMatrixExecutor(postRootMembers)
			supervisor := newWindowsMatrixSupervisor(t, executor,
				attemptIdentity(shape.role), campaignLineage(700+index))
			launched := supervisor.Launch(Spec{
				Attempt: shape.role,
				Command: []string{os.Args[0],
					"-test.run=^TestWindowsJobVisibilityPerDescendantShapeAndRootState$"},
				Dir: directory,
				Env: windowsJobFixtureEnvironment(shape.role,
					"OOZE_WINDOWS_MATRIX_READY="+readyPath,
					"OOZE_WINDOWS_MATRIX_RELEASE="+releasePath,
				),
				Profile: SerialProfile, Deadline: 10 * time.Second,
			})
			owned, ok := launched.(Owned)
			if !ok || owned.Attempt == nil {
				t.Fatalf("launch = %#v, want Owned", launched)
			}
			stopWindowsFixtureOnFailure(t, owned.Attempt)

			identities := strings.Fields(string(awaitWindowsFixtureFile(t, readyPath, 3*time.Second)))
			if len(identities) < 2 {
				t.Fatalf("Windows matrix identities = %q, want root and exact subject", identities)
			}
			root, rootErr := strconv.Atoi(identities[0])
			subject, subjectErr := strconv.Atoi(identities[len(identities)-1])
			if rootErr != nil || subjectErr != nil || root <= 0 || subject <= 0 || root == subject {
				t.Fatalf("Windows matrix root/subject = %q/%q: %v/%v",
					identities[0], identities[len(identities)-1], rootErr, subjectErr)
			}
			subjectProcess := retainWindowsFixtureProcess(t, subject)
			aliveMembers := windowsMatrixMembers(t, executor)
			assertWindowsMatrixMember(t, "root alive", uint32(root), aliveMembers)
			assertWindowsMatrixMember(t, "root alive", uint32(subject), aliveMembers)

			if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
				t.Fatal(err)
			}
			var exitedMembers []uint32
			select {
			case exitedMembers = <-postRootMembers:
			case <-time.After(5 * time.Second):
				t.Fatal("Windows matrix did not observe parent-Job membership after root exit")
			}
			// A terminated root may remain in Job accounting while a retained
			// process handle exists. The supervisor's accepted root-exit fact,
			// rather than disappearance from the accounting list, defines this
			// state; the descendant must remain transitively visible either way.
			assertWindowsMatrixMember(t, "root exited", uint32(subject), exitedMembers)

			terminal := waitWindowsFixtureTerminal(t, owned.Attempt)
			settled, settledOK := terminal.(Settled)
			if !settledOK || settled.Exit.Code != 0 {
				t.Fatalf("terminal = %#v, want successful root after transitive Job drainage", terminal)
			}
			assertWindowsFixtureProcessStopped(t, subjectProcess, subject)
		})
	}
}

func TestWindowsNativeSupervisorRejectsBreakawayFromJob(t *testing.T) {
	role := os.Getenv(windowsJobFixtureRole)
	if role != "" {
		runWindowsJobFixture(t, role)

		return
	}

	directory := t.TempDir()
	statusPath := filepath.Join(directory, "breakaway.status")
	_, supervisor := newWindowsNativeSupervisorForFixture(t, "windows-breakaway", 503)
	launched := supervisor.Launch(Spec{
		Attempt: "windows-breakaway",
		Command: []string{os.Args[0], "-test.run=^TestWindowsNativeSupervisorRejectsBreakawayFromJob$"},
		Dir:     directory,
		Env: windowsJobFixtureEnvironment("breakaway-root",
			"OOZE_WINDOWS_BREAKAWAY_STATUS="+statusPath,
		),
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	stopWindowsFixtureOnFailure(t, owned.Attempt)
	terminal := waitWindowsFixtureTerminal(t, owned.Attempt)
	settled, ok := terminal.(Settled)
	if !ok || settled.Exit.Code != 0 {
		t.Fatalf("terminal = %#v, want successful breakaway-contract fixture", terminal)
	}
	fields := strings.Fields(string(awaitWindowsFixtureFile(t, statusPath, 2*time.Second)))
	if len(fields) != 2 || fields[1] != strconv.Itoa(int(windows.ERROR_ACCESS_DENIED)) {
		t.Fatalf("breakaway subject evidence = %q, want positive root PID and ERROR_ACCESS_DENIED", fields)
	}
	root, err := strconv.Atoi(fields[0])
	if err != nil || root <= 0 {
		t.Fatalf("breakaway subject root identity = %q: %v", fields[0], err)
	}
}

func TestWindowsNativeSupervisorDrainsChildInNestedJob(t *testing.T) {
	role := os.Getenv(windowsJobFixtureRole)
	if role != "" {
		runWindowsJobFixture(t, role)

		return
	}

	directory := t.TempDir()
	readyPath := filepath.Join(directory, "nested.ready")
	releasePath := filepath.Join(directory, "nested.release")
	markerPath := filepath.Join(directory, "nested.marker")
	_, supervisor := newWindowsNativeSupervisorForFixture(t, "windows-nested-job", 504)
	launched := supervisor.Launch(Spec{
		Attempt: "windows-nested-job",
		Command: []string{os.Args[0], "-test.run=^TestWindowsNativeSupervisorDrainsChildInNestedJob$"},
		Dir:     directory,
		Env: windowsJobFixtureEnvironment("nested-root",
			"OOZE_WINDOWS_NESTED_READY="+readyPath,
			"OOZE_WINDOWS_NESTED_RELEASE="+releasePath,
			"OOZE_WINDOWS_NESTED_MARKER="+markerPath,
		),
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	stopWindowsFixtureOnFailure(t, owned.Attempt)

	identities := strings.Fields(string(awaitWindowsFixtureFile(t, readyPath, 3*time.Second)))
	if len(identities) != 2 {
		t.Fatalf("nested-job subject evidence = %q, want root and child identities", identities)
	}
	root, rootErr := strconv.Atoi(identities[0])
	child, childErr := strconv.Atoi(identities[1])
	if rootErr != nil || childErr != nil || root <= 0 || child <= 0 || root == child {
		t.Fatalf("nested-job identities root=%q child=%q errors=(%v, %v)",
			identities[0], identities[1], rootErr, childErr)
	}
	childProcess := retainWindowsFixtureProcess(t, child)

	awaitWindowsFixtureFile(t, markerPath, 2*time.Second)
	if err := os.WriteFile(releasePath, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	terminal := waitWindowsFixtureTerminal(t, owned.Attempt)
	settled, ok := terminal.(Settled)
	if !ok || settled.Exit.Code != 0 {
		t.Fatalf("terminal = %#v, want successful nested-job root", terminal)
	}
	assertWindowsFixtureProcessStopped(t, childProcess, child)
}

func runWindowsJobFixture(t *testing.T, role string) {
	t.Helper()
	switch role {
	case "breakaway-root":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsNativeSupervisorRejectsBreakawayFromJob$")
		command.Env = windowsJobFixtureEnvironment("breakaway-child")
		command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB}
		err := command.Run()
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("breakaway creation = %v, want ERROR_ACCESS_DENIED", err)
		}
		status := fmt.Sprintf("%d %d", os.Getpid(), windows.ERROR_ACCESS_DENIED)
		if err = os.WriteFile(os.Getenv("OOZE_WINDOWS_BREAKAWAY_STATUS"), []byte(status), 0o600); err != nil {
			t.Fatal(err)
		}
	case "breakaway-child":
		return
	case "nested-root":
		runWindowsNestedJobRoot(t)
	case "nested-child":
		runWindowsNestedJobChild(t)
	case "matrix-direct-root", "matrix-deep-root", "matrix-nested-root":
		runWindowsMatrixRoot(t, role)
	case "matrix-middle":
		runWindowsMatrixMiddle(t)
	case "matrix-subject":
		awaitWindowsFixtureFile(t, os.Getenv("OOZE_WINDOWS_MATRIX_SUBJECT_RELEASE"), 8*time.Second)
	default:
		t.Fatalf("unknown Windows job fixture role %q", role)
	}
}

func runWindowsMatrixRoot(t *testing.T, role string) {
	t.Helper()
	root := os.Getpid()
	var subject int
	var middle int
	switch role {
	case "matrix-direct-root":
		subject = startWindowsMatrixProcess(t, "matrix-subject")
	case "matrix-deep-root":
		middle = startWindowsMatrixProcess(t, "matrix-middle")
		subject = awaitWindowsMatrixSubjectIdentity(t)
	case "matrix-nested-root":
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if closeErr := windows.CloseHandle(job); closeErr != nil {
				t.Errorf("close Windows matrix nested job: %v", closeErr)
			}
		}()
		subject = startWindowsMatrixProcessInJob(t, job, "matrix-subject")
	default:
		t.Fatalf("unknown Windows matrix root role %q", role)
	}
	identity := fmt.Sprintf("%d %d", root, subject)
	if middle > 0 {
		identity = fmt.Sprintf("%d %d %d", root, middle, subject)
	}
	if err := os.WriteFile(os.Getenv("OOZE_WINDOWS_MATRIX_READY"), []byte(identity), 0o600); err != nil {
		t.Fatal(err)
	}
	awaitWindowsFixtureFile(t, os.Getenv("OOZE_WINDOWS_MATRIX_RELEASE"), 8*time.Second)
}

func runWindowsMatrixMiddle(t *testing.T) {
	t.Helper()
	subject := startWindowsMatrixProcess(t, "matrix-subject")
	if err := os.WriteFile(os.Getenv("OOZE_WINDOWS_MATRIX_SUBJECT_PID"),
		[]byte(strconv.Itoa(subject)), 0o600); err != nil {
		t.Fatal(err)
	}
	awaitWindowsFixtureFile(t, os.Getenv("OOZE_WINDOWS_MATRIX_SUBJECT_RELEASE"), 8*time.Second)
}

func startWindowsMatrixProcess(t *testing.T, role string) int {
	t.Helper()
	command := exec.Command(os.Args[0],
		"-test.run=^TestWindowsJobVisibilityPerDescendantShapeAndRootState$")
	command.Env = windowsJobFixtureEnvironment(role,
		"OOZE_WINDOWS_MATRIX_READY="+os.Getenv("OOZE_WINDOWS_MATRIX_READY"),
		"OOZE_WINDOWS_MATRIX_RELEASE="+os.Getenv("OOZE_WINDOWS_MATRIX_RELEASE"),
		"OOZE_WINDOWS_MATRIX_SUBJECT_PID="+windowsMatrixSiblingPath("matrix.subject.pid"),
		"OOZE_WINDOWS_MATRIX_SUBJECT_RELEASE="+windowsMatrixSiblingPath("matrix.subject.release"),
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	process := command.Process.Pid
	releaseWindowsFixtureProcess(t, command.Process)

	return process
}

func startWindowsMatrixProcessInJob(t *testing.T, job windows.Handle, role string) int {
	t.Helper()
	command := exec.Command(os.Args[0],
		"-test.run=^TestWindowsJobVisibilityPerDescendantShapeAndRootState$")
	command.Env = windowsJobFixtureEnvironment(role,
		"OOZE_WINDOWS_MATRIX_SUBJECT_RELEASE="+windowsMatrixSiblingPath("matrix.subject.release"))
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	releaseWindowsFixtureProcess(t, command.Process)
	processID := uint32(command.Process.Pid) //nolint:gosec // Windows process IDs are 32-bit values.
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false, processID,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		t.Fatal(err)
	}
	released, err := resumeNativeProcess(processID)
	if err != nil || !released {
		t.Fatalf("release Windows matrix nested subject = (%t, %v)", released, err)
	}

	return int(processID)
}

func awaitWindowsMatrixSubjectIdentity(t *testing.T) int {
	t.Helper()
	path := windowsMatrixSiblingPath("matrix.subject.pid")
	contents := awaitWindowsFixtureFile(t, path, 3*time.Second)
	process, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || process <= 0 {
		t.Fatalf("Windows matrix subject identity = %q: %v", contents, err)
	}

	return process
}

func windowsMatrixSiblingPath(name string) string {
	return filepath.Join(filepath.Dir(os.Getenv("OOZE_WINDOWS_MATRIX_READY")), name)
}

func newWindowsMatrixExecutor(postRootMembers chan<- []uint32) *supervisorNativeExecutor {
	var once sync.Once
	return &supervisorNativeExecutor{
		drainEpoch: 5 * time.Second,
		attempts:   make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:    make(map[supervisorOutputRef]string), diagnostics: make(map[supervisorDiagnosticRef]error),
		createOutputFile: createNativeOutputFile,
		readOutputFile:   readNativeOutput,
		forceDomain: func(state nativePlatformState, root int, drainBy time.Time) error {
			members, err := nativeJobProcessIDs(state.job)
			if err == nil {
				once.Do(func() { postRootMembers <- members })
			}
			if err != nil {
				return err
			}

			return forceNativeDomain(state, root, drainBy)
		},
	}
}

func newWindowsMatrixSupervisor(
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

func windowsMatrixMembers(t *testing.T, executor *supervisorNativeExecutor) []uint32 {
	t.Helper()
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	for _, attempt := range executor.attempts {
		members, err := nativeJobProcessIDs(attempt.platform.job)
		if err != nil {
			t.Fatalf("inspect Windows parent Job members: %v", err)
		}

		return members
	}
	t.Fatal("Windows matrix has no native attempt")

	return nil
}

func assertWindowsMatrixMember(t *testing.T, state string, process uint32, members []uint32) {
	t.Helper()
	if !containsWindowsProcess(members, process) {
		t.Fatalf("%s parent-Job members = %v, want exact process %d", state, members, process)
	}
}

func containsWindowsProcess(processes []uint32, wanted uint32) bool {
	for _, process := range processes {
		if process == wanted {
			return true
		}
	}

	return false
}

func runWindowsNestedJobRoot(t *testing.T) {
	t.Helper()
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := windows.CloseHandle(job); closeErr != nil {
			t.Errorf("close nested fixture job: %v", closeErr)
		}
	}()
	command := exec.Command(os.Args[0], "-test.run=^TestWindowsNativeSupervisorDrainsChildInNestedJob$")
	command.Env = windowsJobFixtureEnvironment("nested-child",
		"OOZE_WINDOWS_NESTED_MARKER="+os.Getenv("OOZE_WINDOWS_NESTED_MARKER"),
	)
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	releaseWindowsFixtureProcess(t, command.Process)
	child := uint32(command.Process.Pid) //nolint:gosec // Windows process IDs are 32-bit values.
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		child,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := windows.CloseHandle(process); closeErr != nil {
			t.Errorf("close nested fixture process handle: %v", closeErr)
		}
	}()
	if err = windows.AssignProcessToJobObject(job, process); err != nil {
		t.Fatal(err)
	}
	members, err := nativeJobProcessIDs(job)
	if err != nil || len(members) != 1 || members[0] != child {
		t.Fatalf("nested job members = %v, error=%v, want exact child %d", members, err, child)
	}
	evidence := fmt.Sprintf("%d %d", os.Getpid(), child)
	if err = os.WriteFile(os.Getenv("OOZE_WINDOWS_NESTED_READY"), []byte(evidence), 0o600); err != nil {
		t.Fatal(err)
	}
	awaitWindowsFixtureFile(t, os.Getenv("OOZE_WINDOWS_NESTED_RELEASE"), 5*time.Second)
}

func runWindowsNestedJobChild(t *testing.T) {
	t.Helper()
	markerPath := os.Getenv("OOZE_WINDOWS_NESTED_MARKER")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		file, err := os.OpenFile(markerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := fmt.Fprintln(file, time.Now().UnixNano())
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal(writeErr, closeErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newWindowsNativeSupervisorForFixture(
	t *testing.T,
	attempt attemptIdentity,
	lineage campaignLineage,
) (*processRuntimeShell, *Supervisor) {
	t.Helper()
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: lineage})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: attempt, class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	return shell, supervisor
}

func waitWindowsFixtureTerminal(t *testing.T, attempt *OwnedAttempt) Terminal {
	t.Helper()
	completed := make(chan Terminal, 1)
	go func() { completed <- attempt.Wait() }()
	select {
	case terminal := <-completed:
		return terminal
	case <-time.After(5 * time.Second):
		t.Fatal("Windows job fixture exceeded its independent five-second wait bound")

		return nil
	}
}

func stopWindowsFixtureOnFailure(t *testing.T, attempt *OwnedAttempt) {
	t.Helper()
	t.Cleanup(func() {
		now := time.Now()
		attempt.Stop(StopRequest{At: now, DrainBy: now.Add(2 * time.Second)})
		_ = waitWindowsFixtureTerminal(t, attempt)
	})
}

func awaitWindowsFixtureFile(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		contents, err := os.ReadFile(path)
		if err == nil && len(contents) > 0 {
			return contents
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture file %q was not populated within %s: %v", path, timeout, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func releaseWindowsFixtureProcess(t *testing.T, process *os.Process) {
	t.Helper()
	t.Cleanup(func() {
		if err := process.Release(); err != nil {
			t.Errorf("release nested fixture process capability: %v", err)
		}
	})
}

func retainWindowsFixtureProcess(t *testing.T, processID int) windows.Handle {
	t.Helper()
	process, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(processID), //nolint:gosec // Windows process IDs are 32-bit values.
	)
	if err != nil {
		t.Fatalf("retain nested-job child %d: %v", processID, err)
	}
	t.Cleanup(func() {
		defer func() {
			if closeErr := windows.CloseHandle(process); closeErr != nil {
				t.Errorf("close retained nested-job child %d: %v", processID, closeErr)
			}
		}()

		var exitCode uint32
		queryErr := windows.GetExitCodeProcess(process, &exitCode)
		if queryErr != nil {
			t.Errorf("query retained nested-job child %d: %v", processID, queryErr)
		}
		var terminateErr error
		if queryErr != nil || exitCode == windowsStillActive {
			terminateErr = windows.TerminateProcess(process, 1)
		}
		result, waitErr := windows.WaitForSingleObject(process, 2_000)
		if waitErr != nil || result != windows.WAIT_OBJECT_0 {
			t.Errorf("drain retained nested-job child %d: terminate=%v wait=(%d, %v)",
				processID, terminateErr, result, waitErr)
		}
	})

	return process
}

func assertWindowsFixtureProcessStopped(t *testing.T, process windows.Handle, processID int) {
	t.Helper()
	result, err := windows.WaitForSingleObject(process, 2_000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("nested-job child %d exit proof = (%d, %v), want exact retained handle signaled", processID, result, err)
	}
}

func windowsJobFixtureEnvironment(role string, additions ...string) []string {
	prefix := windowsJobFixtureRole + "="
	environment := make([]string, 0, len(os.Environ())+len(additions)+1)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, prefix) {
			environment = append(environment, value)
		}
	}
	environment = append(environment, windowsJobFixtureRole+"="+role)

	return append(environment, additions...)
}
