//go:build darwin

package ooze

import (
	"os"
	"testing"
	"time"
)

func TestDarwinNativeSupervisorSettlesSerialCommandThroughPublicLifecycle(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 101})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-native",
		class:    serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriver(shell, time.Second, 5*time.Second)
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
	driver := newNativeSupervisorDriver(shell, time.Second, 5*time.Second)
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

func TestDarwinNativeSupervisorTripsAutomaticDescendantFuse(t *testing.T) {
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 103})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token,
		attempt:  "darwin-fuse",
		class:    sharedAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriver(shell, time.Second, 5*time.Second)
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
	driver := newNativeSupervisorDriver(shell, time.Second, 5*time.Second)
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
