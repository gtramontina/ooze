//go:build linux

package supervision

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxNativeGuardianEnvironment = "OOZE_INTERNAL_MANAGED_ATTEMPT_LINUX_GUARDIAN"
	linuxNativeGuardianMode        = "v1"
	linuxNativeConfigurationFD     = 3
	linuxNativeStatusFD            = 4
	linuxNativeControlFD           = 5
	linuxNativeInfrastructureExit  = 125
)

func nativeSupervisorSupported() bool { return true }

type linuxNativeConfiguration struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

type linuxNativeLaunchStatus struct {
	ProcessID int
	Errno     int
	Message   string
}

type linuxNativeExitStatus struct {
	Code    int
	Signal  int
	Message string
}

type linuxNativeGuardian struct {
	mutex        sync.Mutex
	decoderMutex sync.Mutex

	command       *exec.Cmd
	configuration *os.File
	statusReader  *os.File
	statusWriter  *os.File
	controlReader *os.File
	controlWriter *os.File
	decoder       *json.Decoder
	targetPID     int
	targetExit    ExitStatus

	waitOnce sync.Once
	waitErr  error
}

type nativePlatformState struct{ guardian *linuxNativeGuardian }

//nolint:gochecknoinits // Trusted re-execution must become the guardian before application main.
func init() {
	if os.Getenv(linuxNativeGuardianEnvironment) == linuxNativeGuardianMode {
		os.Exit(runLinuxNativeGuardian())
	}
}

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	configuration, err := os.CreateTemp("", "ooze-linux-guardian-configuration-*")
	if err != nil {
		return nativePlatformState{}, nativeLaunchOperationError{
			operation: nativeLaunchInternalOutput, err: fmt.Errorf("create Linux guardian configuration: %w", err),
		}
	}
	configurationPath := configuration.Name()
	_ = os.Remove(configurationPath)
	cleanupConfiguration := func() { _ = configuration.Close() }
	targetEnvironment := environmentWithoutLinuxGuardian(command.Env)
	if err = json.NewEncoder(configuration).Encode(linuxNativeConfiguration{
		Path: command.Path, Args: command.Args, Dir: command.Dir, Env: targetEnvironment,
	}); err != nil {
		cleanupConfiguration()

		return nativePlatformState{}, fmt.Errorf("encode Linux guardian configuration: %w", err)
	}
	if _, err = configuration.Seek(0, io.SeekStart); err != nil {
		cleanupConfiguration()

		return nativePlatformState{}, fmt.Errorf("rewind Linux guardian configuration: %w", err)
	}
	statusReader, statusWriter, err := os.Pipe()
	if err != nil {
		cleanupConfiguration()

		return nativePlatformState{}, nativeLaunchOperationError{
			operation: nativeLaunchInternalOutput, err: fmt.Errorf("create Linux guardian status pipe: %w", err),
		}
	}
	controlReader, controlWriter, err := os.Pipe()
	if err != nil {
		cleanupConfiguration()
		_ = statusReader.Close()
		_ = statusWriter.Close()

		return nativePlatformState{}, nativeLaunchOperationError{
			operation: nativeLaunchInternalOutput, err: fmt.Errorf("create Linux guardian control pipe: %w", err),
		}
	}
	executable, err := os.Executable()
	if err != nil {
		cleanupConfiguration()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		_ = controlReader.Close()
		_ = controlWriter.Close()

		return nativePlatformState{}, fmt.Errorf("locate Linux managed-attempt guardian: %w", err)
	}
	guardianCommand := exec.Command(executable)
	guardianCommand.Env = append(targetEnvironment, linuxNativeGuardianEnvironment+"="+linuxNativeGuardianMode)
	guardianCommand.ExtraFiles = []*os.File{configuration, statusWriter, controlReader}
	guardianCommand.Stdout = command.Stdout
	guardianCommand.Stderr = command.Stderr
	guardianCommand.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Ptrace: true}
	*command = *guardianCommand
	guardian := &linuxNativeGuardian{
		command: command, configuration: configuration,
		statusReader: statusReader, statusWriter: statusWriter,
		controlReader: controlReader, controlWriter: controlWriter,
	}
	guardian.decoder = json.NewDecoder(statusReader)

	return nativePlatformState{guardian: guardian}, nil
}

