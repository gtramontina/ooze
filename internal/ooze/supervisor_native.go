package ooze

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

const supervisorNativeOperation = "execute native supervisor action"

var errNativeLaunchReleaseRevoked = errors.New("managed-attempt release was revoked before target execution")
var errWindowsReleaseUnknown = errors.New("windows managed-attempt release is unknown")

type nativeLaunchOperation uint8

const (
	nativeLaunchInternalOutput nativeLaunchOperation = iota + 1
	nativeLaunchLauncherStart
	nativeLaunchContainmentPrepare
	nativeLaunchRootTrackerCreate
	nativeLaunchRootTrackerRegister
	nativeLaunchTargetExec
	nativeLaunchCleanup
)

type nativeLaunchStage uint8

const (
	nativeLaunchPreRelease nativeLaunchStage = iota + 1
	nativeLaunchReleaseUnknown
)

type nativeLaunchFailureEvidence struct {
	operation     nativeLaunchOperation
	stage         nativeLaunchStage
	err           error
	closureProven bool
}

type nativeLaunchOperationError struct {
	operation     nativeLaunchOperation
	stage         nativeLaunchStage
	closureProven bool
	err           error
}

func (failure nativeLaunchOperationError) Error() string { return failure.err.Error() }
func (failure nativeLaunchOperationError) Unwrap() error { return failure.err }

type supervisorNativeAttempt struct {
	spec           Spec
	command        *exec.Cmd
	output         *os.File
	platform       nativePlatformState
	releasedAt     time.Time
	releaseRevoked bool
	waitOnce       sync.Once
	waitDone       chan struct{}
	waitAt         time.Time
	trackingErr    error
	waitErr        error
	exit           ExitStatus
}

type supervisorNativeExecutor struct {
	mutex            sync.Mutex
	drainEpoch       time.Duration
	attempts         map[attemptGeneration]*supervisorNativeAttempt
	outputs          map[supervisorOutputRef]string
	nextOutput       supervisorOutputRef
	diagnostics      map[supervisorDiagnosticRef]error
	nextDiagnostic   supervisorDiagnosticRef
	createOutputFile func() (*os.File, error)
	readOutputFile   func(*os.File) (string, uint64, error)
	forceDomain      func(nativePlatformState, int, time.Time) error
	domainEmpty      func(nativePlatformState, int) (bool, error)
}

func newNativeSupervisorDriver(
	runtime *processRuntimeShell,
	launchProgress time.Duration,
	drainEpoch time.Duration,
) (*supervisorDriver, error) {
	if !nativeSupervisorSupported() {
		return nil, ErrUnsupportedPlatform
	}
	executor := &supervisorNativeExecutor{
		drainEpoch:  drainEpoch,
		attempts:    make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:     make(map[supervisorOutputRef]string),
		diagnostics: make(map[supervisorDiagnosticRef]error),
		createOutputFile: func() (*os.File, error) {
			return os.CreateTemp("", "ooze-managed-output-*")
		},
		readOutputFile: readNativeOutput,
	}

	return newSupervisorDriver(supervisorDriverConstruction{
		runtime: runtime, now: time.Now, launchProgress: launchProgress, drainEpoch: drainEpoch,
		prepare: executor.prepare, execute: executor.execute,
		recheckRoot: executor.recheckRoot, sampleRunning: executor.sampleRunning,
		readOutput: executor.readOutput, readDiagnostic: executor.readDiagnostic,
		recordDiagnostic: executor.recordDiagnostic,
	}), nil
}

func (executor *supervisorNativeExecutor) prepare(generation attemptGeneration, spec Spec) {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	if generation == 0 || executor.attempts[generation] != nil {
		invariant(supervisorNativeOperation, "native attempt preparation is zero or duplicated")
	}
	executor.attempts[generation] = &supervisorNativeAttempt{spec: spec, waitDone: make(chan struct{})}
}

