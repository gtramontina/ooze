//go:build windows

package cmdtestrunner

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
	processTreeTerminationTimeout             = 5 * time.Second
	processTreeTerminationTimeoutMilliseconds = uint32(processTreeTerminationTimeout / time.Millisecond)
	jobProcessQueryMaximumAttempts            = 32
)

var (
	errProcessTreeTerminationTimeout = errors.New("process tree termination timed out")
	errUnexpectedProcessWaitStatus   = errors.New("unexpected process wait status")
	errJobProcessCapacityOverflow    = errors.New("job process capacity overflow")
	errJobProcessQueryLimit          = errors.New("job process query retry limit reached")
	errUnexpectedJobProcessList      = errors.New("unexpected job process list")
	kernel32                         = windows.NewLazySystemDLL("kernel32.dll")
	isProcessInJob                   = kernel32.NewProc("IsProcessInJob")
)

func runProcessTree(command *exec.Cmd) (error, error) {
	job, err := newProcessTreeJob()
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(job) }()

	//nolint:exhaustruct // Other process attributes retain OS defaults.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	err = command.Start()
	if err != nil {
		return nil, fmt.Errorf("start supervised command: %w", err)
	}

	processID := uint32(command.Process.Pid) //nolint:gosec // Windows process IDs are 32-bit values.
	err = assignProcessToJob(job, processID)
	if err != nil {
		cleanupErr := terminateUnsupervisedProcessAndWait(command)

		return nil, errors.Join(err, cleanupErr)
	}

	err = resumeProcess(processID)
	if err != nil {
		cleanupErr := terminateJobProcessesAndWait(command, job)

		return nil, errors.Join(err, cleanupErr)
	}

	commandErr, waitErr := classifyCommandWait(command.Wait())
	jobTerminationErr := terminateJobProcesses(job)
	supervisionErr := errors.Join(waitErr, jobTerminationErr)

	return commandErr, supervisionErr
}

func newProcessTreeJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create process tree job: %w", err)
	}

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{} //nolint:exhaustruct // Only LimitFlags is configured.
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)

		return 0, fmt.Errorf("configure process tree job: %w", err)
	}

	return job, nil
}

func assignProcessToJob(job windows.Handle, processID uint32) error {
	const access = windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE

	process, err := windows.OpenProcess(access, false, processID)
	if err != nil {
		return fmt.Errorf("open process %d for supervision: %w", processID, err)
	}
	defer func() { _ = windows.CloseHandle(process) }()

	err = windows.AssignProcessToJobObject(job, process)
	if err != nil {
		return fmt.Errorf("assign process %d to job: %w", processID, err)
	}

	return nil
}

func resumeProcess(processID uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot threads for process %d: %w", processID, err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	//nolint:exhaustruct // Thread32First populates all fields except Size.
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	err = windows.Thread32First(snapshot, &entry)
	if err != nil {
		return fmt.Errorf("inspect first thread for process %d: %w", processID, err)
	}

	for {
		if entry.OwnerProcessID == processID {
			return resumeThread(entry.ThreadID, processID)
		}

		err = windows.Thread32Next(snapshot, &entry)
		if errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
			return fmt.Errorf("find suspended thread for process %d: %w", processID, err)
		}
		if err != nil {
			return fmt.Errorf("inspect next thread for process %d: %w", processID, err)
		}
	}
}

func resumeThread(threadID, processID uint32) error {
	thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, threadID)
	if err != nil {
		return fmt.Errorf("open thread %d for process %d: %w", threadID, processID, err)
	}
	defer func() { _ = windows.CloseHandle(thread) }()

	_, err = windows.ResumeThread(thread)
	if err != nil {
		return fmt.Errorf("resume thread %d for process %d: %w", threadID, processID, err)
	}

	return nil
}

func terminateUnsupervisedProcessAndWait(command *exec.Cmd) error {
	processID := uint32(command.Process.Pid) //nolint:gosec // Windows process IDs are 32-bit values.
	killErr := command.Process.Kill()
	waitErr := waitForProcessExit(processID)
	if waitErr != nil {
		return errors.Join(killErr, waitErr)
	}

	return errors.Join(killErr, reapTerminatedCommand(command))
}

