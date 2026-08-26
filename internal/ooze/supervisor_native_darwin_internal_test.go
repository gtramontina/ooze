//go:build darwin

package ooze

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const darwinEscapeFixtureRole = "OOZE_DARWIN_ESCAPE_FIXTURE_ROLE"

func TestDarwinNativeCommandCannotExecuteBeforeExplicitRelease(t *testing.T) {
	marker := t.TempDir() + "/released"
	command := exec.Command("/usr/bin/touch", marker)
	state, err := prepareNativeCommand(command)
	require.NoError(t, err)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	{
		err = command.Start()
		require.NoError(t, err)
	}
	defer func() {
		_ = forceNativeDomain(state, command.Process.Pid, time.Now().Add(5*time.Second))
		_, _ = command.Process.Wait()
		_ = closeNativeDomain(state)
	}()
	{
		err = confirmNativeCommandStopped(command, state)
		require.NoError(t, err)
	}
	time.Sleep(20 * time.Millisecond)
	{
		_, err = os.Stat(marker)
		require.True(t, os.IsNotExist(err), "target executed before release: stat error=%v", err)
	}
	{
		_, err = releaseNativeCommand(command, state)
		require.NoError(t, err)
	}
	{
		_, err = command.Process.Wait()
		require.NoError(t, err)
	}
	{
		_, err = os.Stat(marker)
		assert.NoError(t, err, "released target did not execute: %v", err)
	}
}

func TestDarwinNativeLaunchResourceExhaustionRequiresExactPreReleaseEvidence(t *testing.T) {
	tests := []struct {
		name      string
		operation nativeLaunchOperation
		err       error
		stage     nativeLaunchStage
		closed    bool
		want      LaunchFailure
	}{
		{name: "descriptor limit", operation: nativeLaunchInternalOutput, err: syscall.EMFILE,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchResourceExhausted},
		{name: "descriptor memory is not whitelisted", operation: nativeLaunchInternalOutput, err: syscall.ENOMEM,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchFailed},
		{name: "launcher process limit", operation: nativeLaunchLauncherStart, err: syscall.EAGAIN,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchResourceExhausted},
		{name: "kqueue descriptor limit", operation: nativeLaunchRootTrackerCreate, err: syscall.ENFILE,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchResourceExhausted},
		{name: "note-exit only admits memory", operation: nativeLaunchRootTrackerRegister, err: syscall.EMFILE,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchFailed},
		{name: "typed exec memory", operation: nativeLaunchTargetExec, err: syscall.ENOMEM,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchResourceExhausted},
		{name: "typed exec process limit is not whitelisted", operation: nativeLaunchTargetExec, err: syscall.EAGAIN,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchFailed},
		{name: "cleanup did not prove closure", operation: nativeLaunchLauncherStart, err: syscall.ENOMEM,
			stage: nativeLaunchPreRelease, closed: false, want: LaunchFailed},
		{name: "release is unknown", operation: nativeLaunchLauncherStart, err: syscall.ENOMEM,
			stage: nativeLaunchReleaseUnknown, closed: true, want: LaunchFailed},
		{name: "cleanup-only failure", operation: nativeLaunchCleanup, err: syscall.ENOMEM,
			stage: nativeLaunchPreRelease, closed: true, want: LaunchFailed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyNativeLaunchFailure(nativeLaunchFailureEvidence{
				operation: test.operation, stage: test.stage, err: test.err, closureProven: test.closed,
			})
			assert.Equal(t, test.want, got, "classification = %v, want %v", got, test.want)
		})
	}
}

func TestDarwinNativeSupervisorPublishesImmutableNotReleasedError(t *testing.T) {
	directory := t.TempDir()
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 107})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "darwin-not-released", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	result := supervisor.Launch(Spec{
		Attempt: "darwin-not-released", Command: []string{directory + "/absent-command"},
		Dir: directory, Env: os.Environ(), Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	notReleased, ok := result.(NotReleased)
	require.True(t, ok, "launch = %#v, want immutable NotReleased error", result)
	assert.Equal(t, LaunchFailed, notReleased.Kind, "launch = %#v, want immutable NotReleased error", result)
	assert.Error(t, notReleased.Err, "launch = %#v, want immutable NotReleased error", result)
	{
		snapshot := shell.snapshot()
		assert.EqualValues(t, 0, len(snapshot.admissions), "proven no-release retained runtime custody: %#v", snapshot)
	}
}

func TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember(t *testing.T) {
	role := os.Getenv(darwinEscapeFixtureRole)
	if role != "" {
		runDarwinEscapeFixture(t, role)

		return
	}

	directory := t.TempDir()
	pidPath := directory + "/escapee.pid"
	markerPath := directory + "/escapee.marker"
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 106})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-escape-behind-member",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	launched := supervisor.Launch(Spec{
		Attempt: "darwin-escape-behind-member",
		Command: []string{os.Args[0], "-test.run=^TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember$"},
		Dir:     directory,
		Env: append(os.Environ(),
			darwinEscapeFixtureRole+"=root",
			"OOZE_DARWIN_ESCAPE_PID="+pidPath,
			"OOZE_DARWIN_ESCAPE_MARKER="+markerPath,
		),
		Profile: SerialProfile, Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	terminal := owned.Attempt.Wait()
	{
		_, ok := terminal.(Settled)
		require.True(t, ok, "terminal = %#v, want Settled", terminal)
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
	assert.Equal(t, before.Size(), after.Size(), "drainage returned while captured escapee %d remained executable", escapee)
}

