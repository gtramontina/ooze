//go:build darwin

package supervision

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const (
	darwinCensusFixtureRole   = "OOZE_DARWIN_CENSUS_FIXTURE_ROLE"
	darwinCensusFixtureShape  = "OOZE_DARWIN_CENSUS_FIXTURE_SHAPE"
	darwinLimitFixtureRole    = "OOZE_DARWIN_LIMIT_FIXTURE_ROLE"
	darwinLimitFixtureShape   = "OOZE_DARWIN_LIMIT_FIXTURE_SHAPE"
	darwinLimitFixtureRelease = "OOZE_DARWIN_LIMIT_FIXTURE_RELEASE"
)

func TestDarwinManagedCensusInstrumentsPerDescendantShape(t *testing.T) {
	if os.Getenv(darwinCensusFixtureRole) == "setsid" {
		{
			_, err := syscall.Setsid()
			require.NoError(t, err)
		}
		{
			err := os.WriteFile(os.Getenv(darwinCensusFixtureShape),
				[]byte(strconv.Itoa(os.Getpid())), 0o600)
			require.NoError(t, err)
		}
		time.Sleep(30 * time.Second)

		return
	}
	setsidPlant := darwinCensusFixtureRole + "=setsid " + darwinCensusFixtureShape +
		"=%[1]s/shape '" + strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") +
		"' -test.run=^TestDarwinManagedCensusInstrumentsPerDescendantShape$ &\n"
	for _, shape := range []struct {
		name, plant, parent                   string
		group, root, managed, rejectConflated [2]bool
	}{
		{
			name: "plain child", plant: "/bin/sleep 30 &\necho $! > %[1]s/shape\n", parent: "root",
			group: [2]bool{true, true}, root: [2]bool{true, false}, managed: [2]bool{true, true},
			rejectConflated: [2]bool{false, true},
		},
		{
			name: "double-forked orphan", plant: "/bin/sh -c '/bin/sleep 30 & echo $! > %[1]s/shape'\n",
			parent: "init", group: [2]bool{true, true}, root: [2]bool{false, false}, managed: [2]bool{true, true},
			rejectConflated: [2]bool{true, true},
		},
		{
			name: "direct-root escapee", plant: "set -m\n/bin/sleep 30 &\necho $! > %[1]s/shape\nset +m\n",
			parent: "root", group: [2]bool{false, false}, root: [2]bool{true, false}, managed: [2]bool{true, false},
			rejectConflated: [2]bool{true, false},
		},
		{
			name: "setsid escapee", plant: setsidPlant,
			parent: "root", group: [2]bool{false, false}, root: [2]bool{true, false}, managed: [2]bool{true, false},
			rejectConflated: [2]bool{true, false},
		},
		{
			name:   "grandchild with middle alive",
			plant:  "/bin/sh -c '/bin/sleep 30 & echo $! > %[1]s/shape; /bin/sleep 30' &\n",
			parent: "middle", group: [2]bool{true, true}, root: [2]bool{true, false}, managed: [2]bool{true, true},
			rejectConflated: [2]bool{false, true},
		},
		{
			name:   "escapee behind live group member",
			plant:  "/bin/sh -c 'set -m; /bin/sleep 30 & echo $! > %[1]s/shape; set +m; /bin/sleep 30' &\n",
			parent: "middle", group: [2]bool{true, true}, root: [2]bool{true, false}, managed: [2]bool{true, true},
			rejectConflated: [2]bool{false, true},
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			root, descendant, release := plantManagedDarwinShape(t, shape.plant, shape.parent)
			assertManagedDarwinCensus(t, root.Process.Pid, descendant,
				shape.group[0], shape.root[0], shape.managed[0], shape.rejectConflated[0])
			releaseManagedDarwinRoot(t, root, release)
			assertManagedDarwinCensus(t, root.Process.Pid, descendant,
				shape.group[1], shape.root[1], shape.managed[1], shape.rejectConflated[1])
		})
	}
}

