//go:build linux

package ooze

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type nativePlatformState struct{}

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	//nolint:exhaustruct // Every other process attribute deliberately retains the OS default.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Ptrace: true}

	return nativePlatformState{}, nil
}

func confirmNativeCommandStopped(command *exec.Cmd, _ nativePlatformState) error {
	status := syscall.WaitStatus(0)
	observed, err := syscall.Wait4(command.Process.Pid, &status, syscall.WUNTRACED, nil)
	if err != nil {
		return fmt.Errorf("confirm traced managed-attempt root stop: %w", err)
	}
	if observed != command.Process.Pid || !status.Stopped() {
		return fmt.Errorf("confirm traced managed-attempt root stop: pid=%d status=%#x", observed, uint32(status))
	}

	return nil
}

func releaseNativeCommand(command *exec.Cmd, _ nativePlatformState) error {
	if err := syscall.PtraceDetach(command.Process.Pid); err != nil {
		return fmt.Errorf("release traced managed-attempt root: %w", err)
	}

	return nil
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

func waitNativeRootExit(nativePlatformState) error { return nil }

func nativeDomainEmpty(_ nativePlatformState, processID int) (bool, error) {
	err := syscall.Kill(-processID, 0)
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err != nil {
		return false, err
	}

	return false, nil
}

func forceNativeDomain(_ nativePlatformState, processID int, _ time.Time) error {
	err := syscall.Kill(-processID, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}

	return err
}

func closeNativeDomain(nativePlatformState) error { return nil }

func nativeDescendantCount(_ nativePlatformState, root int) (bool, uint64, error) {
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