func confirmNativeCommandStopped(command *exec.Cmd, state nativePlatformState) error {
	status := syscall.WaitStatus(0)
	observed, err := syscall.Wait4(command.Process.Pid, &status, syscall.WUNTRACED, nil)
	if err != nil {
		return fmt.Errorf("confirm traced Linux guardian stop: %w", err)
	}
	if observed != command.Process.Pid || !status.Stopped() {
		return fmt.Errorf("confirm traced Linux guardian stop: pid=%d status=%#x", observed, uint32(status))
	}
	state.guardian.mutex.Lock()
	closeErr := errors.Join(
		state.guardian.configuration.Close(),
		state.guardian.statusWriter.Close(),
		state.guardian.controlReader.Close(),
	)
	state.guardian.configuration = nil
	state.guardian.statusWriter = nil
	state.guardian.controlReader = nil
	state.guardian.mutex.Unlock()
	if closeErr != nil {
		return fmt.Errorf("close Linux guardian parent file copies: %w", closeErr)
	}

	return nil
}

func releaseNativeCommand(command *exec.Cmd, state nativePlatformState) (time.Time, error) {
	if err := syscall.PtraceDetach(command.Process.Pid); err != nil {
		return time.Time{}, fmt.Errorf("release traced Linux managed-attempt guardian: %w", err)
	}
	state.guardian.mutex.Lock()
	defer state.guardian.mutex.Unlock()
	var status linuxNativeLaunchStatus
	if err := state.guardian.decoder.Decode(&status); err != nil {
		return time.Time{}, fmt.Errorf("read Linux guardian target launch status: %w", err)
	}
	if status.ProcessID <= 0 {
		launchErr := error(errors.New(status.Message))
		if status.Errno != 0 {
			launchErr = syscall.Errno(status.Errno)
		}

		state.guardian.waitOnce.Do(func() {
			state.guardian.waitErr = nativeWaitFailure(command.Wait())
		})
		return time.Time{}, nativeLaunchOperationError{
			operation: nativeLaunchTargetExec, stage: nativeLaunchPreRelease,
			closureProven: state.guardian.waitErr == nil,
			err:           fmt.Errorf("start Linux managed-attempt target: %w", launchErr),
		}
	}
	state.guardian.targetPID = status.ProcessID

	return time.Now(), nil
}

func nativeLaunchResourceExhausted(operation nativeLaunchOperation, err error) bool {
	switch operation {
	case nativeLaunchInternalOutput:
		return errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
	case nativeLaunchLauncherStart:
		return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENOMEM) ||
			errors.Is(err, syscall.EMFILE) || errors.Is(err, syscall.ENFILE)
	case nativeLaunchTargetExec:
		return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENOMEM)
	default:
		return false
	}
}

func nativeRootWaitBeforeRelease() bool { return false }

func waitNativeRootExit(state nativePlatformState) error {
	state.guardian.decoderMutex.Lock()
	defer state.guardian.decoderMutex.Unlock()
	var status linuxNativeExitStatus
	if err := state.guardian.decoder.Decode(&status); err != nil {
		return fmt.Errorf("read Linux guardian target exit status: %w", err)
	}
	if status.Message != "" {
		return errors.New(status.Message)
	}
	state.guardian.mutex.Lock()
	state.guardian.targetExit = ExitStatus{Code: status.Code, Signal: status.Signal}
	state.guardian.mutex.Unlock()

	return nil
}

func nativeRootCompletion(state nativePlatformState, _ *exec.Cmd) (ExitStatus, error) {
	state.guardian.mutex.Lock()
	defer state.guardian.mutex.Unlock()

	return state.guardian.targetExit, nil
}

func nativeDomainEmpty(state nativePlatformState, _ int) (bool, error) {
	children, err := linuxGuardianChildProcessIDs(state.guardian.command.Process.Pid)
	if err != nil {
		return false, err
	}

	return len(children) == 0, nil
}

func forceNativeDomain(state nativePlatformState, _ int, drainBy time.Time) error {
	if drainBy.IsZero() {
		return errors.New("linux managed-attempt force lacks an absolute drain bound")
	}
	state.guardian.mutex.Lock()
	targetPID := state.guardian.targetPID
	guardianPID := state.guardian.command.Process.Pid
	state.guardian.mutex.Unlock()
	if targetPID <= 0 {
		err := syscall.Kill(guardianPID, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("terminate stopped Linux guardian %d before release: %w", guardianPID, err)
		}

		return nil
	}
	for {
		children, err := linuxGuardianChildProcessIDs(state.guardian.command.Process.Pid)
		if err != nil {
			return err
		}
		if len(children) == 0 {
			return nil
		}
		for _, processID := range children {
			err = syscall.Kill(processID, syscall.SIGKILL)
			if err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("terminate Linux guardian child %d: %w", processID, err)
			}
		}
		if time.Now().After(drainBy) {
			return fmt.Errorf("terminate Linux guardian children before %s: deadline exceeded", drainBy)
		}
		time.Sleep(time.Millisecond)
	}
}

