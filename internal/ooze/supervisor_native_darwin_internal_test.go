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
