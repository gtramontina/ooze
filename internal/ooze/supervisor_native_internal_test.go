package ooze

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

const nativeDrainExpiryFixtureRole = "OOZE_NATIVE_DRAIN_EXPIRY_FIXTURE"

func newNativeSupervisorDriverForTest(
	t *testing.T,
	runtime *processRuntimeShell,
	launchProgress time.Duration,
	drainEpoch time.Duration,
) *supervisorDriver {
	t.Helper()
	driver, err := newNativeSupervisorDriver(runtime, launchProgress, drainEpoch)
	if err != nil {
		t.Fatalf("construct native supervisor driver: %v", err)
	}

	return driver
}

func TestNativeSupervisorDrainExpiryNeverManufacturesEmptiness(t *testing.T) {
	if !nativeSupervisorSupported() {
		t.Skip("native supervision is unavailable on this operating system")
	}
	if os.Getenv(nativeDrainExpiryFixtureRole) == "child" {
		return
	}

	const attempt = "native-drain-expiry"
	var allowEmpty atomic.Bool
	var observedRoot atomic.Int64
	var observations atomic.Int64
	executor := &supervisorNativeExecutor{
		drainEpoch: 20 * time.Millisecond,
		attempts:   make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:    make(map[supervisorOutputRef]string), diagnostics: make(map[supervisorDiagnosticRef]error),
		createOutputFile: createNativeOutputFile,
		readOutputFile:   readNativeOutput,
		domainEmpty: func(_ nativePlatformState, root int) (bool, error) {
			observedRoot.CompareAndSwap(0, int64(root))
			if root <= 0 || int64(root) != observedRoot.Load() {
				return false, errors.New("emptiness fixture observed an invalid or changing root identity")
			}
			observations.Add(1)

			return allowEmpty.Load(), nil
		},
	}
	t.Cleanup(func() {
		// DrainUnconfirmed deliberately retains native custody. Close retained
		// test handles even after an assertion failure; every real child in this
		// fixture exits before the false-negative emptiness oracle is consulted.
		executor.mutex.Lock()
		defer executor.mutex.Unlock()
		for _, nativeAttempt := range executor.attempts {
			cleanupNativeFixtureOutput(t, nativeAttempt.output)
			if err := closeNativeDomain(nativeAttempt.platform); err != nil {
				t.Errorf("close retained native fixture domain: %v", err)
			}
		}
	})
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 502})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: attempt, class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newSupervisorDriver(supervisorDriverConstruction{
		runtime: shell, now: time.Now,
		launchBoundary: func(time.Time) <-chan time.Time { return make(chan time.Time) },
		launchProgress: time.Second, drainEpoch: 20 * time.Millisecond,
		prepare: executor.prepare, execute: executor.execute,
		recheckRoot: executor.recheckRoot, sampleRunning: executor.sampleRunning,
		readOutput: executor.readOutput, readDiagnostic: executor.readDiagnostic,
		recordDiagnostic: executor.recordDiagnostic,
	})
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	launched := supervisor.Launch(Spec{
		Attempt: attempt,
		Command: []string{os.Args[0], "-test.run=^TestNativeSupervisorDrainExpiryNeverManufacturesEmptiness$"},
		Dir:     t.TempDir(), Env: append(os.Environ(), nativeDrainExpiryFixtureRole+"=child"),
		Profile: SerialProfile, Deadline: 5 * time.Second,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}

	terminalResult := make(chan Terminal, 1)
	go func() { terminalResult <- owned.Attempt.Wait() }()
	var terminal Terminal
	select {
	case terminal = <-terminalResult:
	case <-time.After(2 * time.Second):
		t.Fatal("drain-expiry fixture exceeded its independent two-second bound")
	}
	if _, ok = terminal.(DrainUnconfirmed); !ok {
		t.Fatalf("terminal = %#v, want DrainUnconfirmed", terminal)
	}
	if observedRoot.Load() <= 0 || observations.Load() < 2 {
		t.Fatalf("emptiness fixture observed root %d across %d samples, want one positive identity and repeated samples",
			observedRoot.Load(), observations.Load())
	}

	allowEmpty.Store(true)
	now := time.Now()
	if _, ok = supervisor.EmergencyDrain(EmergencyRequest{
		At: now, DrainBy: now.Add(time.Second),
	}).(SweepUnconfirmed); !ok {
		t.Fatal("expired local custody was rewritten instead of remaining an emergency residual")
	}
}

