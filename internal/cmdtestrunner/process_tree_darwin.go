//go:build darwin

package cmdtestrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	darwinLauncherModeEnvironmentVariable = "OOZE_INTERNAL_DARWIN_PROCESS_LAUNCHER"
	darwinLauncherMode                    = "v1"
	darwinLauncherStatusFD                = 4
	darwinLauncherInfrastructureExitCode  = 125
	darwinProcessTerminationTimeout       = 5 * time.Second
	darwinZombieProcessState              = 5
)

var (
	errDarwinProcessTerminationTimeout = errors.New("darwin process termination timed out")
	errDarwinCommandLaunchFailed       = errors.New("darwin command launch failed")
	errDarwinLauncherExited            = errors.New("darwin process launcher exited before suspension")
	errDarwinUnexpectedProcessEvent    = errors.New("unexpected darwin process event")
	errDarwinUnexpectedWaitStatus      = errors.New("unexpected darwin wait status")
)

type darwinLauncherWaitState uint8

const (
	darwinLauncherWaitStateUnknown darwinLauncherWaitState = iota
	darwinLauncherWaitStateStopped
	darwinLauncherWaitStateExited
)

type darwinProcessTracker struct {
	queue        int
	processGroup int
}

type darwinLauncherPipes struct {
	configurationReader *os.File
	configurationWriter *os.File
	statusReader        *os.File
	statusWriter        *os.File
}

func (p darwinLauncherPipes) close() {
	_ = p.configurationReader.Close()
	_ = p.configurationWriter.Close()
	_ = p.statusReader.Close()
	_ = p.statusWriter.Close()
}

//nolint:gochecknoinits // Re-execution must enter launcher mode before the host program's main function.
func init() {
	if os.Getenv(darwinLauncherModeEnvironmentVariable) != darwinLauncherMode {
		return
	}

	os.Exit(runDarwinLauncher())
}

func runProcessTree(command *exec.Cmd) (error, error) {
	launcher, pipes, err := newDarwinLauncher(command)
	if err != nil {
		return nil, err
	}

	err = launcher.Start()
	if err != nil {
		pipes.close()

		return nil, fmt.Errorf("start Darwin process launcher: %w", err)
	}
	_ = pipes.configurationReader.Close()
	_ = pipes.statusWriter.Close()

	configuration := processGuardianCommand{
		Path: command.Path,
		Args: command.Args,
		Dir:  command.Dir,
		Env:  environmentWithout(command.Env, darwinLauncherModeEnvironmentVariable),
	}
	configurationErr := writeDarwinLauncherConfiguration(pipes.configurationWriter, configuration)
	if configurationErr != nil {
		return nil, errors.Join(configurationErr, pipes.statusReader.Close(), stopAndWaitDarwinLauncher(launcher))
	}

	launcherState, err := waitForDarwinLauncherStop(launcher.Process.Pid)
	if launcherState == darwinLauncherWaitStateExited {
		return nil, finishExitedDarwinLauncher(launcher, pipes.statusReader, err)
	}
	if err != nil {
		_ = pipes.statusReader.Close()

		return nil, errors.Join(err, stopAndWaitDarwinLauncher(launcher))
	}

	tracker, err := newDarwinProcessTracker(launcher.Process.Pid)
	if err != nil {
		_ = pipes.statusReader.Close()

		return nil, errors.Join(err, stopAndWaitDarwinLauncher(launcher))
	}
	defer func() { _ = unix.Close(tracker.queue) }()

	err = syscall.Kill(launcher.Process.Pid, syscall.SIGCONT)
	if err != nil {
		_ = pipes.statusReader.Close()

		return nil, errors.Join(fmt.Errorf("release Darwin process launcher: %w", err), stopAndWaitDarwinLauncher(launcher))
	}

	statusErr := readDarwinLauncherStatus(pipes.statusReader)
	rootExitErr := tracker.waitForRootExit()
	terminationErr := tracker.terminateAndWait()
	commandErr, waitErr := classifyCommandWait(launcher.Wait())

	return commandErr, errors.Join(statusErr, rootExitErr, terminationErr, waitErr)
}