func TestDarwinManagedPlatformLimitsRemainExplicit(t *testing.T) {
	if role := os.Getenv(darwinLimitFixtureRole); role != "" {
		runDarwinLimitFixture(t, role)
		return
	}

	t.Run("direct root escape before first managed census", func(t *testing.T) {
		root, descendant, release := plantManagedDarwinShape(t,
			"set -m\n/bin/sleep 30 &\necho $! > %[1]s/shape\nset +m\n", "root")
		releaseManagedDarwinRoot(t, root, release)
		assertDarwinLimitOutsideManagedReach(t, root.Process.Pid, descendant)
	})

	t.Run("double forked setsid orphan", func(t *testing.T) {
		directory := t.TempDir()
		shape := filepath.Join(directory, "shape")
		release := filepath.Join(directory, "release")
		root := exec.Command(os.Args[0], "-test.run=^TestDarwinManagedPlatformLimitsRemainExplicit$")
		root.Env = append(os.Environ(),
			darwinLimitFixtureRole+"=root",
			darwinLimitFixtureShape+"="+shape,
			darwinLimitFixtureRelease+"="+release,
		)
		root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		{
			err := root.Start()
			require.NoError(t, err)
		}
		t.Cleanup(func() {
			_ = unix.Kill(-root.Process.Pid, unix.SIGKILL)
			_, _ = root.Process.Wait()
		})
		awaitDarwinLimitFile(t, shape)
		descendant := readDarwinLimitIdentity(t, shape)
		t.Cleanup(func() { _ = unix.Kill(descendant, unix.SIGKILL) })
		waitManagedDarwinParent(t, descendant, root.Process.Pid, "init")
		assertDarwinLimitOutsideManagedReach(t, root.Process.Pid, descendant)
		{
			err := os.WriteFile(release, []byte("release"), 0o600)
			require.NoError(t, err)
		}
		{
			err := root.Wait()
			require.NoError(t, err)
		}
		assertDarwinLimitOutsideManagedReach(t, root.Process.Pid, descendant)
	})
}

func runDarwinLimitFixture(t *testing.T, role string) {
	t.Helper()
	shape := os.Getenv(darwinLimitFixtureShape)
	release := os.Getenv(darwinLimitFixtureRelease)
	switch role {
	case "root":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinManagedPlatformLimitsRemainExplicit$")
		command.Env = replaceDarwinLimitRole("middle")
		{
			err := command.Start()
			require.NoError(t, err)
		}
		awaitDarwinLimitFile(t, shape)
		awaitDarwinLimitFile(t, release)
	case "middle":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinManagedPlatformLimitsRemainExplicit$")
		command.Env = replaceDarwinLimitRole("session")
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		{
			err := command.Start()
			require.NoError(t, err)
		}
	case "session":
		{
			err := os.WriteFile(shape, []byte(strconv.Itoa(os.Getpid())), 0o600)
			require.NoError(t, err)
		}
		time.Sleep(30 * time.Second)
	default:
		require.FailNowf(t, "unknown Darwin limit fixture role", "role: %q", role)
	}
}

func replaceDarwinLimitRole(role string) []string {
	prefix := darwinLimitFixtureRole + "="
	environment := make([]string, 0, len(os.Environ())+1)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, prefix) {
			environment = append(environment, variable)
		}
	}
	return append(environment, prefix+role)
}

func awaitDarwinLimitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.FailNowf(t, "Darwin limit fixture did not create its file", "path: %s", path)
}

func readDarwinLimitIdentity(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	identity, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	require.NoError(t, err, "invalid Darwin process identity %q: %v", contents, err)
	assert.False(t, identity <= 0, "invalid Darwin process identity %q: %v", contents, err)
	return identity
}

func assertDarwinLimitOutsideManagedReach(t *testing.T, root, descendant int) {
	t.Helper()
	processes, err := darwinProcessCensus()
	require.NoError(t, err)
	identity, present := darwinIdentityForPID(processes, descendant)
	assert.True(t, present, "escaped descendant %d is not live", descendant)
	assert.NotEqual(t, root, int(identity.group), "escaped descendant %d remained in root process group %d", descendant, root)
	managed := darwinReachableDomain(processes, int32(root), nil)
	{
		_, contained := managed[identity.identity]
		assert.False(t, contained, "platform-limit descendant %d was unexpectedly reachable", descendant)
	}
}

func darwinIdentityForPID(processes []darwinProcessSnapshot, pid int) (darwinProcessSnapshot, bool) {
	for _, process := range processes {
		if int(process.identity.pid) == pid {
			return process, true
		}
	}
	return darwinProcessSnapshot{}, false
}

