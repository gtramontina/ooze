//go:build darwin

package ooze

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

const darwinZombieProcessState = 5

type nativePlatformState struct{}

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	//nolint:exhaustruct // Every other process attribute deliberately retains the OS default.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	return nativePlatformState{}, nil
}

func releaseNativeCommand(*exec.Cmd, nativePlatformState) error { return nil }

func nativeDomainEmpty(_ nativePlatformState, processGroup int) (bool, error) {
	err := syscall.Kill(-processGroup, 0)
	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	processes, err := darwinDomainProcesses(processGroup)
	if err != nil {
		return false, err
	}

	return len(processes) == 0, nil
}

func forceNativeDomain(_ nativePlatformState, processGroup int) error {
	err := syscall.Kill(-processGroup, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}

	return err
}

func closeNativeDomain(nativePlatformState) error { return nil }

func nativeDescendantCount(_ nativePlatformState, root int) (bool, uint64, error) {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all", 0)
	if err != nil {
		return false, 0, fmt.Errorf("inspect Darwin processes: %w", err)
	}
	children := make(map[int32][]int32)
	rootLive := false
	for _, process := range all {
		processID := process.Proc.P_pid
		if int(processID) == root && process.Proc.P_stat != darwinZombieProcessState {
			rootLive = true
		}
		if process.Proc.P_stat != darwinZombieProcessState {
			children[process.Eproc.Ppid] = append(children[process.Eproc.Ppid], processID)
		}
	}
	count := uint64(0)
	var walk func(int32)
	walk = func(parent int32) {
		for _, child := range children[parent] {
			count++
			walk(child)
		}
	}
	walk(int32(root)) //nolint:gosec // Darwin process IDs are signed 32-bit values.

	return rootLive, count, nil
}

func darwinDomainProcesses(processGroup int) (map[int]bool, error) {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all", 0)
	if err != nil {
		return nil, fmt.Errorf("inspect Darwin process domain %d: %w", processGroup, err)
	}
	children := make(map[int32][]int32)
	seeds := make([]int32, 0)
	reached := make(map[int]bool)
	for _, process := range all {
		if process.Proc.P_stat == darwinZombieProcessState {
			continue
		}
		processID := process.Proc.P_pid
		children[process.Eproc.Ppid] = append(children[process.Eproc.Ppid], processID)
		if int(process.Eproc.Pgid) == processGroup {
			seeds = append(seeds, processID)
			reached[int(processID)] = true
		}
	}
	var walk func(int32)
	walk = func(parent int32) {
		for _, child := range children[parent] {
			if reached[int(child)] {
				continue
			}
			reached[int(child)] = true
			walk(child)
		}
	}
	for _, seed := range seeds {
		walk(seed)
	}

	return reached, nil
}