func (executor *supervisorNativeExecutor) execute(action supervisorAction) *supervisorEvent {
	switch action.kind {
	case supervisorLaunchNative:
		return executor.launch(action)
	case supervisorRevokeLaunchRelease:
		return executor.revokeLaunchRelease(action)
	case supervisorWaitRoot:
		return executor.waitRoot(action)
	case supervisorObserveEmptiness:
		return executor.observeEmpty(action)
	case supervisorForceOwned:
		return executor.force(action)
	case supervisorCaptureOutput:
		return executor.captureOutput(action)
	case supervisorReleaseDomain:
		return executor.release(action)
	default:
		invariant(supervisorNativeOperation, "action kind is not native")

		return nil
	}
}

func (executor *supervisorNativeExecutor) launch(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	if attempt.command != nil || attempt.output != nil {
		executor.mutex.Unlock()
		invariant(supervisorNativeOperation, "native launch was duplicated")
	}
	createOutputFile := executor.createOutputFile
	if createOutputFile == nil {
		createOutputFile = func() (*os.File, error) {
			return os.CreateTemp("", "ooze-managed-output-*")
		}
	}
	output, err := createOutputFile()
	if err != nil {
		executor.mutex.Unlock()

		return executor.notReleased(action, time.Now(), classifyNativeLaunchFailure(
			nativeLaunchFailureEvidence{
				operation: nativeLaunchInternalOutput, stage: nativeLaunchPreRelease,
				err: err, closureProven: true,
			},
		), err)
	}
	command := exec.Command(attempt.spec.Command[0], attempt.spec.Command[1:]...) //nolint:gosec,noctx
	command.Dir = attempt.spec.Dir
	command.Env = attempt.spec.Env
	command.Stdout = output
	command.Stderr = output
	platform, platformErr := prepareNativeCommand(command)
	if platformErr != nil {
		_ = output.Close()
		_ = os.Remove(output.Name())
		executor.mutex.Unlock()

		return executor.notReleased(action, time.Now(), classifyNativeLaunchFailure(
			nativeLaunchFailureEvidence{
				operation: nativeLaunchFailureOperation(platformErr, nativeLaunchContainmentPrepare),
				stage:     nativeLaunchPreRelease, err: platformErr, closureProven: true,
			},
		), platformErr)
	}
	attempt.command = command
	attempt.output = output
	attempt.platform = platform
	executor.mutex.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = command.Start()
	startFailedAt := time.Now()
	if err != nil {
		closeErr := closeNativeDomain(platform)
		_ = output.Close()
		_ = os.Remove(output.Name())

		return executor.notReleased(action, startFailedAt, classifyNativeLaunchFailure(
			nativeLaunchFailureEvidence{
				operation: nativeLaunchLauncherStart, stage: nativeLaunchPreRelease,
				err: err, closureProven: closeErr == nil,
			},
		), errors.Join(err, closeErr))
	}
	if err = confirmNativeCommandStopped(command, platform); err != nil {
		forceErr := forceNativeDomain(platform, command.Process.Pid, time.Now().Add(executor.drainEpoch))
		if forceErr != nil {
			return executor.releaseUnconfirmed(action, errors.Join(err, forceErr))
		}
		_, waitErr := command.Process.Wait()
		closeErr := closeNativeDomain(platform)
		_ = output.Close()
		_ = os.Remove(output.Name())

		cleanupErr := errors.Join(forceErr, nativeWaitFailure(waitErr), closeErr)
		return executor.notReleased(action, time.Now(), classifyNativeLaunchFailure(
			nativeLaunchFailureEvidence{
				operation: nativeLaunchFailureOperation(err, nativeLaunchContainmentPrepare),
				stage:     nativeLaunchPreRelease, err: err,
				closureProven: cleanupErr == nil,
			},
		), errors.Join(err, cleanupErr))
	}
	rootWaitStarted := nativeRootWaitBeforeRelease()
	if rootWaitStarted {
		go executor.awaitRoot(attempt)
	}
	executor.mutex.Lock()
	if attempt.releaseRevoked {
		executor.mutex.Unlock()
		forceErr := forceNativeDomain(platform, command.Process.Pid, time.Now().Add(executor.drainEpoch))
		if forceErr != nil {
			return executor.releaseUnconfirmed(
				action, errors.Join(errNativeLaunchReleaseRevoked, forceErr),
			)
		}
		if !rootWaitStarted {
			go executor.awaitRoot(attempt)
		}
		<-attempt.waitDone
		cleanupErr := errors.Join(
			attempt.trackingErr, nativeWaitFailure(attempt.waitErr), closeNativeDomain(platform),
			output.Close(), os.Remove(output.Name()),
		)

		return executor.notReleased(
			action, time.Now(), LaunchFailed, errors.Join(errNativeLaunchReleaseRevoked, cleanupErr),
		)
	}
	releasedAt, err := releaseNativeCommand(command, platform)
	if err == nil {
		attempt.releasedAt = releasedAt
	}
	executor.mutex.Unlock()
	if errors.Is(err, errWindowsReleaseUnknown) {
		completion := supervisorLaunchCompletion{
			generation: action.generation, action: action.token,
			at: time.Now(), kind: supervisorLaunchReleaseUnconfirmed,
			diagnostic: executor.recordDiagnostic(err),
		}

		return &supervisorEvent{
			kind: supervisorLaunchCompleted, generation: action.generation,
			at: completion.at, drainBy: completion.at.Add(executor.drainEpoch), completion: &completion,
		}
	}
	if err != nil {
		forceErr := forceNativeDomain(platform, command.Process.Pid, time.Now().Add(executor.drainEpoch))
		if !rootWaitStarted {
			go executor.awaitRoot(attempt)
		}
		if forceErr != nil {
			return executor.releaseUnconfirmed(action, errors.Join(err, forceErr))
		}
		<-attempt.waitDone
		closeErr := closeNativeDomain(platform)
		_ = output.Close()
		_ = os.Remove(output.Name())

		evidence := nativeLaunchFailureEvidence{
			operation: nativeLaunchFailureOperation(err, nativeLaunchCleanup),
			stage:     nativeLaunchReleaseUnknown, err: err, closureProven: false,
		}
		cleanupErr := errors.Join(forceErr, attempt.trackingErr, nativeWaitFailure(attempt.waitErr), closeErr)
		var operationError nativeLaunchOperationError
		if errors.As(err, &operationError) && operationError.stage != 0 {
			evidence.stage = operationError.stage
			evidence.closureProven = operationError.closureProven || cleanupErr == nil
		}

		return executor.notReleased(
			action, time.Now(), classifyNativeLaunchFailure(evidence), errors.Join(err, cleanupErr),
		)
	}
	if !rootWaitStarted {
		go executor.awaitRoot(attempt)
	}
	completion := supervisorLaunchCompletion{
		generation: action.generation, action: action.token,
		at: releasedAt, kind: supervisorLaunchReleased,
	}

	return &supervisorEvent{
		kind: supervisorLaunchCompleted, generation: action.generation,
		at: releasedAt, completion: &completion,
	}
}

