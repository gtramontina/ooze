//go:build !darwin && !linux && !windows

package ooze

import (
	"os/exec"
	"time"
)

type nativePlatformState struct{}

func nativeSupervisorSupported() bool { return false }

func nativeExitStatusFromError(exitErr *exec.ExitError) ExitStatus {
	return ExitStatus{Code: exitErr.ExitCode()}
}

func prepareNativeCommand(*exec.Cmd) (nativePlatformState, error) {
	return nativePlatformState{}, ErrUnsupportedPlatform
}

func confirmNativeCommandStopped(*exec.Cmd, nativePlatformState) error {
	return ErrUnsupportedPlatform
}

func releaseNativeCommand(*exec.Cmd, nativePlatformState) (time.Time, error) {
	return time.Time{}, ErrUnsupportedPlatform
}

func nativeLaunchResourceExhausted(nativeLaunchOperation, error) bool { return false }

func nativeRootWaitBeforeRelease() bool { return false }

func nativeRootCompletion(nativePlatformState, *exec.Cmd) (ExitStatus, error) {
	return ExitStatus{}, ErrUnsupportedPlatform
}

func waitNativeRootExit(nativePlatformState) error { return ErrUnsupportedPlatform }

func nativeDomainEmpty(nativePlatformState, int) (bool, error) {
	return false, ErrUnsupportedPlatform
}

func forceNativeDomain(nativePlatformState, int, time.Time) error {
	return ErrUnsupportedPlatform
}

func closeNativeDomain(nativePlatformState) error { return ErrUnsupportedPlatform }

func nativeDescendantCount(nativePlatformState, int) (bool, uint64, error) {
	return false, 0, ErrUnsupportedPlatform
}