func terminateJobProcessesAndWait(command *exec.Cmd, job windows.Handle) error {
	processID := uint32(command.Process.Pid) //nolint:gosec // Windows process IDs are 32-bit values.
	terminateErr := windows.TerminateJobObject(job, 1)
	waitErr := waitForProcessExit(processID)
	if waitErr != nil {
		return errors.Join(terminateErr, waitErr)
	}

	return errors.Join(terminateErr, reapTerminatedCommand(command))
}

func waitForProcessExit(processID uint32) error {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, processID)
	if err != nil {
		return fmt.Errorf("open process %d to await termination: %w", processID, err)
	}
	defer func() { _ = windows.CloseHandle(process) }()

	status, err := windows.WaitForSingleObject(process, processTreeTerminationTimeoutMilliseconds)
	if err != nil {
		return fmt.Errorf("await process %d termination: %w", processID, err)
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("%w after %s", errProcessTreeTerminationTimeout, processTreeTerminationTimeout)
	}
	if status != uint32(windows.WAIT_OBJECT_0) {
		return fmt.Errorf("await process %d termination: %w %d", processID, errUnexpectedProcessWaitStatus, status)
	}

	return nil
}

func reapTerminatedCommand(command *exec.Cmd) error {
	_, waitErr := classifyCommandWait(command.Wait())

	return waitErr
}

func terminateJobProcesses(job windows.Handle) error {
	processes, err := retainJobProcesses(job)
	if err != nil {
		return err
	}

	err = windows.TerminateJobObject(job, 1)
	if err != nil {
		closeProcessHandles(processes)

		return fmt.Errorf("terminate process tree: %w", err)
	}
	deadline := time.Now().Add(processTreeTerminationTimeout)

	err = waitForProcessesAndClose(processes, deadline)
	if err != nil {
		return err
	}

	lateProcesses, err := retainJobProcesses(job)
	if err != nil {
		return err
	}

	return waitForProcessesAndClose(lateProcesses, deadline)
}

func retainJobProcesses(job windows.Handle) ([]windows.Handle, error) {
	processIDs, err := jobProcessIDs(job)
	if err != nil {
		return nil, err
	}

	processes := make([]windows.Handle, 0, len(processIDs))
	for _, processID := range processIDs {
		const access = windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.SYNCHRONIZE
		process, openErr := windows.OpenProcess(access, false, processID)
		if errors.Is(openErr, windows.ERROR_INVALID_PARAMETER) {
			continue
		}
		if openErr != nil {
			closeProcessHandles(processes)

			return nil, fmt.Errorf("retain job process %d: %w", processID, openErr)
		}

		belongsToJob, membershipErr := processBelongsToJob(process, job)
		if membershipErr != nil {
			_ = windows.CloseHandle(process)
			closeProcessHandles(processes)

			return nil, membershipErr
		}
		if !belongsToJob {
			_ = windows.CloseHandle(process)

			continue
		}

		processes = append(processes, process)
	}

	return processes, nil
}

func processBelongsToJob(process, job windows.Handle) (bool, error) {
	var belongsToJob int32
	succeeded, _, callErr := isProcessInJob.Call(
		uintptr(process),
		uintptr(job),
		uintptr(unsafe.Pointer(&belongsToJob)),
	)
	if succeeded == 0 {
		return false, fmt.Errorf("validate retained process job membership: %w", callErr)
	}

	return belongsToJob != 0, nil
}

func jobProcessIDs(job windows.Handle) ([]uint32, error) {
	const initialProcessCapacity = 16

	processCapacity := uint32(initialProcessCapacity)
	for range jobProcessQueryMaximumAttempts {
		processIDs, assignedProcesses, err := queryJobProcessIDs(job, processCapacity)
		if err == nil {
			return processIDs, nil
		}
		if !errors.Is(err, windows.ERROR_MORE_DATA) {
			return nil, fmt.Errorf("inspect process tree: %w", err)
		}

		processCapacity, err = nextJobProcessCapacity(processCapacity, assignedProcesses)
		if err != nil {
			return nil, fmt.Errorf("inspect process tree: %w", err)
		}
	}

	return nil, fmt.Errorf("inspect process tree: %w", errJobProcessQueryLimit)
}