func (executor *supervisorNativeExecutor) releaseUnconfirmed(
	action supervisorAction,
	err error,
) *supervisorEvent {
	at := time.Now()
	completion := supervisorLaunchCompletion{
		generation: action.generation, action: action.token, at: at,
		kind: supervisorLaunchReleaseUnconfirmed, diagnostic: executor.recordDiagnostic(err),
	}

	return &supervisorEvent{
		kind: supervisorLaunchCompleted, generation: action.generation,
		at: at, drainBy: at.Add(executor.drainEpoch), completion: &completion,
	}
}

func (executor *supervisorNativeExecutor) revokeLaunchRelease(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	attempt := executor.requireAttempt(action.generation)
	if attempt.releasedAt.IsZero() {
		attempt.releaseRevoked = true
	}

	return nil
}

func (executor *supervisorNativeExecutor) notReleased(
	action supervisorAction,
	at time.Time,
	failure LaunchFailure,
	err error,
) *supervisorEvent {
	if err == nil {
		invariant(supervisorNativeOperation, "native no-release result lacks its primary failure")
	}
	completion := supervisorLaunchCompletion{
		generation: action.generation, action: action.token,
		at: at, kind: supervisorLaunchProvenNotReleased, failure: failure,
		diagnostic: executor.recordDiagnostic(err),
	}

	return &supervisorEvent{
		kind: supervisorLaunchCompleted, generation: action.generation,
		at: at, completion: &completion,
	}
}