func closeNativeDomain(state nativePlatformState) error {
	if state.guardian == nil {
		return nil
	}
	if state.guardian.command.Process == nil {
		state.guardian.mutex.Lock()
		files := []*os.File{
			state.guardian.configuration, state.guardian.statusReader, state.guardian.statusWriter,
			state.guardian.controlReader, state.guardian.controlWriter,
		}
		state.guardian.configuration = nil
		state.guardian.statusReader = nil
		state.guardian.statusWriter = nil
		state.guardian.controlReader = nil
		state.guardian.controlWriter = nil
		state.guardian.mutex.Unlock()
		for _, file := range files {
			if file != nil {
				_ = file.Close()
			}
		}

		return nil
	}
	state.guardian.mutex.Lock()
	writer := state.guardian.controlWriter
	state.guardian.controlWriter = nil
	state.guardian.mutex.Unlock()
	var controlErr error
	if writer != nil {
		_, writeErr := writer.Write([]byte{1})
		controlErr = errors.Join(writeErr, writer.Close())
	}
	state.guardian.waitOnce.Do(func() {
		state.guardian.waitErr = nativeWaitFailure(state.guardian.command.Wait())
	})
	state.guardian.mutex.Lock()
	reader := state.guardian.statusReader
	state.guardian.statusReader = nil
	state.guardian.mutex.Unlock()
	var readerErr error
	if reader != nil {
		readerErr = reader.Close()
	}

	return errors.Join(controlErr, state.guardian.waitErr, readerErr)
}

func nativeDescendantCount(state nativePlatformState, _ int) (bool, uint64, error) {
	state.guardian.mutex.Lock()
	targetPID := state.guardian.targetPID
	state.guardian.mutex.Unlock()
	if targetPID <= 0 {
		return false, 0, errors.New("linux guardian target identity is absent")
	}

	return linuxDescendantCount(targetPID)
}

func runLinuxNativeGuardian() int {
	configurationFile := os.NewFile(linuxNativeConfigurationFD, "linux-managed-attempt-configuration")
	statusFile := os.NewFile(linuxNativeStatusFD, "linux-managed-attempt-status")
	controlFile := os.NewFile(linuxNativeControlFD, "linux-managed-attempt-control")
	if configurationFile == nil || statusFile == nil || controlFile == nil {
		return linuxNativeInfrastructureExit
	}
	defer func() {
		_ = configurationFile.Close()
		_ = statusFile.Close()
		_ = controlFile.Close()
	}()
	var configuration linuxNativeConfiguration
	if err := json.NewDecoder(configurationFile).Decode(&configuration); err != nil {
		_ = json.NewEncoder(statusFile).Encode(linuxNativeLaunchStatus{Message: err.Error()})

		return linuxNativeInfrastructureExit
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		_ = json.NewEncoder(statusFile).Encode(linuxNativeLaunchStatus{Message: err.Error()})

		return linuxNativeInfrastructureExit
	}
	command := exec.Command(configuration.Path, configuration.Args[1:]...)
	command.Dir = configuration.Dir
	command.Env = configuration.Env
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		status := linuxNativeLaunchStatus{Errno: linuxErrno(err), Message: err.Error()}
		_ = json.NewEncoder(statusFile).Encode(status)

		return linuxNativeInfrastructureExit
	}
	encoder := json.NewEncoder(statusFile)
	if err := encoder.Encode(linuxNativeLaunchStatus{ProcessID: command.Process.Pid}); err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()

		return linuxNativeInfrastructureExit
	}
	waitErr := command.Wait()
	exit := nativeExitStatus(waitErr)
	status := linuxNativeExitStatus{Code: exit.Code, Signal: exit.Signal}
	if failure := nativeWaitFailure(waitErr); failure != nil {
		status.Message = failure.Error()
	}
	if err := encoder.Encode(status); err != nil {
		return linuxNativeInfrastructureExit
	}

	return runLinuxNativeGuardianDrain(controlFile)
}

