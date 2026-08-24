//go:build darwin

package ooze

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const darwinZombieProcessState = 5

type nativePlatformState struct{ domain *darwinNativeDomain }

type darwinNativeDomain struct {
	mutex   sync.Mutex
	queue   int
	tracked map[darwinProcessIdentity]struct{}
}

type darwinProcessIdentity struct {
	pid       int32
	startSec  int64
	startUsec int32
}

type darwinProcessSnapshot struct {
	identity darwinProcessIdentity
	parent   int32
	group    int32
}

func prepareNativeCommand(command *exec.Cmd) (nativePlatformState, error) {
	//nolint:exhaustruct // Every other process attribute deliberately retains the OS default.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Ptrace: true}

	return nativePlatformState{domain: &darwinNativeDomain{
		queue: -1, tracked: make(map[darwinProcessIdentity]struct{}),
	}}, nil
}

func confirmNativeCommandStopped(command *exec.Cmd, state nativePlatformState) error {
	status := syscall.WaitStatus(0)
	observed, err := syscall.Wait4(command.Process.Pid, &status, syscall.WUNTRACED, nil)
	if err != nil {
		return fmt.Errorf("confirm traced managed-attempt root stop: %w", err)
	}
	if observed != command.Process.Pid || !status.Stopped() {
		return fmt.Errorf("confirm traced managed-attempt root stop: pid=%d status=%#x", observed, uint32(status))
	}
	queue, err := unix.Kqueue()
	if err != nil {
		return nativeLaunchOperationError{
			operation: nativeLaunchRootTrackerCreate,
			err:       fmt.Errorf("create managed-attempt root tracking queue: %w", err),
		}
	}
	change := unix.Kevent_t{
		Ident: uint64(command.Process.Pid), Filter: unix.EVFILT_PROC,
		Flags: unix.EV_ADD | unix.EV_ENABLE, Fflags: unix.NOTE_EXIT,
	} //nolint:exhaustruct // The kernel event carries no user data or preset result fields.
	if _, err = unix.Kevent(queue, []unix.Kevent_t{change}, nil, nil); err != nil {
		_ = unix.Close(queue)

		return nativeLaunchOperationError{
			operation: nativeLaunchRootTrackerRegister,
			err:       fmt.Errorf("install managed-attempt root exit tracking: %w", err),
		}
	}
	state.domain.mutex.Lock()
	state.domain.queue = queue
	state.domain.mutex.Unlock()

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
	case nativeLaunchRootTrackerCreate:
		return errors.Is(err, syscall.ENOMEM) || errors.Is(err, syscall.EMFILE) ||
			errors.Is(err, syscall.ENFILE)
	case nativeLaunchRootTrackerRegister, nativeLaunchTargetExec:
		return errors.Is(err, syscall.ENOMEM)
	default:
		return false
	}
}

func waitNativeRootExit(state nativePlatformState) error {
	state.domain.mutex.Lock()
	queue := state.domain.queue
	state.domain.mutex.Unlock()
	if queue < 0 {
		return errors.New("darwin managed-attempt root tracking queue is absent")
	}
	events := make([]unix.Kevent_t, 1)
	for {
		observed, err := unix.Kevent(queue, nil, events, nil)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("await Darwin managed-attempt root exit: %w", err)
		}
		if observed != 1 || events[0].Flags&unix.EV_ERROR != 0 || events[0].Fflags&unix.NOTE_EXIT == 0 {
			return errors.New("await Darwin managed-attempt root exit: unexpected kernel event")
		}

		return nil
	}
}

func nativeDomainEmpty(state nativePlatformState, processGroup int) (bool, error) {
	processes, err := darwinProcessCensus()
	if err != nil {
		return false, err
	}
	state.domain.mutex.Lock()
	defer state.domain.mutex.Unlock()
	for _, process := range processes {
		if int(process.group) == processGroup {
			return false, nil
		}
		if _, tracked := state.domain.tracked[process.identity]; tracked {
			return false, nil
		}
	}

	return true, nil
}

