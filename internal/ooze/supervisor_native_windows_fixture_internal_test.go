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
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const windowsJobFixtureRole = "OOZE_WINDOWS_JOB_FIXTURE_ROLE"

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
	default:
		t.Fatalf("unknown Windows job fixture role %q", role)
	}
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