func cleanupNativeFixtureOutput(t *testing.T, output *os.File) {
	t.Helper()
	if output == nil {
		return
	}
	path := output.Name()
	if err := output.Close(); err != nil {
		t.Errorf("close retained native fixture output %q: %v", path, err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Errorf("remove retained native fixture output %q: %v", path, err)
	}
}

func TestNativeSupervisorCapturePreservesPartialPrefixAndDiagnostic(t *testing.T) {
	readErr := errors.New("read merged output prefix")
	executor := &supervisorNativeExecutor{
		attempts: map[attemptGeneration]*supervisorNativeAttempt{
			1: {output: &os.File{}},
		},
		outputs:     make(map[supervisorOutputRef]string),
		diagnostics: make(map[supervisorDiagnosticRef]error),
		readOutputFile: func(*os.File) (string, uint64, error) {
			return "short", 10, readErr
		},
	}
	action := supervisorAction{
		kind: supervisorCaptureOutput, generation: 1, token: 7, at: time.Unix(1, 0),
	}
	event := executor.captureOutput(action)
	if event == nil || event.output == nil {
		t.Fatal("capture returned no output completion")
	}
	completion := event.output
	if completion.ref == 0 || completion.cutoff != 10 || completion.prefixLength != 5 ||
		completion.diagnostic == 0 || executor.readOutput(completion.ref) != "short" ||
		executor.readDiagnostic(completion.diagnostic).Error() != readErr.Error() {
		t.Fatalf("partial output completion = %#v", completion)
	}
}

func TestWindowsResumeCutRequiresExactPriorCountOne(t *testing.T) {
	resumeErr := errors.New("resume failed")
	cleanupErr := errors.New("close thread handle")
	for _, test := range []struct {
		name      string
		prior     uint32
		resumeErr error
		cleanup   error
		released  bool
		wantErr   error
	}{
		{name: "exact cut", prior: 1, released: true},
		{name: "exact cut retains cleanup", prior: 1, cleanup: cleanupErr, released: true, wantErr: cleanupErr},
		{name: "already running is unknown", prior: 0, wantErr: errWindowsReleaseUnknown},
		{name: "still suspended is unknown", prior: 2, wantErr: errWindowsReleaseUnknown},
		{name: "resume failure is unknown", prior: 1, resumeErr: resumeErr, wantErr: resumeErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			released, err := windowsResumeCut(test.prior, test.resumeErr, test.cleanup)
			if released != test.released || !errors.Is(err, test.wantErr) {
				t.Fatalf("resume cut = (%v, %v), want (%v, %v)", released, err, test.released, test.wantErr)
			}
		})
	}
}

func TestWindowsLaunchResourceExhaustionRequiresExactEvidence(t *testing.T) {
	classify := func(evidence nativeLaunchFailureEvidence, code uint32) LaunchFailure {
		return classifyNativeLaunchFailureWith(evidence, func(operation nativeLaunchOperation, _ error) bool {
			return windowsLaunchResourceExhaustedCode(operation, code)
		})
	}
	for _, operation := range []nativeLaunchOperation{
		nativeLaunchInternalOutput,
		nativeLaunchLauncherStart,
		nativeLaunchContainmentPrepare,
	} {
		for _, code := range []uint32{4, 8, 14, 89, 1450, 1455} {
			evidence := nativeLaunchFailureEvidence{
				operation: operation, stage: nativeLaunchPreRelease,
				err: errors.New("windows launch boundary"), closureProven: true,
			}
			if got := classify(evidence, code); got != LaunchResourceExhausted {
				t.Fatalf("operation %d code %d classification = %v, want resource exhausted", operation, code, got)
			}
		}
	}

	for _, test := range []struct {
		name     string
		evidence nativeLaunchFailureEvidence
		code     uint32
	}{
		{
			name: "unlisted code",
			evidence: nativeLaunchFailureEvidence{
				operation: nativeLaunchLauncherStart, stage: nativeLaunchPreRelease,
				err: errors.New("access denied"), closureProven: true,
			},
			code: 5,
		},
		{
			name: "target exec operation",
			evidence: nativeLaunchFailureEvidence{
				operation: nativeLaunchTargetExec, stage: nativeLaunchPreRelease,
				err: errors.New("not enough memory"), closureProven: true,
			},
			code: 8,
		},
		{
			name: "root tracker operation",
			evidence: nativeLaunchFailureEvidence{
				operation: nativeLaunchRootTrackerCreate, stage: nativeLaunchPreRelease,
				err: errors.New("not enough memory"), closureProven: true,
			},
			code: 8,
		},
		{
			name: "release unknown stage",
			evidence: nativeLaunchFailureEvidence{
				operation: nativeLaunchContainmentPrepare, stage: nativeLaunchReleaseUnknown,
				err: errors.New("not enough memory"), closureProven: true,
			},
			code: 8,
		},
		{
			name: "closure unproven",
			evidence: nativeLaunchFailureEvidence{
				operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
				err: errors.New("not enough memory"), closureProven: false,
			},
			code: 8,
		},
		{
			name: "cleanup only",
			evidence: nativeLaunchFailureEvidence{
				operation: nativeLaunchCleanup, stage: nativeLaunchPreRelease,
				err: errors.New("not enough memory"), closureProven: true,
			},
			code: 8,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classify(test.evidence, test.code); got != LaunchFailed {
				t.Fatalf("classification = %v, want launch failed", got)
			}
		})
	}
}
