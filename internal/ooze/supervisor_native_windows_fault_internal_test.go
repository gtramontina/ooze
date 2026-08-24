//go:build windows

package ooze

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWindowsNativeClassifierExtractsWrappedResourceStatus(t *testing.T) {
	err := fmt.Errorf("create private merged output: %w", syscall.Errno(8))
	got := classifyNativeLaunchFailure(nativeLaunchFailureEvidence{
		operation: nativeLaunchInternalOutput, stage: nativeLaunchPreRelease,
		err: err, closureProven: true,
	})
	if got != LaunchResourceExhausted {
		t.Fatalf("classification = %v, want resource exhausted", got)
	}
}

func TestWindowsNativeOutputFaultPublishesResourceExhaustedThroughPublicLaunch(t *testing.T) {
	fault := fmt.Errorf("create private merged output: %w", syscall.Errno(8))
	executor := &supervisorNativeExecutor{
		drainEpoch: 5 * time.Second,
		attempts:   make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:    make(map[supervisorOutputRef]string), diagnostics: make(map[supervisorDiagnosticRef]error),
		createOutputFile: func() (*os.File, error) { return nil, fault },
		readOutputFile:   readNativeOutput,
	}
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 501})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "windows-output-resource-fault", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: time.Now,
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 5 * time.Second,
		prepare: executor.prepare, execute: executor.execute,
		readOutput: executor.readOutput, readDiagnostic: executor.readDiagnostic,
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	result := supervisor.Launch(Spec{
		Attempt: "windows-output-resource-fault", Command: []string{"unreachable.exe"}, Dir: `C:\`,
		Profile: SerialProfile, Deadline: time.Second,
	})
	notReleased, ok := result.(NotReleased)
	if !ok || notReleased.Kind != LaunchResourceExhausted || notReleased.Err == nil ||
		!strings.Contains(notReleased.Err.Error(), fault.Error()) {
		t.Fatalf("launch = %#v, want immutable Windows resource-exhausted fault", result)
	}
	if errors.Is(notReleased.Err, syscall.Errno(8)) {
		t.Fatalf("public launch error retained mutable native identity: %v", notReleased.Err)
	}
}