func assertManagedDarwinCensus(
	t *testing.T,
	root, descendant int,
	wantGroup, wantRoot, wantManaged bool,
	wantRejectConflated bool,
) {
	t.Helper()
	processes, err := darwinProcessCensus()
	require.NoError(t, err)
	groupOccupied := false
	for _, process := range processes {
		groupOccupied = groupOccupied || (int(process.group) == root && int(process.identity.pid) != root)
	}
	fromRoot := darwinDescendants(processes, int32(root))
	managed := darwinReachableDomain(processes, int32(root), nil)
	managedPIDs := make(map[int32]bool, len(managed))
	for identity := range managed {
		managedPIDs[identity.pid] = true
	}
	assert.Equal(t, wantGroup, groupOccupied, "census group/root/managed=%t/%t/%t, want %t/%t/%t", groupOccupied, fromRoot[int32(descendant)], managedPIDs[int32(descendant)], wantGroup, wantRoot, wantManaged)
	assert.Equal(t, wantRoot, fromRoot[int32(descendant)], "census group/root/managed=%t/%t/%t, want %t/%t/%t", groupOccupied, fromRoot[int32(descendant)], managedPIDs[int32(descendant)], wantGroup, wantRoot, wantManaged)
	assert.Equal(t, wantManaged, managedPIDs[int32(descendant)], "census group/root/managed=%t/%t/%t, want %t/%t/%t", groupOccupied, fromRoot[int32(descendant)], managedPIDs[int32(descendant)], wantGroup, wantRoot, wantManaged)
	brokenGroupOccupied := fromRoot[int32(descendant)]
	{
		rejected := brokenGroupOccupied != wantGroup
		assert.Equal(t, wantRejectConflated, rejected, "conflated root/group instrument rejected=%t, want %t", rejected, wantRejectConflated)
	}
}

func darwinDescendants(processes []darwinProcessSnapshot, root int32) map[int32]bool {
	children := make(map[int32][]int32)
	for _, process := range processes {
		children[process.parent] = append(children[process.parent], process.identity.pid)
	}
	found := make(map[int32]bool)
	var walk func(int32)
	walk = func(parent int32) {
		for _, child := range children[parent] {
			if !found[child] {
				found[child] = true
				walk(child)
			}
		}
	}
	walk(root)

	return found
}

func plantManagedDarwinShape(t *testing.T, plant, parent string) (*exec.Cmd, int, io.Closer) {
	t.Helper()
	directory := t.TempDir()
	script := strings.ReplaceAll(plant, "%[1]s", directory) + "read discarded\nexit 0\n"
	root := exec.Command("/bin/sh", "-c", script)
	root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := root.StdinPipe()
	require.NoError(t, err)
	{
		err = root.Start()
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_ = unix.Kill(-root.Process.Pid, unix.SIGKILL)
		_ = stdin.Close()
		_, _ = root.Process.Wait()
	})

	shape := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && shape == 0 {
		contents, readErr := os.ReadFile(filepath.Join(directory, "shape"))
		if readErr == nil {
			shape, _ = strconv.Atoi(strings.TrimSpace(string(contents)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	assert.False(t, shape <= 0, "descendant shape did not report its process ID")
	t.Cleanup(func() { _ = unix.Kill(shape, unix.SIGKILL) })
	waitManagedDarwinParent(t, shape, root.Process.Pid, parent)

	return root, shape, stdin
}

func waitManagedDarwinParent(t *testing.T, shape, root int, parent string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", shape)
		require.NoError(t, err, "observe descendant parent: %v/%d", err, len(processes))
		require.Len(t, processes, 1, "observe descendant parent: %v/%d", err, len(processes))
		observed := int(processes[0].Eproc.Ppid)
		if (parent == "root" && observed == root) || (parent == "init" && observed == 1) ||
			(parent == "middle" && observed != root && observed != 1) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.FailNowf(t, "descendant did not settle to expected parentage", "parent: %s", parent)
}

func releaseManagedDarwinRoot(t *testing.T, root *exec.Cmd, release io.Closer) {
	t.Helper()
	{
		err := release.Close()
		require.NoError(t, err)
	}
	{
		err := root.Wait()
		require.NoError(t, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", root.Process.Pid)
		require.NoError(t, err)
		if len(processes) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.FailNow(t, "root never left the process table")
}