func runLinuxNativeGuardianDrain(control *os.File) int {
	if err := unix.SetNonblock(int(control.Fd()), true); err != nil {
		return linuxNativeInfrastructureExit
	}
	releaseRequested := false
	buffer := make([]byte, 1)
	for {
		hasChildren, err := reapLinuxGuardianChildren()
		if err != nil {
			return linuxNativeInfrastructureExit
		}
		if !releaseRequested {
			count, readErr := control.Read(buffer)
			if count != 0 || errors.Is(readErr, io.EOF) {
				releaseRequested = true
			} else if readErr != nil && !errors.Is(readErr, syscall.EAGAIN) {
				return linuxNativeInfrastructureExit
			}
		}
		if releaseRequested && !hasChildren {
			return 0
		}
		time.Sleep(time.Millisecond)
	}
}

func reapLinuxGuardianChildren() (bool, error) {
	for {
		var status syscall.WaitStatus
		processID, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.ECHILD) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if processID == 0 {
			return true, nil
		}
	}
}

func linuxGuardianChildProcessIDs(guardianPID int) ([]int, error) {
	taskRoot := filepath.Join("/proc", strconv.Itoa(guardianPID), "task")
	tasks, err := os.ReadDir(taskRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("inspect Linux guardian tasks: %w", err)
	}
	children := make(map[int]struct{})
	childrenFilesAvailable := false
	for _, task := range tasks {
		contents, readErr := os.ReadFile(filepath.Join(taskRoot, task.Name(), "children"))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("inspect Linux guardian children: %w", readErr)
		}
		childrenFilesAvailable = true
		for value := range strings.FieldsSeq(string(contents)) {
			processID, conversionErr := strconv.Atoi(value)
			if conversionErr != nil {
				return nil, fmt.Errorf("parse Linux guardian child %q: %w", value, conversionErr)
			}
			children[processID] = struct{}{}
		}
	}
	if !childrenFilesAvailable {
		return linuxDirectChildren(guardianPID)
	}
	result := make([]int, 0, len(children))
	for processID := range children {
		result = append(result, processID)
	}

	return result, nil
}

func linuxDirectChildren(parentPID int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("inspect Linux processes: %w", err)
	}
	children := make([]int, 0)
	for _, entry := range entries {
		processID, conversionErr := strconv.Atoi(entry.Name())
		if conversionErr != nil {
			continue
		}
		contents, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if errors.Is(readErr, os.ErrNotExist) || errors.Is(readErr, os.ErrPermission) {
			continue
		}
		if readErr != nil {
			return nil, readErr
		}
		for line := range strings.Lines(string(contents)) {
			value, found := strings.CutPrefix(line, "PPid:")
			if !found {
				continue
			}
			observedParent, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr != nil {
				return nil, parseErr
			}
			if observedParent == parentPID {
				children = append(children, processID)
			}
			break
		}
	}

	return children, nil
}

func linuxDescendantCount(root int) (bool, uint64, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, 0, fmt.Errorf("inspect Linux processes: %w", err)
	}
	children := make(map[int][]int)
	live := make(map[int]bool)
	for _, entry := range entries {
		processID, conversionErr := strconv.Atoi(entry.Name())
		if conversionErr != nil {
			continue
		}
		contents, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if readErr != nil {
			continue
		}
		closing := strings.LastIndexByte(string(contents), ')')
		if closing < 0 {
			continue
		}
		fields := strings.Fields(string(contents[closing+1:]))
		if len(fields) < 2 {
			continue
		}
		parent, parseErr := strconv.Atoi(fields[1])
		if parseErr != nil {
			continue
		}
		children[parent] = append(children[parent], processID)
		live[processID] = fields[0] != "Z"
	}
	count := uint64(0)
	var walk func(int)
	walk = func(parent int) {
		for _, child := range children[parent] {
			if live[child] {
				count++
			}
			walk(child)
		}
	}
	walk(root)

	return live[root], count, nil
}

func environmentWithoutLinuxGuardian(environment []string) []string {
	prefix := linuxNativeGuardianEnvironment + "="
	filtered := make([]string, 0, len(environment))
	for _, variable := range environment {
		if !strings.HasPrefix(variable, prefix) {
			filtered = append(filtered, variable)
		}
	}

	return filtered
}

func linuxErrno(err error) int {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return int(errno)
	}

	return 0
}
