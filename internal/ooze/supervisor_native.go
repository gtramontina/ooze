package ooze

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const supervisorNativeOperation = "execute native supervisor action"

type supervisorNativeAttempt struct {
	spec       Spec
	command    *exec.Cmd
	output     *os.File
	platform   nativePlatformState
	releasedAt time.Time
	waitOnce   sync.Once
	waitAt     time.Time
	waitErr    error
	exit       ExitStatus
}

type supervisorNativeExecutor struct {
	mutex      sync.Mutex
	drainEpoch time.Duration
	attempts   map[attemptGeneration]*supervisorNativeAttempt
	outputs    map[supervisorOutputRef]string
	nextOutput supervisorOutputRef
}

func newNativeSupervisorDriver(
	runtime *processRuntimeShell,
	launchProgress time.Duration,
	drainEpoch time.Duration,
) *supervisorDriver {
	executor := &supervisorNativeExecutor{
		drainEpoch: drainEpoch,
		attempts:   make(map[attemptGeneration]*supervisorNativeAttempt),
		outputs:    make(map[supervisorOutputRef]string),
	}

	return newSupervisorDriver(supervisorDriverConstruction{
		runtime: runtime, now: time.Now, launchProgress: launchProgress, drainEpoch: drainEpoch,
		prepare: executor.prepare, execute: executor.execute, readOutput: executor.readOutput,
	})
}

func (executor *supervisorNativeExecutor) prepare(generation attemptGeneration, spec Spec) {
	executor.mutex.Lock()
	defer executor.mutex.Unlock()
	if generation == 0 || executor.attempts[generation] != nil {
		invariant(supervisorNativeOperation, "native attempt preparation is zero or duplicated")
	}
	executor.attempts[generation] = &supervisorNativeAttempt{spec: spec}
}

func (executor *supervisorNativeExecutor) execute(action supervisorAction) *supervisorEvent {
	switch action.kind {
	case supervisorLaunchNative:
		return executor.launch(action)
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
	output, err := os.CreateTemp("", "ooze-managed-output-*")
	if err != nil {
		executor.mutex.Unlock()

		return nativeNotReleasedEvent(action, time.Now(), LaunchFailed)
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

		return nativeNotReleasedEvent(action, time.Now(), classifyNativeLaunchFailure(platformErr))
	}
	attempt.command = command
	attempt.output = output
	attempt.platform = platform
	executor.mutex.Unlock()

	err = command.Start()
	at := time.Now()
	if err != nil {
		_ = closeNativeDomain(platform)
		_ = output.Close()
		_ = os.Remove(output.Name())

		return nativeNotReleasedEvent(action, at, classifyNativeLaunchFailure(err))
	}
	if err = releaseNativeCommand(command, platform); err != nil {
		_ = forceNativeDomain(platform, command.Process.Pid)
		_, _ = command.Process.Wait()
		_ = closeNativeDomain(platform)
		_ = output.Close()
		_ = os.Remove(output.Name())

		return nativeNotReleasedEvent(action, time.Now(), classifyNativeLaunchFailure(err))
	}
	executor.mutex.Lock()
	attempt.releasedAt = at
	executor.mutex.Unlock()
	completion := supervisorLaunchCompletion{
		generation: action.generation, action: action.token,
		at: at, kind: supervisorLaunchReleased,
	}

	return &supervisorEvent{
		kind: supervisorLaunchCompleted, generation: action.generation,
		at: at, completion: &completion,
	}
}

func nativeNotReleasedEvent(action supervisorAction, at time.Time, failure LaunchFailure) *supervisorEvent {
	completion := supervisorLaunchCompletion{
		generation: action.generation, action: action.token,
		at: at, kind: supervisorLaunchProvenNotReleased, failure: failure,
	}

	return &supervisorEvent{
		kind: supervisorLaunchCompleted, generation: action.generation,
		at: at, completion: &completion,
	}
}

func classifyNativeLaunchFailure(err error) LaunchFailure {
	if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENOMEM) ||
		errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE) {
		return LaunchResourceExhausted
	}

	return LaunchFailed
}

func (executor *supervisorNativeExecutor) waitRoot(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	attempt.waitOnce.Do(func() {
		attempt.waitErr = attempt.command.Wait()
		attempt.waitAt = time.Now()
		attempt.exit = nativeExitStatus(attempt.waitErr)
	})
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

func nativeExitStatus(err error) ExitStatus {
	if err == nil {
		return ExitStatus{}
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return ExitStatus{}
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return ExitStatus{Code: exitErr.ExitCode()}
	}
	if status.Signaled() {
		return ExitStatus{Signal: int(status.Signal())}
	}

	return ExitStatus{Code: status.ExitStatus()}
}

func (executor *supervisorNativeExecutor) observeEmpty(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	at := time.Now()
	empty, err := nativeDomainEmpty(attempt.platform, attempt.command.Process.Pid)
	kind := supervisorDrainObservedResidual
	diagnostic := supervisorDiagnosticRef(0)
	if err != nil {
		kind = supervisorDrainObservationFailed
		diagnostic = 1
	} else if empty {
		kind = supervisorDrainObservedEmpty
	}

	return nativeDrainEvent(action, at, kind, diagnostic)
}

func (executor *supervisorNativeExecutor) force(action supervisorAction) *supervisorEvent {
	executor.mutex.Lock()
	attempt := executor.requireAttempt(action.generation)
	executor.mutex.Unlock()
	err := forceNativeDomain(attempt.platform, attempt.command.Process.Pid)
	attempt.waitOnce.Do(func() {
		attempt.waitErr = attempt.command.Wait()
		attempt.waitAt = time.Now()
		attempt.exit = nativeExitStatus(attempt.waitErr)
	})
	diagnostic := supervisorDiagnosticRef(0)
	if err != nil {
		diagnostic = 1
	}

	return nativeDrainEvent(action, time.Now(), supervisorDrainForceCompleted, diagnostic)
}

func nativeDrainEvent(
	action supervisorAction,
	at time.Time,
	kind supervisorDrainCompletionKind,
	diagnostic supervisorDiagnosticRef,
) *supervisorEvent {
	completion := supervisorDrainCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at, kind: kind, diagnostic: diagnostic,
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
	contents, err := readNativeOutput(attempt.output)
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
		diagnostic = 1
	}
	completion := supervisorOutputCompletion{
		generation: action.generation,
		action:     supervisorPendingAction{kind: action.kind, token: action.token},
		at:         at, ref: ref, cutoff: uint64(len(contents)), prefixLength: uint64(len(contents)),
		diagnostic: diagnostic,
	}

	return &supervisorEvent{
		kind: supervisorOutputCompleted, generation: action.generation,
		at: at, output: &completion,
	}
}

func readNativeOutput(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	contents, err := io.ReadAll(file)

	return string(contents), err
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
		diagnostic = 1
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

func (executor *supervisorNativeExecutor) requireAttempt(
	generation attemptGeneration,
) *supervisorNativeAttempt {
	attempt := executor.attempts[generation]
	if generation == 0 || attempt == nil {
		invariant(supervisorNativeOperation, "attempt generation is stale or unknown")
	}

	return attempt
}