func finishExitedDarwinLauncher(launcher *exec.Cmd, statusReader *os.File, waitErr error) error {
	statusErr := readDarwinLauncherStatus(statusReader)
	releaseErr := launcher.Process.Release()

	return errors.Join(waitErr, statusErr, releaseErr)
}

func readDarwinLauncherStatus(reader *os.File) error {
	launcherStatus, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if len(launcherStatus) == 0 {
		return errors.Join(readErr, closeErr)
	}

	return errors.Join(
		readErr,
		closeErr,
		fmt.Errorf("%w: %s", errDarwinCommandLaunchFailed, strings.TrimSpace(string(launcherStatus))),
	)
}

func newDarwinLauncher(command *exec.Cmd) (*exec.Cmd, darwinLauncherPipes, error) {
	configurationReader, configurationWriter, err := os.Pipe()
	if err != nil {
		return nil, darwinLauncherPipes{}, fmt.Errorf("create Darwin launcher configuration pipe: %w", err)
	}
	statusReader, statusWriter, err := os.Pipe()
	if err != nil {
		_ = configurationReader.Close()
		_ = configurationWriter.Close()

		return nil, darwinLauncherPipes{}, fmt.Errorf("create Darwin launcher status pipe: %w", err)
	}
	pipes := darwinLauncherPipes{
		configurationReader: configurationReader,
		configurationWriter: configurationWriter,
		statusReader:        statusReader,
		statusWriter:        statusWriter,
	}

	executable, err := os.Executable()
	if err != nil {
		pipes.close()

		return nil, darwinLauncherPipes{}, fmt.Errorf("locate Darwin process launcher: %w", err)
	}

	launcher := exec.Command(executable) //nolint:noctx // Re-executes this trusted binary in private launcher mode.
	launcher.Env = append(
		environmentWithout(command.Env, darwinLauncherModeEnvironmentVariable),
		darwinLauncherModeEnvironmentVariable+"="+darwinLauncherMode,
	)
	launcher.ExtraFiles = []*os.File{configurationReader, statusWriter}
	launcher.Stdout = command.Stdout
	launcher.Stderr = command.Stderr
	//nolint:exhaustruct // Other process attributes retain OS defaults.
	launcher.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	return launcher, pipes, nil
}

func writeDarwinLauncherConfiguration(writer *os.File, configuration processGuardianCommand) error {
	encodeErr := json.NewEncoder(writer).Encode(configuration)
	closeErr := writer.Close()

	return errors.Join(encodeErr, closeErr)
}

func runDarwinLauncher() int {
	configurationFile := os.NewFile(processGuardianConfigurationFD, "darwin-launcher-configuration")
	statusFile := os.NewFile(darwinLauncherStatusFD, "darwin-launcher-status")
	if configurationFile == nil || statusFile == nil {
		return darwinLauncherInfrastructureExitCode
	}
	defer func() { _ = statusFile.Close() }()

	var configuration processGuardianCommand
	err := decodeProcessGuardianCommand(configurationFile, &configuration)
	if err != nil {
		_, _ = fmt.Fprintf(statusFile, "decode launcher configuration: %v", err)

		return darwinLauncherInfrastructureExitCode
	}
	unix.CloseOnExec(darwinLauncherStatusFD)

	err = syscall.Kill(os.Getpid(), syscall.SIGSTOP)
	if err != nil {
		_, _ = fmt.Fprintf(statusFile, "stop launcher before execution: %v", err)

		return darwinLauncherInfrastructureExitCode
	}

	err = syscall.Chdir(configuration.Dir)
	if err == nil {
		//nolint:gosec // The caller supplied this trusted test command.
		err = syscall.Exec(configuration.Path, configuration.Args, configuration.Env)
	}
	_, _ = fmt.Fprintf(statusFile, "%v", err)

	return darwinLauncherInfrastructureExitCode
}