func classifyNativeLaunchFailure(evidence nativeLaunchFailureEvidence) LaunchFailure {
	return classifyNativeLaunchFailureWith(evidence, nativeLaunchResourceExhausted)
}

func classifyNativeLaunchFailureWith(
	evidence nativeLaunchFailureEvidence,
	resourceExhausted func(nativeLaunchOperation, error) bool,
) LaunchFailure {
	if evidence.err != nil && evidence.stage == nativeLaunchPreRelease && evidence.closureProven &&
		resourceExhausted(evidence.operation, evidence.err) {
		return LaunchResourceExhausted
	}

	return LaunchFailed
}

func windowsLaunchResourceExhaustedCode(operation nativeLaunchOperation, code uint32) bool {
	switch operation {
	case nativeLaunchInternalOutput, nativeLaunchLauncherStart, nativeLaunchContainmentPrepare:
	default:
		return false
	}
	for _, exhausted := range []uint32{4, 8, 14, 89, 1450, 1455} {
		if code == exhausted {
			return true
		}
	}

	return false
}

func nativeLaunchFailureOperation(err error, fallback nativeLaunchOperation) nativeLaunchOperation {
	var operationError nativeLaunchOperationError
	if errors.As(err, &operationError) {
		return operationError.operation
	}

	return fallback
}

func windowsResumeCut(prior uint32, resumeErr error, cleanupErr error) (bool, error) {
	if resumeErr != nil {
		return false, errors.Join(errWindowsReleaseUnknown, resumeErr, cleanupErr)
	}
	if prior != 1 {
		return false, errors.Join(
			errWindowsReleaseUnknown,
			fmt.Errorf("resume thread returned prior suspension count %d", prior),
			cleanupErr,
		)
	}

	return true, cleanupErr
}

func (executor *supervisorNativeExecutor) waitRoot(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	executor.awaitRoot(attempt)
	if err := errors.Join(attempt.trackingErr, nativeWaitFailure(attempt.waitErr)); err != nil {
		fact := supervisorRunningFact{
			generation: action.generation, action: action.token,
			kind: supervisorRunningObservationFailed, at: attempt.waitAt,
			source: supervisorObservationWait, diagnostic: executor.recordDiagnostic(err),
		}

		return &supervisorEvent{
			kind: supervisorRunningObserved, generation: action.generation,
			at: attempt.waitAt, drainBy: attempt.waitAt.Add(executor.drainEpoch),
			running: &supervisorRunningBundle{
				generation: action.generation, waitAction: action.token,
				facts: []supervisorRunningFact{fact},
			},
		}
	}
	fact := supervisorRunningFact{
		generation: action.generation, action: action.token,
		kind: supervisorRunningRootExited, at: attempt.waitAt,
		exitCode: attempt.exit.Code, exitSignal: attempt.exit.Signal,
	}

	return &supervisorEvent{
		kind: supervisorRunningObserved, generation: action.generation,
		at: attempt.waitAt, drainBy: attempt.waitAt.Add(executor.drainEpoch),
		running: &supervisorRunningBundle{
			generation: action.generation, waitAction: action.token,
			facts: []supervisorRunningFact{fact},
		},
	}
}