func runDarwinEscapeFixture(t *testing.T, role string) {
	t.Helper()
	pidPath := os.Getenv("OOZE_DARWIN_ESCAPE_PID")
	markerPath := os.Getenv("OOZE_DARWIN_ESCAPE_MARKER")
	switch role {
	case "root":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember$")
		command.Env = darwinEscapeFixtureEnvironment("intermediate")
		{
			err := command.Start()
			require.NoError(t, err)
		}
		awaitDarwinEscapeFixtureFile(t, pidPath)
	case "intermediate":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember$")
		command.Env = darwinEscapeFixtureEnvironment("escapee")
		{
			err := command.Start()
			require.NoError(t, err)
		}
		awaitDarwinEscapeFixtureFile(t, pidPath)
		for {
			time.Sleep(time.Second)
		}
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
		require.FailNowf(t, "unknown Darwin escape fixture role", "role: %q", role)
	}
}

func darwinEscapeFixtureEnvironment(role string) []string {
	prefix := darwinEscapeFixtureRole + "="
	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, prefix) {
			environment = append(environment, variable)
		}
	}

	return append(environment, prefix+role)
}

func awaitDarwinEscapeFixtureFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	require.FailNowf(t, "timed out awaiting Darwin escape fixture file", "path: %s", path)
}

func TestDarwinNativeSupervisorSettlesSerialCommandThroughPublicLifecycle(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 101})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-native",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			assert.Equal(t, grant.attempt, attempt, "start attempt = %q, want %q", attempt, grant.attempt)
			prepared := shell.startCommitted(grant, startInstallation{grant: grant, cell: cell})

			return prepared.start
		},
		driver,
	)

	launched := supervisor.Launch(Spec{
		Attempt:  "darwin-native",
		Command:  []string{"/bin/sh", "-c", "printf 'native-output'; exit 17"},
		Dir:      t.TempDir(),
		Env:      os.Environ(),
		Profile:  SerialProfile,
		Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	terminal := owned.Attempt.Wait()
	settled, ok := terminal.(Settled)
	require.True(t, ok, "terminal = %#v, want Settled", terminal)
	assert.Equal(t, (ExitStatus{Code: 17}), settled.Exit, "settled native evidence = %#v", settled)
	assert.EqualValues(t, "native-output", settled.Output.Bytes, "settled native evidence = %#v", settled)
	assert.True(t, settled.Output.CompleteThroughCutoff, "settled native evidence = %#v", settled)
	assert.True(t, settled.Output.Final, "settled native evidence = %#v", settled)
	{
		snapshot := shell.snapshot()
		assert.Equal(t, runtimeOpen, snapshot.lifecycle, "runtime after native settlement = %#v", snapshot)
		assert.EqualValues(t, 0, len(snapshot.admissions), "runtime after native settlement = %#v", snapshot)
	}
}

func TestDarwinNativeSupervisorTripsSerialCommandAtResolvedDeadline(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 102})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-deadline",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	launched := supervisor.Launch(Spec{
		Attempt:  "darwin-deadline",
		Command:  []string{"/bin/sh", "-c", "sleep 10"},
		Dir:      t.TempDir(),
		Env:      os.Environ(),
		Profile:  SerialProfile,
		Deadline: 50 * time.Millisecond,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	started := time.Now()
	terminal := owned.Attempt.Wait()
	{
		elapsed := time.Since(started)
		assert.False(t, elapsed >= 5*time.Second, "deadline terminal took %s", elapsed)
	}
	tripped, ok := terminal.(Tripped)
	require.True(t, ok, "terminal = %#v, want Tripped", terminal)
	{
		_, ok := tripped.Trip.(SerialDeadlineTrip)
		require.True(t, ok, "serial deadline evidence = %#v", tripped)
		assert.Equal(t, CommandDeadlineFired, tripped.BoundFired, "serial deadline evidence = %#v", tripped)
		assert.Equal(t, 50*time.Millisecond, tripped.CommandDuration, "serial deadline evidence = %#v", tripped)
	}
}

