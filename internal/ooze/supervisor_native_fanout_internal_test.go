package ooze

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	nativeFanoutFixtureRole = "OOZE_NATIVE_FANOUT_FIXTURE_ROLE"
	nativeFanoutFixtureDir  = "OOZE_NATIVE_FANOUT_FIXTURE_DIR"
	nativeFanoutFixtureLeaf = "OOZE_NATIVE_FANOUT_FIXTURE_LEAF"
	nativeFanoutBreadth     = 16
)

func TestNativeSupervisorDrainsWideFanout(t *testing.T) {
	switch os.Getenv(nativeFanoutFixtureRole) {
	case "root":
		runNativeFanoutRoot(t)
		return
	case "leaf":
		runNativeFanoutLeaf(t)
		return
	}
	if !nativeSupervisorSupported() {
		t.Skip("native supervision is unavailable on this operating system")
	}

	directory := t.TempDir()
	shell := newProcessRuntimeShell(1)
	campaign := shell.registerCampaign(campaignProvenance{lineage: 503})
	requested := shell.requestAdmission(admissionRequest{
		campaign: campaign.token, attempt: "native-wide-fanout", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return shell.startCommitted(grant, startInstallation{grant: grant, cell: cell}).start
		},
		driver,
	)
	launched := supervisor.Launch(Spec{
		Attempt: "native-wide-fanout",
		Command: []string{os.Args[0], "-test.run=^TestNativeSupervisorDrainsWideFanout$"},
		Dir:     directory,
		Env: append(os.Environ(),
			nativeFanoutFixtureRole+"=root",
			nativeFanoutFixtureDir+"="+directory,
		),
		Profile: SerialProfile, Deadline: 15 * time.Second,
	})
	owned, ok := launched.(Owned)
	if !ok || owned.Attempt == nil {
		t.Fatalf("launch = %#v, want Owned", launched)
	}
	terminal := owned.Attempt.Wait()
	if _, ok = terminal.(Settled); !ok {
		t.Fatalf("terminal = %#v, want Settled", terminal)
	}

	root := readNativeFanoutIdentity(t, filepath.Join(directory, "root"))
	pids := make(map[int]struct{}, nativeFanoutBreadth)
	before := make([]int64, nativeFanoutBreadth)
	for index := range nativeFanoutBreadth {
		pid, parent := readNativeFanoutPair(t, filepath.Join(directory, fmt.Sprintf("ready-%02d", index)))
		if parent != root {
			t.Fatalf("leaf %d parent = %d, want exact root %d", pid, parent, root)
		}
		if _, duplicate := pids[pid]; duplicate {
			t.Fatalf("fanout reused process identity %d", pid)
		}
		pids[pid] = struct{}{}
		process, findErr := os.FindProcess(pid)
		if findErr == nil {
			t.Cleanup(func() { _ = process.Kill() })
		}
		before[index] = nativeFanoutMarkerSize(t, directory, index)
	}
	time.Sleep(100 * time.Millisecond)
	for index := range nativeFanoutBreadth {
		if after := nativeFanoutMarkerSize(t, directory, index); after != before[index] {
			t.Fatalf("leaf %d marker grew from %d to %d after drainage", index, before[index], after)
		}
	}
}

func runNativeFanoutRoot(t *testing.T) {
	t.Helper()
	directory := os.Getenv(nativeFanoutFixtureDir)
	if err := os.WriteFile(filepath.Join(directory, "root"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	for index := range nativeFanoutBreadth {
		command := exec.Command(os.Args[0], "-test.run=^TestNativeSupervisorDrainsWideFanout$")
		command.Env = append(os.Environ(),
			nativeFanoutFixtureRole+"=leaf",
			nativeFanoutFixtureLeaf+"="+strconv.Itoa(index),
		)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for index := range nativeFanoutBreadth {
		awaitNativeFanoutFile(t, filepath.Join(directory, fmt.Sprintf("ready-%02d", index)), deadline)
	}
}

func runNativeFanoutLeaf(t *testing.T) {
	t.Helper()
	directory := os.Getenv(nativeFanoutFixtureDir)
	index, err := strconv.Atoi(os.Getenv(nativeFanoutFixtureLeaf))
	if err != nil {
		t.Fatal(err)
	}
	identity := fmt.Sprintf("%d %d", os.Getpid(), os.Getppid())
	if err = os.WriteFile(filepath.Join(directory, fmt.Sprintf("ready-%02d", index)), []byte(identity), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(directory, fmt.Sprintf("marker-%02d", index))
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		file, openErr := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if openErr != nil {
			t.Fatal(openErr)
		}
		_, writeErr := file.WriteString("x")
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			t.Fatal(writeErr, closeErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func awaitNativeFanoutFile(t *testing.T, path string, deadline time.Time) {
	t.Helper()
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("fanout fixture did not create %s", path)
}

func readNativeFanoutIdentity(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || identity <= 0 {
		t.Fatalf("invalid process identity %q: %v", contents, err)
	}
	return identity
}

func readNativeFanoutPair(t *testing.T, path string) (int, int) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(contents))
	if len(fields) != 2 {
		t.Fatalf("invalid fanout identity pair %q", contents)
	}
	pid, pidErr := strconv.Atoi(fields[0])
	parent, parentErr := strconv.Atoi(fields[1])
	if pidErr != nil || parentErr != nil || pid <= 0 || parent <= 0 {
		t.Fatalf("invalid fanout identities %q: %v/%v", contents, pidErr, parentErr)
	}
	return pid, parent
}

func nativeFanoutMarkerSize(t *testing.T, directory string, index int) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(directory, fmt.Sprintf("marker-%02d", index)))
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
