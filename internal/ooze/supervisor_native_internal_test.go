package ooze

import (
	"errors"
	"os"
	"testing"
	"time"
)

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
