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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	terminal := owned.Attempt.Wait()
	settled, ok := terminal.(Settled)
	require.True(t, ok, "terminal = %#v, want exact target status and output", terminal)
	assert.Equal(t, (ExitStatus{Code: 17}), settled.Exit, "terminal = %#v, want exact target status and output", terminal)
	assert.EqualValues(t, "linux-native-output", settled.Output.Bytes, "terminal = %#v, want exact target status and output", terminal)
	{
		snapshot := shell.snapshot()
		assert.Equal(t, runtimeOpen, snapshot.lifecycle, "runtime after Linux settlement = %#v", snapshot)
		assert.EqualValues(t, 0, len(snapshot.admissions), "runtime after Linux settlement = %#v", snapshot)
	}
}

func TestLinuxNativeSupervisorForcesGuardianDomainAtDeadline(t *testing.T) {
	_, supervisor := newLinuxNativeSupervisorForTest(t, "linux-native-deadline", 203)
	launched := supervisor.Launch(Spec{
		Attempt: "linux-native-deadline", Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: SerialProfile, Deadline: 50 * time.Millisecond,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	terminal := owned.Attempt.Wait()
	tripped, ok := terminal.(Tripped)
	require.True(t, ok, "terminal = %#v, want exact Linux command deadline", terminal)
	assert.Equal(t, CommandDeadlineFired, tripped.BoundFired, "terminal = %#v, want exact Linux command deadline", terminal)
	assert.Equal(t, 50*time.Millisecond, tripped.CommandDuration, "terminal = %#v, want exact Linux command deadline", terminal)
	{
		_, ok := tripped.Trip.(SerialDeadlineTrip)
		require.True(t, ok, "trip = %#v, want SerialDeadlineTrip", tripped.Trip)
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
	require.True(t, ok, "launch = %#v, want proven typed target exec failure", result)
	assert.Equal(t, LaunchFailed, notReleased.Kind, "launch = %#v, want proven typed target exec failure", result)
	assert.NotNil(t, notReleased.Err, "launch = %#v, want proven typed target exec failure", result)
	{
		snapshot := shell.snapshot()
		assert.EqualValues(t, 0, len(snapshot.admissions), "typed target exec failure retained custody: %#v", snapshot)
	}
}

func TestLinuxRevokedPreReleaseGuardianCleanupIsBounded(t *testing.T) {
	executor := &supervisorNativeExecutor{
		drainEpoch:     2 * time.Second,
		attempts:       make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:        make(map[supervisorOutputRef]string),
		diagnostics:    make(map[supervisorDiagnosticRef]error),
		readOutputFile: readNativeOutput,
	}
	generation := attemptGeneration(1)
	executor.prepare(generation, Spec{
		Attempt: "linux-revoked-pre-release", Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	executor.mutex.Lock()
	executor.requireAttempt(generation).releaseRevoked = true
	executor.mutex.Unlock()
	completed := make(chan *supervisorEvent, 1)
	go func() {
		completed <- executor.launch(supervisorAction{
			kind: supervisorLaunchNative, generation: generation, token: 1,
		})
	}()
	select {
	case event := <-completed:
		require.NotNil(t, event, "revoked Linux launch completion = %#v, want proven not released", event)
		require.NotNil(t, event.completion, "revoked Linux launch completion = %#v, want proven not released", event)
		assert.Equal(t, supervisorLaunchProvenNotReleased, event.completion.kind, "revoked Linux launch completion = %#v, want proven not released", event)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "revoked stopped Linux guardian cleanup did not resolve")
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
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	{
		terminal := owned.Attempt.Wait()
		assert.NotNil(t, terminal, "Linux escape fixture returned no terminal")
	}
	pidBytes, err := os.ReadFile(pidPath)
	require.NoError(t, err)
	escapee, err := strconv.Atoi(string(pidBytes))
	require.NoError(t, err)
	defer func() { _ = syscall.Kill(escapee, syscall.SIGKILL) }()
	before, err := os.Stat(markerPath)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	after, err := os.Stat(markerPath)
	require.NoError(t, err)
	assert.Equal(t, before.Size(), after.Size(), "guardian drainage returned while orphaned escapee %d remained executable", escapee)
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
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
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
		{
			err := command.Start()
			require.NoError(t, err)
		}
		awaitLinuxEscapeFixtureFile(t, pidPath)
	case "escapee":
		{
			err := syscall.Setpgid(0, 0)
			require.NoError(t, err)
		}
		{
			err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
			require.NoError(t, err)
		}
		for {
			file, err := os.OpenFile(markerPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			require.NoError(t, err)
			_, err = fmt.Fprintln(file, time.Now().UnixNano())
			closeErr := file.Close()
			require.NoError(t, err, "write fixture marker; close error=%v", closeErr)
			assert.NoError(t, closeErr, "close fixture marker")
			time.Sleep(5 * time.Millisecond)
		}
	default:
		require.FailNow(t, "unknown Linux escape fixture role %q", role)
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
	require.FailNow(t, "timed out awaiting Linux escape fixture file %s", path)
}
