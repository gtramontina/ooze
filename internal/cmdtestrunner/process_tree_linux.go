//go:build linux

package cmdtestrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxGuardianModeEnvironmentVariable = "OOZE_INTERNAL_LINUX_PROCESS_GUARDIAN"
	linuxGuardianMode                    = "v1"
	linuxGuardianCommandFailedExitCode   = 1
	linuxGuardianInfrastructureExitCode  = 125
	linuxGuardianTerminationTimeout      = 5 * time.Second
)

var (
	errLinuxGuardianTerminationTimeout = errors.New("linux guardian termination timed out")
	errLinuxParentProcessIDMissing     = errors.New("linux parent process ID is missing")
)

//nolint:gochecknoinits // Re-execution must enter guardian mode before the host program's main function.
func init() {
	if os.Getenv(linuxGuardianModeEnvironmentVariable) != linuxGuardianMode {
		return
	}

	os.Exit(runLinuxGuardian())
}

func runProcessTree(command *exec.Cmd) (error, error) {
	configurationReader, configurationWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create Linux guardian configuration pipe: %w", err)
	}
	defer func() { _ = configurationReader.Close() }()

	executable, err := os.Executable()
	if err != nil {
		_ = configurationWriter.Close()

		return nil, fmt.Errorf("locate Linux process guardian: %w", err)
	}

	guardian := exec.Command(executable) //nolint:noctx // Re-executes this trusted binary in private guardian mode.
	guardian.Env = environmentWithout(command.Env, linuxGuardianModeEnvironmentVariable)
	guardian.Env = append(guardian.Env, linuxGuardianModeEnvironmentVariable+"="+linuxGuardianMode)
	guardian.ExtraFiles = []*os.File{configurationReader}
	guardian.Stdout = command.Stdout
	guardian.Stderr = command.Stderr

	err = guardian.Start()
	if err != nil {
		_ = configurationWriter.Close()

		return nil, fmt.Errorf("start Linux process guardian: %w", err)
	}
	_ = configurationReader.Close()

	configuration := processGuardianCommand{
		Path: command.Path,
		Args: command.Args,
		Dir:  command.Dir,
		Env:  environmentWithout(command.Env, linuxGuardianModeEnvironmentVariable),
	}
	encodeErr := json.NewEncoder(configurationWriter).Encode(configuration)
	closeErr := configurationWriter.Close()
	if encodeErr != nil || closeErr != nil {
		killErr := guardian.Process.Kill()
		_, waitErr := classifyCommandWait(guardian.Wait())

		return nil, errors.Join(
			fmt.Errorf("configure Linux process guardian: %w", errors.Join(encodeErr, closeErr)),
			killErr,
			waitErr,
		)
	}

	return classifyLinuxGuardianWait(guardian.Wait())
}

func runLinuxGuardian() int {
	configurationFile := os.NewFile(processGuardianConfigurationFD, "linux-guardian-configuration")
	if configurationFile == nil {
		return linuxGuardianInfrastructureExitCode
	}

	var configuration processGuardianCommand
	err := decodeProcessGuardianCommand(configurationFile, &configuration)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "decode Linux guardian configuration: %v\n", err)

		return linuxGuardianInfrastructureExitCode
	}

	err = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "configure Linux child subreaper: %v\n", err)

		return linuxGuardianInfrastructureExitCode
	}

	//nolint:gosec,noctx // The caller supplied this trusted test command; the guardian has no cancellation context.
	command := exec.Command(configuration.Path, configuration.Args[1:]...)
	command.Dir = configuration.Dir
	command.Env = configuration.Env
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	//nolint:exhaustruct // Other process attributes retain OS defaults.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err = command.Start()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "start supervised command: %v\n", err)

		return linuxGuardianInfrastructureExitCode
	}

	commandFailed, waitErr := guardianCommandFailed(command.Wait())
	terminationErr := terminateLinuxGuardianDescendants()
	if waitErr != nil || terminationErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "supervise command: %v\n", errors.Join(waitErr, terminationErr))

		return linuxGuardianInfrastructureExitCode
	}
	if commandFailed {
		return linuxGuardianCommandFailedExitCode
	}

	return 0
}

func guardianCommandFailed(waitErr error) (bool, error) {
	if waitErr == nil {
		return false, nil
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return true, nil
	}

	return false, fmt.Errorf("wait for supervised command: %w", waitErr)
}

