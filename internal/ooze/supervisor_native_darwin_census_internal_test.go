//go:build darwin

package ooze

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

	"golang.org/x/sys/unix"
)

const (
	darwinLimitFixtureRole    = "OOZE_DARWIN_LIMIT_FIXTURE_ROLE"
	darwinLimitFixtureShape   = "OOZE_DARWIN_LIMIT_FIXTURE_SHAPE"
	darwinLimitFixtureRelease = "OOZE_DARWIN_LIMIT_FIXTURE_RELEASE"
)

func TestDarwinManagedCensusInstrumentsPerDescendantShape(t *testing.T) {
	for _, shape := range []struct {
		name, plant, parent  string
		group, root, managed [2]bool
	}{
		{
			name: "plain child", plant: "/bin/sleep 30 &\necho $! > %[1]s/shape\n", parent: "root",
			group: [2]bool{true, true}, root: [2]bool{true, false}, managed: [2]bool{true, true},
		},
		{
			name: "double-forked orphan", plant: "/bin/sh -c '/bin/sleep 30 & echo $! > %[1]s/shape'\n",
			parent: "init", group: [2]bool{true, true}, root: [2]bool{false, false}, managed: [2]bool{true, true},
		},
		{
			name: "direct-root escapee", plant: "set -m\n/bin/sleep 30 &\necho $! > %[1]s/shape\nset +m\n",
			parent: "root", group: [2]bool{false, false}, root: [2]bool{true, false}, managed: [2]bool{true, false},
		},
		{
			name:   "escapee behind live group member",
			plant:  "/bin/sh -c 'set -m; /bin/sleep 30 & echo $! > %[1]s/shape; set +m; /bin/sleep 30' &\n",
			parent: "middle", group: [2]bool{true, true}, root: [2]bool{true, false}, managed: [2]bool{true, true},
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			root, descendant, release := plantManagedDarwinShape(t, shape.plant, shape.parent)
			assertManagedDarwinCensus(t, root.Process.Pid, descendant,
				shape.group[0], shape.root[0], shape.managed[0])
			releaseManagedDarwinRoot(t, root, release)
			assertManagedDarwinCensus(t, root.Process.Pid, descendant,
				shape.group[1], shape.root[1], shape.managed[1])
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
		root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} //nolint:exhaustruct // OS defaults are intentional.
		if err := root.Start(); err != nil {
			t.Fatal(err)
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
		if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := root.Wait(); err != nil {
			t.Fatal(err)
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
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		awaitDarwinLimitFile(t, shape)
		awaitDarwinLimitFile(t, release)
	case "middle":
		command := exec.Command(os.Args[0], "-test.run=^TestDarwinManagedPlatformLimitsRemainExplicit$")
		command.Env = replaceDarwinLimitRole("session")
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true} //nolint:exhaustruct // The session escape is the fixture.
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	case "session":
		if err := os.WriteFile(shape, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
	default:
		t.Fatalf("unknown Darwin limit fixture role %q", role)
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
	t.Fatalf("Darwin limit fixture did not create %s", path)
}

func readDarwinLimitIdentity(t *testing.T, path string) int {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := strconv.Atoi(strings.TrimSpace(string(contents)))
	if err != nil || identity <= 0 {
		t.Fatalf("invalid Darwin process identity %q: %v", contents, err)
	}
	return identity
}

func assertDarwinLimitOutsideManagedReach(t *testing.T, root, descendant int) {
	t.Helper()
	processes, err := darwinProcessCensus()
	if err != nil {
		t.Fatal(err)
	}
	identity, present := darwinIdentityForPID(processes, descendant)
	if !present {
		t.Fatalf("escaped descendant %d is not live", descendant)
	}
	if int(identity.group) == root {
		t.Fatalf("escaped descendant %d remained in root process group %d", descendant, root)
	}
	managed := darwinReachableDomain(processes, int32(root), nil)
	if _, contained := managed[identity.identity]; contained {
		t.Fatalf("platform-limit descendant %d was unexpectedly reachable", descendant)
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
) {
	t.Helper()
	processes, err := darwinProcessCensus()
	if err != nil {
		t.Fatal(err)
	}
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
	if groupOccupied != wantGroup || fromRoot[int32(descendant)] != wantRoot ||
		managedPIDs[int32(descendant)] != wantManaged { //nolint:gosec // Kernel process IDs are signed 32-bit values.
		t.Fatalf("census group/root/managed=%t/%t/%t, want %t/%t/%t",
			groupOccupied, fromRoot[int32(descendant)], managedPIDs[int32(descendant)],
			wantGroup, wantRoot, wantManaged)
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
	root := exec.Command("/bin/sh", "-c", script)          //nolint:gosec,noctx // Fixed executable; bounded below.
	root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} //nolint:exhaustruct // OS defaults are intentional.
	stdin, err := root.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = root.Start(); err != nil {
		t.Fatal(err)
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
	if shape <= 0 {
		t.Fatal("descendant shape did not report its process ID")
	}
	t.Cleanup(func() { _ = unix.Kill(shape, unix.SIGKILL) })
	waitManagedDarwinParent(t, shape, root.Process.Pid, parent)

	return root, shape, stdin
}

func waitManagedDarwinParent(t *testing.T, shape, root int, parent string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", shape)
		if err != nil || len(processes) != 1 {
			t.Fatalf("observe descendant parent: %v/%d", err, len(processes))
		}
		observed := int(processes[0].Eproc.Ppid)
		if (parent == "root" && observed == root) || (parent == "init" && observed == 1) ||
			(parent == "middle" && observed != root && observed != 1) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("descendant did not settle to %s parentage", parent)
}

func releaseManagedDarwinRoot(t *testing.T, root *exec.Cmd, release io.Closer) {
	t.Helper()
	if err := release.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.Wait(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", root.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		if len(processes) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("root never left the process table")
}
