//go:build windows

package supervision

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsJobQueryMaximumAttempts = 32
	windowsStillActive             = 259
)

func nativeSupervisorSupported() bool { return true }

type nativePlatformState struct {
	job    windows.Handle
	shared *windowsNativeState
}

type windowsNativeState struct {
	releaseCleanup error
	root           windows.Handle
	rootUntracked  bool
}

func nativeExitStatusFromError(exitErr *exec.ExitError) ExitStatus {
	return ExitStatus{Code: exitErr.ExitCode()}
}

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nativePlatformState{}, fmt.Errorf("create managed-attempt job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)

		return nativePlatformState{}, fmt.Errorf("configure managed-attempt job: %w", err)
	}
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}

	return nativePlatformState{job: job, shared: &windowsNativeState{}}, nil
}

func releaseNativeCommand(command *exec.Cmd, state nativePlatformState) (time.Time, error) {
	processID := uint32(command.Process.Pid)
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		processID,
	)
	if err != nil {
		state.shared.rootUntracked = true
		return time.Time{}, nativeLaunchOperationError{
			operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
			err: fmt.Errorf("open suspended process %d: %w", processID, err),
		}
	}
	state.shared.root = process
	assignErr := windows.AssignProcessToJobObject(state.job, process)
	if assignErr != nil {
		return time.Time{}, nativeLaunchOperationError{
			operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
			err: fmt.Errorf("assign process %d to job: %w", processID, assignErr),
		}
	}

	released, resumeErr := resumeNativeProcess(processID)
	if !released {
		return time.Time{}, resumeErr
	}
	if resumeErr != nil {
		state.shared.releaseCleanup = errors.Join(state.shared.releaseCleanup, resumeErr)
	}

	return time.Now(), nil
}

func nativeLaunchResourceExhausted(operation nativeLaunchOperation, err error) bool {
	for _, code := range []syscall.Errno{4, 8, 14, 89, 1450, 1455} {
		if errors.Is(err, code) {
			return windowsLaunchResourceExhaustedCode(operation, uint32(code))
		}
	}

	return false
}

func nativeRootWaitBeforeRelease() bool { return true }

func nativeRootCompletion(_ nativePlatformState, command *exec.Cmd) (ExitStatus, error) {
	err := command.Wait()

	return nativeExitStatus(err), err
}

func waitNativeRootExit(nativePlatformState) error { return nil }

func confirmNativeCommandStopped(*exec.Cmd, nativePlatformState) error { return nil }

func resumeNativeProcess(processID uint32) (released bool, resultErr error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return false, nativeLaunchOperationError{
			operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
			err: fmt.Errorf("snapshot threads for process %d: %w", processID, err),
		}
	}
	defer func() { resultErr = errors.Join(resultErr, windows.CloseHandle(snapshot)) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err = windows.Thread32First(snapshot, &entry); err != nil {
		return false, nativeLaunchOperationError{
			operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
			err: fmt.Errorf("inspect first thread for process %d: %w", processID, err),
		}
	}
	for {
		if entry.OwnerProcessID == processID {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return false, nativeLaunchOperationError{
					operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
					err: fmt.Errorf("open suspended thread for process %d: %w", processID, openErr),
				}
			}
			prior, resumeErr := windows.ResumeThread(thread)
			closeErr := windows.CloseHandle(thread)

			return windowsResumeCut(prior, resumeErr, closeErr)
		}
		err = windows.Thread32Next(snapshot, &entry)
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
			return false, nativeLaunchOperationError{
				operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
				err: fmt.Errorf("find suspended thread for process %d: %w", processID, err),
			}
		}
		if err != nil {
			return false, nativeLaunchOperationError{
				operation: nativeLaunchContainmentPrepare, stage: nativeLaunchPreRelease,
				err: fmt.Errorf("inspect thread for process %d: %w", processID, err),
			}
		}
	}
}