func TestDarwinNativeSupervisorReturnsRuntimeProvedOverlapDeadlineForClassification(t *testing.T) {
	shell := newProcessRuntimeShell(2)
	grants := make(map[attemptIdentity]admissionGrant)
	for index, attempt := range []attemptIdentity{"darwin-overlap-deadline", "darwin-overlap-peer"} {
		campaign := shell.registerCampaign(campaignProvenance{lineage: campaignLineage(120 + index)})
		requested := shell.requestAdmission(admissionRequest{
			campaign: campaign.token, attempt: attempt, class: sharedAdmission,
		})
		grants[attempt] = <-requested.delivery
	}
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(attempt attemptIdentity, cell *pendingStartCell) installedStart {
			grant, ok := grants[attempt]
			require.True(t, ok, "start attempt = %q, want registered overlap attempt", attempt)

			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	primaryLaunch := supervisor.Launch(Spec{
		Attempt: "darwin-overlap-deadline", Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: AutomaticProfile, Deadline: 500 * time.Millisecond,
	})
	primary, ok := primaryLaunch.(Owned)
	require.True(t, ok, "primary launch = %#v, want Owned", primaryLaunch)
	require.NotNil(t, primary.Attempt, "primary launch = %#v, want Owned", primaryLaunch)
	peerLaunch := supervisor.Launch(Spec{
		Attempt: "darwin-overlap-peer", Command: []string{"/bin/sh", "-c", "sleep 1"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: AutomaticProfile, Deadline: 2 * time.Second,
	})
	peer, ok := peerLaunch.(Owned)
	require.True(t, ok, "peer launch = %#v, want Owned", peerLaunch)
	require.NotNil(t, peer.Attempt, "peer launch = %#v, want Owned", peerLaunch)

	terminal := primary.Attempt.Wait()
	disposition := ClassifyPrimaryMutation(terminal)
	provisional, ok := disposition.(MutationNeedsConfirmation)
	require.True(t, ok, "overlapped public terminal = %#v, want MutationNeedsConfirmation", disposition)
	assert.Equal(t, terminal, provisional.Primary(), "overlapped public terminal = %#v, want MutationNeedsConfirmation", disposition)
	{
		settled, ok := peer.Attempt.Wait().(Settled)
		require.True(t, ok, "overlap peer terminal = %#v, want passing Settled", settled)
		assert.True(t, settled.Exit.Passed(), "overlap peer terminal = %#v, want passing Settled", settled)
	}
}

func TestDarwinNativeSupervisorTripsAutomaticDescendantFuse(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 103})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-fuse",
		class:    sharedAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	launched := supervisor.Launch(Spec{
		Attempt: "darwin-fuse",
		Command: []string{
			"/bin/sh", "-c",
			"i=0; while [ $i -lt 65 ]; do sleep 10 & i=$((i+1)); done; wait",
		},
		Dir: t.TempDir(), Env: os.Environ(), Profile: AutomaticProfile,
		Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	terminal := owned.Attempt.Wait()
	tripped, ok := terminal.(Tripped)
	require.True(t, ok, "terminal = %#v, want Tripped", terminal)
	fuse, ok := tripped.Trip.(FuseTrip)
	require.True(t, ok, "automatic fuse evidence = %#v", tripped)
	assert.False(t, fuse.Live < 65, "automatic fuse evidence = %#v", tripped)
	assert.Equal(t, NoBoundFired, tripped.BoundFired, "automatic fuse evidence = %#v", tripped)
}

func TestDarwinNativeSupervisorEmergencyDrainsWithoutWaiter(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 104})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-emergency",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	launched := supervisor.Launch(Spec{
		Attempt: "darwin-emergency",
		Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir:     t.TempDir(), Env: os.Environ(), Profile: SerialProfile,
		Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	emergencyAt := time.Now()
	shell.closeRuntime(runtimeFatalCause("native emergency test"))
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
	})
	{
		_, ok := settlement.(SweepDrained)
		require.True(t, ok, "emergency settlement = %#v, want SweepDrained", settlement)
	}
	terminal := owned.Attempt.Wait()
	stopped, ok := terminal.(Stopped)
	require.True(t, ok, "emergency terminal = %#v, want Stopped", terminal)
	assert.Equal(t, NoBoundFired, stopped.BoundFired, "emergency terminal = %#v, want Stopped", terminal)
}

