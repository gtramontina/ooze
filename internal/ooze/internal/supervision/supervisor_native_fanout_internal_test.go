package supervision

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	campaign := registerCampaignForTest(shell, campaignProvenance{lineage: 503})
	requested := requestAdmissionForTest(shell, admissionRequest{
		campaign: campaign.token, attempt: "native-wide-fanout", class: serialPrimaryAdmission,
	})
	grant := <-requested.delivery
	driver := newNativeSupervisorDriverForTest(t, shell, time.Second, 5*time.Second)
	supervisor := newDrivenSupervisorForTest(
		func(_ attemptIdentity, cell *pendingStartCell) installedStart {
			return startCommittedForTest(shell, grant, startInstallation{grant: grant, cell: cell}).start
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
	require.True(t, ok, "launch = %#v, want Owned", launched)
	require.NotNil(t, owned.Attempt, "launch = %#v, want Owned", launched)
	terminal := owned.Attempt.Wait()
	{
		_, ok = terminal.(Settled)
		require.True(t, ok, "terminal = %#v, want Settled", terminal)
	}

	root := readNativeFanoutIdentity(t, filepath.Join(directory, "root"))
	pids := make(map[int]struct{}, nativeFanoutBreadth)
	before := make([]int64, nativeFanoutBreadth)
	for index := range nativeFanoutBreadth {
		pid, parent := readNativeFanoutPair(t, filepath.Join(directory, fmt.Sprintf("ready-%02d", index)))
		assert.Equal(t, root, parent, "leaf %d parent = %d, want exact root %d", pid, parent, root)
		{
			_, duplicate := pids[pid]
			assert.False(t, duplicate, "fanout reused process identity %d", pid)
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
		{
			after := nativeFanoutMarkerSize(t, directory, index)
			assert.Equal(t, before[index], after, "leaf %d marker grew from %d to %d after drainage", index, before[index], after)
		}
	}
}

func runNativeFanoutRoot(t *testing.T) {
	t.Helper()
	directory := os.Getenv(nativeFanoutFixtureDir)
	{
		err := os.WriteFile(filepath.Join(directory, "root"), []byte(strconv.Itoa(os.Getpid())), 0o600)
		require.NoError(t, err)
	}
	for index := range nativeFanoutBreadth {
		command := exec.Command(os.Args[0], "-test.run=^TestNativeSupervisorDrainsWideFanout$")
		command.Env = append(os.Environ(),
			nativeFanoutFixtureRole+"=leaf",
			nativeFanoutFixtureLeaf+"="+strconv.Itoa(index),
		)
		{
			err := command.Start()
			require.NoError(t, err)
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
	require.NoError(t, err)
	identity := fmt.Sprintf("%d %d", os.Getpid(), os.Getppid())
	{
		err = os.WriteFile(filepath.Join(directory, fmt.Sprintf("ready-%02d", index)), []byte(identity), 0o600)
		require.NoError(t, err)
	}
	marker := filepath.Join(directory, fmt.Sprintf("marker-%02d", index))
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		file, openErr := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		require.NoError(t, openErr)
		_, writeErr := file.WriteString("x")
		closeErr := file.Close()
		require.NoError(t, writeErr, "write fanout fixture marker; close error=%v", closeErr)
		assert.NoError(t, closeErr, "close fanout fixture marker")
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
	require.FailNowf(t, "fanout fixture did not create its file", "path: %s", path)
}

func readNativeFanoutIdentity(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	identity, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	require.NoError(t, err, "invalid process identity %q: %v", contents, err)
	assert.False(t, identity <= 0, "invalid process identity %q: %v", contents, err)
	return identity
}

func readNativeFanoutPair(t *testing.T, path string) (int, int) {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	fields := strings.Fields(string(contents))
	require.EqualValues(t, 2, len(fields), "invalid fanout identity pair %q", contents)
	pid, pidErr := strconv.Atoi(fields[0])
	parent, parentErr := strconv.Atoi(fields[1])
	require.NoError(t, pidErr, "invalid fanout identities %q: %v/%v", contents, pidErr, parentErr)
	require.NoError(t, parentErr, "invalid fanout identities %q: %v/%v", contents, pidErr, parentErr)
	require.Positive(t, pid, "invalid fanout identities %q: %v/%v", contents, pidErr, parentErr)
	require.Positive(t, parent, "invalid fanout identities %q: %v/%v", contents, pidErr, parentErr)
	return pid, parent
}

func nativeFanoutMarkerSize(t *testing.T, directory string, index int) int64 {
	t.Helper()
	info, err := os.Stat(filepath.Join(directory, fmt.Sprintf("marker-%02d", index)))
	require.NoError(t, err)
	return info.Size()
}