func nativeDomainEmpty(state nativePlatformState, _ int) (bool, error) {
	processes, err := nativeJobProcessIDs(state.job)
	if err != nil {
		return false, err
	}
	if state.shared.rootUntracked {
		return false, nil
	}
	if state.shared.root != 0 {
		var exitCode uint32
		if err = windows.GetExitCodeProcess(state.shared.root, &exitCode); err != nil {
			return false, fmt.Errorf("inspect suspended managed-attempt root: %w", err)
		}
		if exitCode == windowsStillActive {
			return false, nil
		}
	}

	return len(processes) == 0, nil
}

func forceNativeDomain(state nativePlatformState, root int, _ time.Time) error {
	var rootErr error
	if state.shared.rootUntracked {
		rootErr = errors.New("suspended managed-attempt root identity is unavailable")
	} else if state.shared.root != 0 {
		var exitCode uint32
		rootErr = windows.GetExitCodeProcess(state.shared.root, &exitCode)
		if rootErr == nil && exitCode == windowsStillActive {
			rootErr = windows.TerminateProcess(state.shared.root, 1)
		}
		if rootErr != nil {
			rootErr = fmt.Errorf("terminate exact suspended managed-attempt root %d: %w", root, rootErr)
		}
	}
	jobErr := windows.TerminateJobObject(state.job, 1)
	if jobErr != nil {
		jobErr = fmt.Errorf("terminate managed-attempt job: %w", jobErr)
	}

	return errors.Join(rootErr, jobErr)
}

func closeNativeDomain(state nativePlatformState) error {
	if state.job == 0 {
		return nil
	}

	var rootErr error
	if state.shared.root != 0 {
		rootErr = windows.CloseHandle(state.shared.root)
		state.shared.root = 0
	}

	return errors.Join(state.shared.releaseCleanup, rootErr, windows.CloseHandle(state.job))
}

func nativeDescendantCount(state nativePlatformState, root int) (bool, uint64, error) {
	processes, err := nativeJobProcessIDs(state.job)
	if err != nil {
		return false, 0, err
	}
	rootLive := false
	count := uint64(0)
	for _, processID := range processes {
		if processID == uint32(root) {
			rootLive = true
		} else {
			count++
		}
	}

	return rootLive, count, nil
}

func nativeJobProcessIDs(job windows.Handle) ([]uint32, error) {
	capacity := uint32(16)
	for range windowsJobQueryMaximumAttempts {
		processes, assigned, err := queryNativeJobProcessIDs(job, capacity)
		if err == nil {
			return processes, nil
		}
		if !errors.Is(err, windows.ERROR_MORE_DATA) {
			return nil, fmt.Errorf("inspect managed-attempt job: %w", err)
		}
		if assigned > capacity {
			capacity = assigned
		} else if capacity > ^uint32(0)/2 {
			return nil, errors.New("managed-attempt job process capacity overflow")
		} else {
			capacity *= 2
		}
	}

	return nil, errors.New("managed-attempt job process query did not converge")
}

func queryNativeJobProcessIDs(
	job windows.Handle,
	capacity uint32,
) ([]uint32, uint32, error) {
	headerSize := unsafe.Sizeof(uint32(0)) * 2
	processIDSize := unsafe.Sizeof(uintptr(0))
	bufferSize := headerSize + uintptr(capacity)*processIDSize
	if bufferSize > uintptr(^uint32(0)) {
		return nil, 0, errors.New("managed-attempt job process buffer overflow")
	}
	buffer := make([]uintptr, (bufferSize+processIDSize-1)/processIDSize)
	pointer := unsafe.Pointer(&buffer[0])
	err := windows.QueryInformationJobObject(
		job,
		windows.JobObjectBasicProcessIdList,
		uintptr(pointer),
		uint32(bufferSize),
		nil,
	)
	assigned := *(*uint32)(pointer)
	listed := *(*uint32)(unsafe.Add(pointer, unsafe.Sizeof(uint32(0))))
	if err != nil {
		return nil, assigned, err
	}
	if listed > capacity {
		return nil, assigned, errors.New("managed-attempt job returned an oversized process list")
	}
	values := unsafe.Slice((*uintptr)(unsafe.Add(pointer, headerSize)), listed)
	processes := make([]uint32, len(values))
	for index, processID := range values {
		processes[index] = uint32(processID)
	}

	return processes, assigned, nil
}
