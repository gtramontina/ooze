//go:build darwin

package ooze

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const darwinEscapeFixtureRole = "OOZE_DARWIN_ESCAPE_FIXTURE_ROLE"

func TestDarwinNativeCommandCannotExecuteBeforeExplicitRelease(t *testing.T) {
	marker := t.TempDir() + "/released"
	command := exec.Command("/usr/bin/touch", marker)
	state, err := prepareNativeCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = forceNativeDomain(state, command.Process.Pid, time.Now().Add(5*time.Second))
		_, _ = command.Process.Wait()
		_ = closeNativeDomain(state)
	}()
	if err = confirmNativeCommandStopped(command, state); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("target executed before release: stat error=%v", err)
	}
	if _, err = releaseNativeCommand(command, state); err != nil {
		t.Fatal(err)
	}
	if _, err = command.Process.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(marker); err != nil {
		t.Fatalf("released target did not execute: %v", err)
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
			if got != test.want {
				t.Fatalf("classification = %v, want %v", got, test.want)
			}
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
	if !ok || notReleased.Kind != LaunchFailed || notReleased.Err == nil {
		t.Fatalf("launch = %#v, want immutable NotReleased error", result)
	}
	if snapshot := shell.snapshot(); len(snapshot.admissions) != 0 {
		t.Fatalf("proven no-release retained runtime custody: %#v", snapshot)
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
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	terminal := owned.Attempt.Wait()
	if _, ok := terminal.(Settled); !ok {
		t.Fatalf("terminal = %#v, want Settled", terminal)
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
		t.Fatalf("drainage returned while captured escapee %d remained executable", escapee)
	}
}

func runDarwinEscapeFixture(t *testing.T, role string) {
	t.Helper()
	pidPath := os.Getenv("OOZE_DARWIN_ESCAPE_PID")
	markerPath := os.Getenv("OOZE_DARWIN_ESCAPE_MARKER")
	switch role {
	case "root":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember$")
		command.Env = darwinEscapeFixtureEnvironment("intermediate")
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		awaitDarwinEscapeFixtureFile(t, pidPath)
	case "intermediate":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinNativeSupervisorCapturesEscapeeBehindLiveGroupMember$")
		command.Env = darwinEscapeFixtureEnvironment("escapee")
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		awaitDarwinEscapeFixtureFile(t, pidPath)
		for {
			time.Sleep(time.Second)
		}
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
		t.Fatalf("unknown Darwin escape fixture role %q", role)
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
	t.Fatalf("timed out awaiting Darwin escape fixture file %s", path)
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
			if attempt != grant.attempt {
				t.Fatalf("start attempt = %q, want %q", attempt, grant.attempt)
			}
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
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	terminal := owned.Attempt.Wait()
	settled, ok := terminal.(Settled)
	if !ok {
		t.Fatalf("terminal = %#v, want Settled", terminal)
	}
	if settled.Exit != (ExitStatus{Code: 17}) || settled.Output.Bytes != "native-output" ||
		!settled.Output.CompleteThroughCutoff || !settled.Output.Final {
		t.Fatalf("settled native evidence = %#v", settled)
	}
	if snapshot := shell.snapshot(); snapshot.lifecycle != runtimeOpen || len(snapshot.admissions) != 0 {
		t.Fatalf("runtime after native settlement = %#v", snapshot)
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
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	started := time.Now()
	terminal := owned.Attempt.Wait()
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("deadline terminal took %s", elapsed)
	}
	tripped, ok := terminal.(Tripped)
	if !ok {
		t.Fatalf("terminal = %#v, want Tripped", terminal)
	}
	if _, ok := tripped.Trip.(SerialDeadlineTrip); !ok ||
		tripped.BoundFired != CommandDeadlineFired ||
		tripped.CommandDuration != 50*time.Millisecond {
		t.Fatalf("serial deadline evidence = %#v", tripped)
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
			if !ok {
				t.Fatalf("start attempt = %q, want registered overlap attempt", attempt)
			}

			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)

	primaryLaunch := supervisor.Launch(Spec{
		Attempt: "darwin-overlap-deadline", Command: []string{"/bin/sh", "-c", "sleep 10"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: AutomaticProfile, Deadline: 500 * time.Millisecond,
	})
	primary, ok := primaryLaunch.(Owned)
	if !ok || primary.Attempt == nil {
		t.Fatalf("primary launch = %#v, want Owned", primaryLaunch)
	}
	peerLaunch := supervisor.Launch(Spec{
		Attempt: "darwin-overlap-peer", Command: []string{"/bin/sh", "-c", "sleep 1"},
		Dir: t.TempDir(), Env: os.Environ(), Profile: AutomaticProfile, Deadline: 2 * time.Second,
	})
	peer, ok := peerLaunch.(Owned)
	if !ok || peer.Attempt == nil {
		t.Fatalf("peer launch = %#v, want Owned", peerLaunch)
	}

	terminal := primary.Attempt.Wait()
	disposition := ClassifyPrimaryMutation(terminal)
	provisional, ok := disposition.(MutationNeedsConfirmation)
	if !ok || provisional.Primary() != terminal {
		t.Fatalf("overlapped public terminal = %#v, want MutationNeedsConfirmation", disposition)
	}
	if settled, ok := peer.Attempt.Wait().(Settled); !ok || !settled.Exit.Passed() {
		t.Fatalf("overlap peer terminal = %#v, want passing Settled", settled)
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
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	terminal := owned.Attempt.Wait()
	tripped, ok := terminal.(Tripped)
	if !ok {
		t.Fatalf("terminal = %#v, want Tripped", terminal)
	}
	fuse, ok := tripped.Trip.(FuseTrip)
	if !ok || fuse.Live < 65 || tripped.BoundFired != NoBoundFired {
		t.Fatalf("automatic fuse evidence = %#v", tripped)
	}
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
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	emergencyAt := time.Now()
	shell.closeRuntime(runtimeFatalCause("native emergency test"))
	settlement := supervisor.EmergencyDrain(EmergencyRequest{
		At: emergencyAt, DrainBy: emergencyAt.Add(5 * time.Second),
	})
	if _, ok := settlement.(SweepDrained); !ok {
		t.Fatalf("emergency settlement = %#v, want SweepDrained", settlement)
	}
	terminal := owned.Attempt.Wait()
	stopped, ok := terminal.(Stopped)
	if !ok || stopped.BoundFired != NoBoundFired {
		t.Fatalf("emergency terminal = %#v, want Stopped", terminal)
	}
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
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	stopAt := time.Now()
	owned.Attempt.Stop(StopRequest{At: stopAt, DrainBy: stopAt.Add(5 * time.Second)})
	terminal := owned.Attempt.Wait()
	stopped, ok := terminal.(Stopped)
	if !ok || stopped.BoundFired != NoBoundFired || stopped.CommandDuration <= 0 {
		t.Fatalf("stop terminal = %#v, want Stopped", terminal)
	}
}

func TestDarwinCapturedIdentitiesRemainTrackedBeforeControl(t *testing.T) {
	first := darwinProcessIdentity{pid: 41, startSec: 7, startUsec: 11}
	second := darwinProcessIdentity{pid: 42, startSec: 8, startUsec: 12}
	tracked := map[darwinProcessIdentity]struct{}{first: {}}
	trackDarwinIdentities(tracked, map[darwinProcessIdentity]struct{}{second: {}})
	if _, ok := tracked[first]; !ok {
		t.Fatal("existing Darwin identity was discarded")
	}
	if _, ok := tracked[second]; !ok {
		t.Fatal("new Darwin identity was not retained before control")
	}
}

func TestDarwinStaleTrackedIdentityDoesNotSeedPidReusedDescendants(t *testing.T) {
	stale := darwinProcessIdentity{pid: 41, startSec: 7, startUsec: 11}
	reused := darwinProcessIdentity{pid: 41, startSec: 8, startUsec: 12}
	unrelated := darwinProcessIdentity{pid: 42, startSec: 9, startUsec: 13}
	reached := darwinReachableDomain([]darwinProcessSnapshot{
		{identity: reused, parent: 1, group: 99},
		{identity: unrelated, parent: 41, group: 99},
	}, 77, map[darwinProcessIdentity]struct{}{stale: {}})
	if _, ok := reached[unrelated]; ok {
		t.Fatal("PID reuse allowed a stale tracked identity to capture an unrelated descendant")
	}
}