func terminateLinuxGuardianDescendants() error {
	deadline := time.Now().Add(linuxGuardianTerminationTimeout)
	for {
		hasChildren, err := reapExitedGuardianChildren()
		if err != nil {
			return err
		}
		if !hasChildren {
			return nil
		}

		childProcessIDs, err := linuxGuardianChildProcessIDs()
		if err != nil {
			return err
		}
		for _, processID := range childProcessIDs {
			err = syscall.Kill(processID, syscall.SIGKILL)
			if err != nil && !errors.Is(err, syscall.ESRCH) {
				return fmt.Errorf("terminate adopted process %d: %w", processID, err)
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf(
				"terminate adopted processes: %w after %s",
				errLinuxGuardianTerminationTimeout,
				linuxGuardianTerminationTimeout,
			)
		}
		time.Sleep(time.Millisecond)
	}
}

func reapExitedGuardianChildren() (bool, error) {
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
			return false, fmt.Errorf("reap adopted process: %w", err)
		}
		if processID == 0 {
			return true, nil
		}
	}
}

func linuxGuardianChildProcessIDs() ([]int, error) {
	return linuxGuardianChildProcessIDsAt("/proc/self/task", "/proc", os.Getpid())
}

func linuxGuardianChildProcessIDsAt(taskRoot, processRoot string, guardianProcessID int) ([]int, error) {
	processIDs, childrenFilesAvailable, err := linuxGuardianTaskChildProcessIDs(taskRoot)
	if err != nil {
		return nil, err
	}
	if childrenFilesAvailable {
		return processIDs, nil
	}

	return linuxGuardianDirectChildProcessIDs(processRoot, guardianProcessID)
}

func linuxGuardianTaskChildProcessIDs(taskRoot string) ([]int, bool, error) {
	taskDirectories, err := os.ReadDir(taskRoot)
	if err != nil {
		return nil, false, fmt.Errorf("inspect Linux guardian tasks: %w", err)
	}

	childrenFilesAvailable := false
	processIDs := make(map[int]struct{})
	for _, taskDirectory := range taskDirectories {
		childrenPath := filepath.Join(taskRoot, taskDirectory.Name(), "children")
		contents, readErr := os.ReadFile(childrenPath)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return nil, false, fmt.Errorf("inspect Linux guardian children: %w", readErr)
		}
		childrenFilesAvailable = true

		for value := range strings.FieldsSeq(string(contents)) {
			processID, conversionErr := strconv.Atoi(value)
			if conversionErr != nil {
				return nil, false, fmt.Errorf("parse Linux guardian child process ID %q: %w", value, conversionErr)
			}
			processIDs[processID] = struct{}{}
		}
	}

	result := make([]int, 0, len(processIDs))
	for processID := range processIDs {
		result = append(result, processID)
	}

	return result, childrenFilesAvailable, nil
}

func linuxGuardianDirectChildProcessIDs(processRoot string, guardianProcessID int) ([]int, error) {
	processDirectories, err := os.ReadDir(processRoot)
	if err != nil {
		return nil, fmt.Errorf("inspect Linux processes: %w", err)
	}

	processIDs := make([]int, 0)
	for _, processDirectory := range processDirectories {
		processID, conversionErr := strconv.Atoi(processDirectory.Name())
		if conversionErr != nil {
			continue
		}

		statusPath := filepath.Join(processRoot, processDirectory.Name(), "status")
		contents, readErr := os.ReadFile(statusPath)
		if linuxProcessStatusIsUninspectable(readErr) {
			continue
		}
		if readErr != nil {
			return nil, fmt.Errorf("inspect Linux process %d: %w", processID, readErr)
		}

		parentProcessID, parseErr := linuxParentProcessID(contents)
		if errors.Is(parseErr, errLinuxParentProcessIDMissing) {
			continue
		}
		if parseErr != nil {
			return nil, fmt.Errorf("inspect Linux process %d: %w", processID, parseErr)
		}
		if parentProcessID == guardianProcessID {
			processIDs = append(processIDs, processID)
		}
	}

	return processIDs, nil
}

func linuxProcessStatusIsUninspectable(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ESRCH)
}

func linuxParentProcessID(status []byte) (int, error) {
	const parentProcessIDLabel = "PPid:"

	for line := range strings.Lines(string(status)) {
		value, found := strings.CutPrefix(line, parentProcessIDLabel)
		if !found {
			continue
		}

		parentProcessID, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0, fmt.Errorf("parse parent process ID %q: %w", strings.TrimSpace(value), err)
		}

		return parentProcessID, nil
	}

	return 0, errLinuxParentProcessIDMissing
}

func classifyLinuxGuardianWait(waitErr error) (error, error) {
	if waitErr == nil {
		return nil, nil //nolint:nilnil // A successful command belongs to neither error category.
	}

	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == linuxGuardianCommandFailedExitCode {
		return fmt.Errorf("test command exited unsuccessfully: %w", waitErr), nil
	}

	return nil, fmt.Errorf("linux process guardian failed: %w", waitErr)
}