func nextJobProcessCapacity(current, assigned uint32) (uint32, error) {
	if assigned > current {
		return assigned, nil
	}
	if current > ^uint32(0)/2 {
		return 0, errJobProcessCapacityOverflow
	}

	return current * 2, nil
}

func queryJobProcessIDs(job windows.Handle, processCapacity uint32) ([]uint32, uint32, error) {
	const headerSize = unsafe.Sizeof(uint32(0)) * 2
	const processIDSize = unsafe.Sizeof(uintptr(0))

	bufferSize := headerSize + uintptr(processCapacity)*processIDSize
	if bufferSize > uintptr(^uint32(0)) {
		return nil, 0, fmt.Errorf("query job process IDs: %w", errJobProcessCapacityOverflow)
	}
	buffer := make([]uintptr, (bufferSize+processIDSize-1)/processIDSize)
	bufferPointer := unsafe.Pointer(&buffer[0])
	err := windows.QueryInformationJobObject(
		job,
		windows.JobObjectBasicProcessIdList,
		uintptr(bufferPointer),
		uint32(bufferSize),
		nil,
	)
	if err != nil {
		err = fmt.Errorf("query job process IDs: %w", err)
	}

	assignedProcesses := *(*uint32)(bufferPointer)
	listedProcesses := *(*uint32)(unsafe.Add(bufferPointer, unsafe.Sizeof(uint32(0))))
	if err != nil {
		return nil, assignedProcesses, err
	}
	if listedProcesses > processCapacity {
		return nil, assignedProcesses, fmt.Errorf(
			"query job process IDs: %w: listed %d processes in a %d-process buffer",
			errUnexpectedJobProcessList,
			listedProcesses,
			processCapacity,
		)
	}
	processIDValues := unsafe.Slice((*uintptr)(unsafe.Add(bufferPointer, headerSize)), listedProcesses)
	processIDs := make([]uint32, 0, listedProcesses)
	for _, processID := range processIDValues {
		processIDs = append(processIDs, uint32(processID)) //nolint:gosec // Windows process IDs are 32-bit values.
	}

	return processIDs, assignedProcesses, err
}

func waitForProcessHandle(process windows.Handle, deadline time.Time) error {
	timeout := processWaitTimeoutMilliseconds(time.Until(deadline))
	status, err := windows.WaitForSingleObject(process, timeout)
	if err != nil {
		return fmt.Errorf("await process termination: %w", err)
	}
	if status == uint32(windows.WAIT_TIMEOUT) {
		return fmt.Errorf("%w after %s", errProcessTreeTerminationTimeout, processTreeTerminationTimeout)
	}
	if status != uint32(windows.WAIT_OBJECT_0) {
		return fmt.Errorf("await process termination: %w %d", errUnexpectedProcessWaitStatus, status)
	}

	return nil
}

func processWaitTimeoutMilliseconds(remaining time.Duration) uint32 {
	if remaining <= 0 {
		return 0
	}
	if remaining >= processTreeTerminationTimeout {
		return processTreeTerminationTimeoutMilliseconds
	}

	return uint32((remaining + time.Millisecond - 1) / time.Millisecond) //nolint:gosec // Capped above at five seconds.
}

func waitForProcessesAndClose(processes []windows.Handle, deadline time.Time) error {
	var waitErr error
	for _, process := range processes {
		waitErr = errors.Join(waitErr, waitForProcessHandle(process, deadline))
	}
	closeProcessHandles(processes)

	return waitErr
}

func closeProcessHandles(processes []windows.Handle) {
	for _, process := range processes {
		_ = windows.CloseHandle(process)
	}
}