func forceNativeDomain(state nativePlatformState, processGroup int, drainBy time.Time) error {
	if drainBy.IsZero() {
		return errors.New("darwin managed-attempt force lacks an absolute drain bound")
	}
	state.domain.mutex.Lock()
	defer state.domain.mutex.Unlock()
	processes, err := darwinProcessCensus()
	if err != nil {
		return err
	}
	captured := darwinReachableDomain(processes, int32(processGroup), state.domain.tracked)
	if err = signalDarwinProcessGroup(processGroup, syscall.SIGSTOP); err != nil {
		return err
	}
	for {
		for identity := range captured {
			process, present := darwinFindIdentity(processes, identity)
			if present && int(process.group) != processGroup {
				if err = signalDarwinIdentity(identity, syscall.SIGSTOP); err != nil {
					return err
				}
			}
		}
		if time.Now().After(drainBy) {
			return fmt.Errorf("freeze Darwin managed-attempt closure before %s: deadline exceeded", drainBy)
		}
		processes, err = darwinProcessCensus()
		if err != nil {
			return err
		}
		next := darwinReachableDomain(processes, int32(processGroup), captured)
		if sameDarwinIdentitySet(next, captured) {
			captured = next

			break
		}
		captured = next
	}
	if err = signalDarwinProcessGroup(processGroup, syscall.SIGKILL); err != nil {
		return err
	}
	for identity := range captured {
		process, present := darwinFindIdentity(processes, identity)
		if present && int(process.group) != processGroup {
			if err = signalDarwinIdentity(identity, syscall.SIGKILL); err != nil {
				return err
			}
		}
		state.domain.tracked[identity] = struct{}{}
	}

	return nil
}

func closeNativeDomain(state nativePlatformState) error {
	state.domain.mutex.Lock()
	defer state.domain.mutex.Unlock()
	if state.domain.queue < 0 {
		return nil
	}
	err := unix.Close(state.domain.queue)
	state.domain.queue = -1

	return err
}

func sameDarwinIdentitySet(
	left map[darwinProcessIdentity]struct{},
	right map[darwinProcessIdentity]struct{},
) bool {
	if len(left) != len(right) {
		return false
	}
	for identity := range left {
		if _, present := right[identity]; !present {
			return false
		}
	}

	return true
}

func signalDarwinProcessGroup(processGroup int, signal syscall.Signal) error {
	err := syscall.Kill(-processGroup, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("signal Darwin managed-attempt process group %d with %s: %w", processGroup, signal, err)
	}

	return nil
}

func signalDarwinIdentity(identity darwinProcessIdentity, signal syscall.Signal) error {
	processes, err := darwinProcessCensus()
	if err != nil {
		return err
	}
	if _, present := darwinFindIdentity(processes, identity); !present {
		return nil
	}
	err = syscall.Kill(int(identity.pid), signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("signal Darwin managed-attempt identity %d with %s: %w", identity.pid, signal, err)
	}

	return nil
}

func darwinFindIdentity(
	processes []darwinProcessSnapshot,
	identity darwinProcessIdentity,
) (darwinProcessSnapshot, bool) {
	for _, process := range processes {
		if process.identity == identity {
			return process, true
		}
	}

	return darwinProcessSnapshot{}, false
}

func darwinReachableDomain(
	processes []darwinProcessSnapshot,
	processGroup int32,
	prior map[darwinProcessIdentity]struct{},
) map[darwinProcessIdentity]struct{} {
	reached := make(map[darwinProcessIdentity]struct{}, len(prior)+len(processes))
	parents := make(map[int32]struct{}, len(prior)+len(processes))
	for identity := range prior {
		reached[identity] = struct{}{}
		parents[identity.pid] = struct{}{}
	}
	for _, process := range processes {
		if process.group == processGroup {
			reached[process.identity] = struct{}{}
			parents[process.identity.pid] = struct{}{}
		}
	}
	for {
		added := false
		for _, process := range processes {
			if _, parentReached := parents[process.parent]; !parentReached {
				continue
			}
			if _, alreadyReached := reached[process.identity]; alreadyReached {
				continue
			}
			reached[process.identity] = struct{}{}
			parents[process.identity.pid] = struct{}{}
			added = true
		}
		if !added {
			return reached
		}
	}
}

func darwinProcessCensus() ([]darwinProcessSnapshot, error) {
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all", 0)
	if err != nil {
		return nil, fmt.Errorf("inspect Darwin managed-attempt identities: %w", err)
	}
	processes := make([]darwinProcessSnapshot, 0, len(all))
	for _, process := range all {
		if process.Proc.P_stat == darwinZombieProcessState {
			continue
		}
		processes = append(processes, darwinProcessSnapshot{
			identity: darwinProcessIdentity{
				pid:       process.Proc.P_pid,
				startSec:  process.Proc.P_starttime.Sec,
				startUsec: process.Proc.P_starttime.Usec,
			},
			parent: process.Eproc.Ppid,
			group:  process.Eproc.Pgid,
		})
	}

	return processes, nil
}

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
