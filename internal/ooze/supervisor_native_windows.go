//go:build windows

package ooze

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsJobQueryMaximumAttempts = 32

type nativePlatformState struct{ job windows.Handle }

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nativePlatformState{}, fmt.Errorf("create managed-attempt job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{} //nolint:exhaustruct
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
	//nolint:exhaustruct // Every other process attribute deliberately retains the OS default.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}

	return nativePlatformState{job: job}, nil
}

func releaseNativeCommand(command *exec.Cmd, state nativePlatformState) error {
	processID := uint32(command.Process.Pid) //nolint:gosec // Windows process IDs are 32-bit.
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		processID,
	)
	if err != nil {
		return fmt.Errorf("open suspended process %d: %w", processID, err)
	}
	assignErr := windows.AssignProcessToJobObject(state.job, process)
	closeErr := windows.CloseHandle(process)
	if assignErr != nil || closeErr != nil {
		return errors.Join(fmt.Errorf("assign process %d to job: %w", processID, assignErr), closeErr)
	}

	return resumeNativeProcess(processID)
}

func waitNativeRootExit(nativePlatformState) error { return nil }

func confirmNativeCommandStopped(*exec.Cmd, nativePlatformState) error { return nil }

func resumeNativeProcess(processID uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot threads for process %d: %w", processID, err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))} //nolint:exhaustruct
	if err = windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("inspect first thread for process %d: %w", processID, err)
	}
	for {
		if entry.OwnerProcessID == processID {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
			if openErr != nil {
				return fmt.Errorf("open suspended thread for process %d: %w", processID, openErr)
			}
			prior, resumeErr := windows.ResumeThread(thread)
			closeErr := windows.CloseHandle(thread)
			if resumeErr != nil || prior != 1 || closeErr != nil {
				return errors.Join(
					fmt.Errorf("resume process %d from count %d: %w", processID, prior, resumeErr),
					closeErr,
				)
			}

			return nil
		}
		err = windows.Thread32Next(snapshot, &entry)
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
			return fmt.Errorf("find suspended thread for process %d: %w", processID, err)
		}
		if err != nil {
			return fmt.Errorf("inspect thread for process %d: %w", processID, err)
		}
	}
}

func nativeDomainEmpty(state nativePlatformState, _ int) (bool, error) {
	processes, err := nativeJobProcessIDs(state.job)
	if err != nil {
		return false, err
	}

	return len(processes) == 0, nil
}

func forceNativeDomain(state nativePlatformState, _ int, _ time.Time) error {
	err := windows.TerminateJobObject(state.job, 1)
	if err != nil {
		return fmt.Errorf("terminate managed-attempt job: %w", err)
	}

	return nil
}

func closeNativeDomain(state nativePlatformState) error {
	if state.job == 0 {
		return nil
	}

	return windows.CloseHandle(state.job)
}

func nativeDescendantCount(state nativePlatformState, root int) (bool, uint64, error) {
	processes, err := nativeJobProcessIDs(state.job)
	if err != nil {
		return false, 0, err
	}
	rootLive := false
	count := uint64(0)
	for _, processID := range processes {
		if processID == uint32(root) { //nolint:gosec // Windows process IDs are 32-bit.
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
		processes[index] = uint32(processID) //nolint:gosec // Windows process IDs are 32-bit.
	}

	return processes, assigned, nil
}