func nativeWaitFailure(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}

	return fmt.Errorf("wait for managed-attempt root: %w", err)
}

func (executor *supervisorNativeExecutor) awaitRoot(attempt *supervisorNativeAttempt) {
	attempt.waitOnce.Do(func() {
		attempt.trackingErr = waitNativeRootExit(attempt.platform)
		attempt.exit, attempt.waitErr = nativeRootCompletion(attempt.platform, attempt.command)
		attempt.waitAt = time.Now()
		close(attempt.waitDone)
	})
}

func nativeExitStatus(err error) ExitStatus {
	if err == nil {
		return ExitStatus{}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ExitStatus{}
	}
	return nativeExitStatusFromError(exitErr)
}

func (executor *supervisorNativeExecutor) observeEmpty(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	domainEmpty := executor.domainEmpty
	if domainEmpty == nil {
		domainEmpty = nativeDomainEmpty
	}
	empty, err := domainEmpty(attempt.platform, attempt.command.Process.Pid)
	if err == nil && empty {
		select {
		case <-attempt.waitDone:
		case <-time.After(time.Until(action.drainBy)):
		}
	}
	at := time.Now()
	kind := supervisorDrainObservedResidual
	diagnostic := supervisorDiagnosticRef(0)
	if err != nil {
		kind = supervisorDrainObservationFailed
		diagnostic = executor.recordDiagnostic(err)
	} else if empty {
		kind = supervisorDrainObservedEmpty
	}

	return nativeDrainEvent(
		action, at, kind, executor.completedWaitDiagnostic(attempt), diagnostic,
	)
}

func (executor *supervisorNativeExecutor) force(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	forceDomain := executor.forceDomain
	if forceDomain == nil {
		forceDomain = forceNativeDomain
	}
	controlErr := forceDomain(attempt.platform, attempt.command.Process.Pid, action.drainBy)
	waitCompleted := false
	if controlErr == nil {
		executor.awaitRoot(attempt)
		waitCompleted = true
	} else {
		select {
		case <-attempt.waitDone:
			waitCompleted = true
		default:
		}
	}
	waitDiagnostic := supervisorDiagnosticRef(0)
	if waitCompleted {
		waitDiagnostic = executor.completedWaitDiagnostic(attempt)
	}
	controlDiagnostic := supervisorDiagnosticRef(0)
	if controlErr != nil {
		controlDiagnostic = executor.recordDiagnostic(controlErr)
	}

	return nativeDrainEvent(
		action, time.Now(), supervisorDrainForceCompleted, waitDiagnostic, controlDiagnostic,
	)
}

func (executor *supervisorNativeExecutor) completedWaitDiagnostic(
	attempt *supervisorNativeAttempt,
) supervisorDiagnosticRef {
	select {
	case <-attempt.waitDone:
	default:
		return 0
	}
	waitErr := errors.Join(attempt.trackingErr, nativeWaitFailure(attempt.waitErr))
	if waitErr == nil {
		return 0
	}

	return executor.recordDiagnostic(waitErr)
}

func (executor *supervisorNativeExecutor) recheckRoot(
	generation attemptGeneration,
) (ExitStatus, time.Time, bool, error) {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(generation)
	executor.mutex.Unlock()
	select {
	case <-attempt.waitDone:
		return attempt.exit, attempt.waitAt, true, nil
	default:
		return ExitStatus{}, time.Time{}, false, nil
	}
}

func (executor *supervisorNativeExecutor) sampleRunning(
	generation attemptGeneration,
) (bool, uint64, error) {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(generation)
	executor.mutex.Unlock()

	return nativeDescendantCount(attempt.platform, attempt.command.Process.Pid)
}

