//go:build linux

package ooze

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const linuxEscapeFixtureRole = "OOZE_LINUX_ESCAPE_FIXTURE_ROLE"

func TestLinuxNativeSupervisorSettlesTargetStatusThroughGuardian(t *testing.T) {
	shell, supervisor := newLinuxNativeSupervisorForTest(t, "linux-native-status", 201)
	launched := supervisor.Launch(Spec{
		Attempt: "linux-native-status",
		Command: []string{"/bin/sh", "-c", "printf 'linux-native-output'; exit 17"},
		Dir:     t.TempDir(), Env: os.Environ(), Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	terminal := owned.Attempt.Wait()
	settled, ok := terminal.(Settled)
	if !ok || settled.Exit != (ExitStatus{Code: 17}) || settled.Output.Bytes != "linux-native-output" {
		t.Fatalf("terminal = %#v, want exact target status and output", terminal)
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeOpen || len(snapshot.admissions) != 0 {
		t.Fatalf("runtime after Linux settlement = %#v", snapshot)
	}
}

func TestLinuxNativeSupervisorForcesGuardianDomainAtDeadline(t *testing.T) {
	_, supervisor := newLinuxNativeSupervisorForTest(t, "linux-native-deadline", 203)
	launched := supervisor.Launch(Spec{
		Attempt: "linux-native-deadline", Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: SerialProfile, Deadline: 50 * time.Millisecond,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	terminal := owned.Attempt.Wait()
	tripped, ok := terminal.(Tripped)
	if !ok || tripped.BoundFired != CommandDeadlineFired || tripped.CommandDuration != 50*time.Millisecond {
		t.Fatalf("terminal = %#v, want exact Linux command deadline", terminal)
	}
	if _, ok := tripped.Trip.(SerialDeadlineTrip); !ok {
		t.Fatalf("trip = %#v, want SerialDeadlineTrip", tripped.Trip)
	}
}

func TestLinuxNativeSupervisorProvesTypedTargetExecFailure(t *testing.T) {
	directory := t.TempDir()
	shell, supervisor := newLinuxNativeSupervisorForTest(t, "linux-native-exec-failure", 204)
	result := supervisor.Launch(Spec{
		Attempt: "linux-native-exec-failure", Command: []string{directory + "/absent-target"},
		Dir: directory, Env: os.Environ(), Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	notReleased, ok := result.(NotReleased)
	if !ok || notReleased.Kind != LaunchFailed || notReleased.Err == nil {
		t.Fatalf("launch = %#v, want proven typed target exec failure", result)
	}
	if snapshot := shell.snapshot(); len(snapshot.admissions) != 0 {
		t.Fatalf("typed target exec failure retained custody: %#v", snapshot)
	}
}

func TestLinuxNativeSupervisorReapsOrphanedEscapeeThroughGuardian(t *testing.T) {
	role := os.Getenv(linuxEscapeFixtureRole)
	if role != "" {
		runLinuxEscapeFixture(t, role)

		return
	}
	directory := t.TempDir()
	pidPath := directory + "/escapee.pid"
	markerPath := directory + "/escapee.marker"
	_, supervisor := newLinuxNativeSupervisorForTest(t, "linux-native-escape", 202)
	launched := supervisor.Launch(Spec{
		Attempt: "linux-native-escape",
		Command: []string{os.Args[0], "-test.run=^TestLinuxNativeSupervisorReapsOrphanedEscapeeThroughGuardian$"},
		Dir:     directory,
		Env: append(os.Environ(),
			linuxEscapeFixtureRole+"=root",
			"OOZE_LINUX_ESCAPE_PID="+pidPath,
			"OOZE_LINUX_ESCAPE_MARKER="+markerPath,
		),
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	if terminal := owned.Attempt.Wait(); terminal == nil {
		t.Fatal("Linux escape fixture returned no terminal")
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	escapee, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Kill(escapee, syscall.SIGKILL) }()
	before, err := os.Stat(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	after, err := os.Stat(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("guardian drainage returned while orphaned escapee %d remained executable", escapee)
	}
}

func newLinuxNativeSupervisorForTest(
	t *testing.T,
	attempt string,
	lineage campaignLineage,
) (*processRuntimeShell, *Supervisor) {
	t.Helper()
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: lineage})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: attemptIdentity(attempt), class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriver(shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	return shell, supervisor
}

func runLinuxEscapeFixture(t *testing.T, role string) {
	t.Helper()
	pidPath := os.Getenv("OOZE_LINUX_ESCAPE_PID")
	markerPath := os.Getenv("OOZE_LINUX_ESCAPE_MARKER")
	switch role {
	case "root":
		command := exec.Command(os.Args[0], "-test.run=^TestLinuxNativeSupervisorReapsOrphanedEscapeeThroughGuardian$")
		command.Env = linuxEscapeFixtureEnvironment("escapee")
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		awaitLinuxEscapeFixtureFile(t, pidPath)
	case "escapee":
		if err := syscall.Setpgid(0, 0); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			file, err := os.OpenFile(markerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fmt.Fprintln(file, time.Now().UnixNano())
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
			time.Sleep(5 * time.Millisecond)
		}
	default:
		t.Fatalf("unknown Linux escape fixture role %q", role)
	}
}

func linuxEscapeFixtureEnvironment(role string) []string {
	prefix := linuxEscapeFixtureRole + "="
	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, prefix) {
			environment = append(environment, variable)
		}

	}

	return append(environment, prefix+role)
}

func awaitLinuxEscapeFixtureFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out awaiting Linux escape fixture file %s", path)
}