func waitForDarwinLauncherStop(processID int) (darwinLauncherWaitState, error) {
	var status syscall.WaitStatus
	for {
		observedProcessID, err := syscall.Wait4(processID, &status, syscall.WUNTRACED, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return darwinLauncherWaitStateUnknown, fmt.Errorf("await Darwin launcher suspension: %w", err)
		}
		if observedProcessID != processID {
			return darwinLauncherWaitStateUnknown, fmt.Errorf(
				"await Darwin launcher suspension: %w %d",
				errDarwinUnexpectedWaitStatus,
				status,
			)
		}
		if status.Exited() || status.Signaled() {
			return darwinLauncherWaitStateExited, fmt.Errorf("%w: %d", errDarwinLauncherExited, status)
		}
		if !darwinWaitStatusIsSIGSTOP(status) {
			return darwinLauncherWaitStateUnknown, fmt.Errorf(
				"await Darwin launcher suspension: %w %d",
				errDarwinUnexpectedWaitStatus,
				status,
			)
		}

		return darwinLauncherWaitStateStopped, nil
	}
}

func newDarwinProcessTracker(processID int) (*darwinProcessTracker, error) {
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("create Darwin process tracking queue: %w", err)
	}

	change := unix.Kevent_t{
		Ident:  uint64(processID), //nolint:gosec // Darwin process IDs are non-negative integers.
		Filter: unix.EVFILT_PROC,
		Flags:  unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT,
		Fflags: unix.NOTE_EXIT,
		Data:   0,
		Udata:  nil,
	}
	_, err = unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil)
	if err != nil {
		_ = unix.Close(queue)

		return nil, fmt.Errorf("track Darwin process tree: %w", err)
	}

	return &darwinProcessTracker{
		queue:        queue,
		processGroup: processID,
	}, nil
}

func (t *darwinProcessTracker) waitForRootExit() error {
	events := make([]unix.Kevent_t, 1)
	var observed int
	var err error
	for {
		observed, err = unix.Kevent(t.queue, nil, events, nil)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("await Darwin command exit: %w", err)
	}
	if observed != 1 || events[0].Flags&unix.EV_ERROR != 0 || events[0].Fflags&unix.NOTE_EXIT == 0 {
		return fmt.Errorf("await Darwin command exit: %w", errDarwinUnexpectedProcessEvent)
	}

	return nil
}

func (t *darwinProcessTracker) terminateAndWait() error {
	deadline := time.Now().Add(darwinProcessTerminationTimeout)
	for {
		hasExecutableProcesses, err := darwinProcessGroupHasExecutableProcesses(t.processGroup)
		if err != nil {
			return err
		}
		if !hasExecutableProcesses {
			return nil
		}

		err = syscall.Kill(-t.processGroup, syscall.SIGKILL)
		if err != nil && !errors.Is(err, syscall.EPERM) && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("terminate Darwin process tree %d: %w", t.processGroup, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w after %s", errDarwinProcessTerminationTimeout, darwinProcessTerminationTimeout)
		}
		time.Sleep(time.Millisecond)
	}
}

func darwinProcessGroupHasExecutableProcesses(processGroup int) (bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", processGroup)
	if err != nil {
		return false, fmt.Errorf("inspect Darwin process group %d: %w", processGroup, err)
	}

	for _, process := range processes {
		if int(process.Proc.P_pid) == processGroup {
			continue
		}
		if process.Proc.P_stat != darwinZombieProcessState {
			return true, nil
		}
	}

	return false, nil
}

func stopAndWaitDarwinLauncher(command *exec.Cmd) error {
	killErr := command.Process.Kill()
	_, waitErr := classifyCommandWait(command.Wait())

	return errors.Join(killErr, waitErr)
}

func darwinWaitStatusIsSIGSTOP(status syscall.WaitStatus) bool {
	const (
		waitStatusMask    = 0x7f
		waitStatusStopped = 0x7f
		waitStatusShift   = 8
	)

	value := uint32(status)

	return value&waitStatusMask == waitStatusStopped && syscall.Signal(value>>waitStatusShift) == syscall.SIGSTOP
}