func nativeDrainEvent(
	action supervisorAction,
	at time.Time,
	kind supervisorDrainCompletionKind,
	waitDiagnostic supervisorDiagnosticRef,
	diagnostic supervisorDiagnosticRef,
) *supervisorEvent {
	completion := supervisorDrainCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at, kind: kind, waitDiagnostic: waitDiagnostic, diagnostic: diagnostic,
	}

	return &supervisorEvent{
		kind: supervisorDrainCompleted, generation: action.generation,
		at: at, drain: &completion,
	}
}

func (executor *supervisorNativeExecutor) captureOutput(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	at := time.Now()
	readOutputFile := executor.readOutputFile
	if readOutputFile == nil {
		readOutputFile = readNativeOutput
	}
	contents, cutoff, err := readOutputFile(attempt.output)
	executor.mutex.Lock()
	executor.nextOutput++
	if executor.nextOutput == 0 {
		executor.mutex.Unlock()
		invariant(supervisorNativeOperation, "output reference exhausted")
	}
	ref := executor.nextOutput
	executor.outputs[ref] = contents
	executor.mutex.Unlock()
	diagnostic := supervisorDiagnosticRef(0)
	if err != nil {
		diagnostic = executor.recordDiagnostic(err)
	}
	completion := supervisorOutputCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at, ref: ref, cutoff: cutoff, prefixLength: uint64(len(contents)),
		waitDiagnostic: executor.completedWaitDiagnostic(attempt), diagnostic: diagnostic,
	}

	return &supervisorEvent{
		kind: supervisorOutputCompleted, generation: action.generation,
		at: at, output: &completion,
	}
}

func readNativeOutput(file *os.File) (string, uint64, error) {
	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if info.Size() < 0 {
		return "", 0, errors.New("merged output size is negative")
	}
	cutoff := uint64(info.Size())
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", cutoff, err
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, int64(cutoff)))

	return string(contents), cutoff, readErr
}

func (executor *supervisorNativeExecutor) release(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	err := errors.Join(
		attempt.output.Close(),
		os.Remove(attempt.output.Name()),
		closeNativeDomain(attempt.platform),
	)
	diagnostic := supervisorDiagnosticRef(0)
	if err != nil {
		diagnostic = executor.recordDiagnostic(err)
	}
	at := time.Now()
	completion := supervisorReleaseCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at, diagnostic: diagnostic,
	}

	return &supervisorEvent{
		kind: supervisorReleaseCompleted, generation: action.generation,
		at: at, release: &completion,
	}
}

func (executor *supervisorNativeExecutor) readOutput(ref supervisorOutputRef) string {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	output, ok := executor.outputs[ref]
	if !ok {
		invariant(supervisorNativeOperation, fmt.Sprintf("output reference %d is unknown", ref))
	}

	return output
}

func (executor *supervisorNativeExecutor) recordDiagnostic(err error) supervisorDiagnosticRef {
	if err == nil {
		invariant(supervisorNativeOperation, "cannot register a nil diagnostic")
	}
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	executor.nextDiagnostic++
	if executor.nextDiagnostic == 0 {
		invariant(supervisorNativeOperation, "diagnostic reference exhausted")
	}
	ref := executor.nextDiagnostic
	executor.diagnostics[ref] = errors.New(err.Error())

	return ref
}

func (executor *supervisorNativeExecutor) readDiagnostic(ref supervisorDiagnosticRef) error {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	err := executor.diagnostics[ref]
	if ref == 0 || err == nil {
		invariant(supervisorNativeOperation, "diagnostic reference is zero or unknown")
	}

	return err
}

func (executor *supervisorNativeExecutor) requireAttempt(
	generation attemptGeneration,
) *supervisorNativeAttempt {
	attempt := executor.attempts[generation]
	if generation == 0 || attempt == nil {
		invariant(supervisorNativeOperation, "attempt generation is stale or unknown")
	}

	return attempt
}