func TestDarwinNativeSupervisorStopsOwnedCommandBeforeWait(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 105})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-stop",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	launched := supervisor.Launch(Spec{
		Attempt: "darwin-stop",
		Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir:     t.TempDir(), Env: os.Environ(), Profile: SerialProfile,
		Deadline: 10 * time.Second,
	})
	owned, ok := launched.(Owned)
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	stopAt := time.Now()
	owned.Attempt.Stop(StopRequest{At: stopAt, DrainBy: stopAt.Add(5 * time.Second)})
	terminal := owned.Attempt.Wait()
	stopped, ok := terminal.(Stopped)
	require.True(t, ok, "stop terminal = %#v, want Stopped", terminal)
	assert.Equal(t, NoBoundFired, stopped.BoundFired, "stop terminal = %#v, want Stopped", terminal)
	assert.False(t, stopped.CommandDuration <= 0, "stop terminal = %#v, want Stopped", terminal)
}

func TestDarwinCapturedIdentitiesRemainTrackedBeforeControl(t *testing.T) {
	first := darwinProcessIdentity{pid: 41, startSec: 7, startUsec: 11}
	second := darwinProcessIdentity{pid: 42, startSec: 8, startUsec: 12}
	tracked := map[darwinProcessIdentity]struct{}{first: {}}
	trackDarwinIdentities(tracked, map[darwinProcessIdentity]struct{}{second: {}})
	{
		_, ok := tracked[first]
		require.True(t, ok, "existing Darwin identity was discarded")
	}
	{
		_, ok := tracked[second]
		assert.True(t, ok, "new Darwin identity was not retained before control")
	}
}

func TestDarwinCapturedIdentityControlDoesNotDependOnGroupDelivery(t *testing.T) {
	first := darwinProcessIdentity{pid: 41, startSec: 7, startUsec: 11}
	second := darwinProcessIdentity{pid: 42, startSec: 8, startUsec: 12}
	captured := map[darwinProcessIdentity]struct{}{first: {}, second: {}}
	for _, test := range []struct {
		name     string
		groupErr error
	}{
		{"group_signal_succeeds", nil},
		{"group_signal_permission_denied", syscall.EPERM},
		{"group_signal_process_missing", syscall.ESRCH},
	} {
		t.Run(test.name, func(t *testing.T) {
			controlled := make(map[darwinProcessIdentity]struct{})
			err := signalDarwinCapturedIdentities(
				captured, 41, syscall.SIGSTOP,
				func(int, syscall.Signal) error { return test.groupErr },
				func(identity darwinProcessIdentity, _ syscall.Signal) error {
					controlled[identity] = struct{}{}

					return nil
				},
			)
			require.NoError(t, err, "group error %v: exact control = %v, want success", test.groupErr, err)
			assert.Equal(t, len(captured), len(controlled), "group error %v: controlled identities = %#v, want %#v", test.groupErr, controlled, captured)
		})
	}
}

func TestDarwinCapturedIdentityControlFailsClosed(t *testing.T) {
	first := darwinProcessIdentity{pid: 41, startSec: 7, startUsec: 11}
	second := darwinProcessIdentity{pid: 42, startSec: 8, startUsec: 12}
	captured := map[darwinProcessIdentity]struct{}{first: {}, second: {}}
	groupErr := syscall.EPERM
	exactErr := errors.New("exact identity control denied")
	controlled := make(map[darwinProcessIdentity]struct{})
	err := signalDarwinCapturedIdentities(
		captured, 41, syscall.SIGKILL,
		func(int, syscall.Signal) error { return groupErr },
		func(identity darwinProcessIdentity, _ syscall.Signal) error {
			controlled[identity] = struct{}{}
			if identity == first {
				return exactErr
			}

			return nil
		},
	)
	require.ErrorIs(t, err, groupErr, "control error = %v, want group and exact identity evidence", err)
	assert.ErrorIs(t, err, exactErr, "control error = %v, want group and exact identity evidence", err)
	assert.Equal(t, len(captured), len(controlled), "controlled identities = %#v, want every captured identity attempted", controlled)
}

func TestDarwinStaleTrackedIdentityDoesNotSeedPidReusedDescendants(t *testing.T) {
	stale := darwinProcessIdentity{pid: 41, startSec: 7, startUsec: 11}
	reused := darwinProcessIdentity{pid: 41, startSec: 8, startUsec: 12}
	unrelated := darwinProcessIdentity{pid: 42, startSec: 9, startUsec: 13}
	reached := darwinReachableDomain([]darwinProcessSnapshot{
		{identity: reused, parent: 1, group: 99},
		{identity: unrelated, parent: 41, group: 99},
	}, 77, map[darwinProcessIdentity]struct{}{stale: {}})
	{
		_, ok := reached[unrelated]
		assert.False(t, ok, "PID reuse allowed a stale tracked identity to capture an unrelated descendant")
	}
}
