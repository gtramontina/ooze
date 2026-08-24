//go:build darwin || linux

package ooze

import (
	"errors"
	"os/exec"
	"syscall"
)

type nativePlatformState struct{}

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	//nolint:exhaustruct // Every other process attribute deliberately retains the OS default.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	return nativePlatformState{}, nil
}

func releaseNativeCommand(*exec.Cmd, nativePlatformState) error { return nil }

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

func forceNativeDomain(_ nativePlatformState, processID int) error {
	err := syscall.Kill(-processID, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}

	return err
}

func closeNativeDomain(nativePlatformState) error { return nil }
