//go:build windows

package ooze

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWindowsNativeClassifierExtractsWrappedResourceStatus(t *testing.T) {
	err := fmt.Errorf("create private merged output: %w", syscall.Errno(8))
	got := classifyNativeLaunchFailure(nativeLaunchFailureEvidence{
		operation: nativeLaunchInternalOutput, stage: nativeLaunchPreRelease,
		err: err, closureProven: true,
	})
	assert.Equal(t, LaunchResourceExhausted, got, "classification = %v, want resource exhausted", got)
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
	require.True(t, ok, "launch = %#v, want immutable Windows resource-exhausted fault", result)
	assert.Equal(t, LaunchResourceExhausted, notReleased.Kind, "launch = %#v, want immutable Windows resource-exhausted fault", result)
	require.Error(t, notReleased.Err, "launch = %#v, want immutable Windows resource-exhausted fault", result)
	assert.Contains(t, notReleased.Err.Error(), fault.Error(), "launch = %#v, want immutable Windows resource-exhausted fault", result)
	assert.NotErrorIs(t, notReleased.Err, syscall.Errno(8), "public launch error retained mutable native identity: %v", notReleased.Err)
}
