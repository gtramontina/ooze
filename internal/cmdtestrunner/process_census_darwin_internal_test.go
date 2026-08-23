//go:build darwin

package cmdtestrunner

import (
	"fmt"
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

// Which descendant shapes each census instrument can see, before and after the
// attempt root exits.
//
// This exists because a one-dimensional census is the failure mode that has
// twice produced a wrong conclusion on this branch. An instrument pointed at a
// single descendant shape looks sound: a fixture asserting process-group escape
// passed while really exercising nothing of the sort, and a claim that the two
// instruments fail identically after root exit held only for the one shape it
// was measured on. Both were caught by hand. Asserting visibility per shape,
// per instrument and at both root states makes CI catch the next one.
//
// The three instruments:
//
//   - groupCensus is production code, darwinProcessGroupHasExecutableProcesses.
//     It answers one boolean about the whole execution domain rather than about
//     any individual process, which is exactly why it needs a per-shape table:
//     the boolean is right for the wrong reason in more than one row below.
//   - walkFromRoot is parent identity seeded from the attempt root. This is the
//     instrument the resolved fuse decision requires, and it is characterized
//     rather than exercised here, since no production census walks yet.
//   - walkFromLiveGroupMembers is the same walk seeded from every live member of
//     the domain's process group, plus those members. It is a candidate drain
//     instrument, and the last row is the only place it earns its complexity.
//
// A double-forked descendant that also calls setsid() is absent deliberately.
// Every instrument here sees it exactly as it sees escapeeParentRoot, because
// all three key on process group or parent identity and it has left both; its
// distinct disposition as a platform limit is asserted by the supervised-domain
// contract fixture instead.
// The parentage each planted shape must settle into before it is observed.
const (
	parentIsRoot   = "root"
	parentIsInit   = "init"
	parentIsMiddle = "middle"
)

func TestDarwinCensusInstrumentsPerDescendantShape(t *testing.T) {
	for _, shape := range []struct {
		name   string
		plant  string
		parent string

		// Expected visibility: [rootAlive, rootExited].
		domainOccupied [2]bool
		seenFromRoot   [2]bool
		seenFromGroup  [2]bool
		because        string
	}{
		{
			name:           "a plain child",
			plant:          "/bin/sleep 30 &\necho $! > %[1]s/shape\n",
			parent:         parentIsRoot,
			domainOccupied: [2]bool{true, true},
			seenFromRoot:   [2]bool{true, false},
			seenFromGroup:  [2]bool{true, true},
			because:        "stays in the group, so only the walk loses it when the root exits",
		},
		{
			name:           "a double-forked orphan",
			plant:          "/bin/sh -c '/bin/sleep 30 & echo $! > %[1]s/shape'\n",
			parent:         parentIsInit,
			domainOccupied: [2]bool{true, true},
			seenFromRoot:   [2]bool{false, false},
			seenFromGroup:  [2]bool{true, true},
			because: "reparenting to init happens the moment the middle exits, so the " +
				"walk is already incomplete while the root still lives",
		},
		{
			name:           "an escapee whose parent is the root",
			plant:          "set -m\n/bin/sleep 30 &\necho $! > %[1]s/shape\nset +m\n",
			parent:         parentIsRoot,
			domainOccupied: [2]bool{false, false},
			seenFromRoot:   [2]bool{true, false},
			seenFromGroup:  [2]bool{true, false},
			because: "left the group, so the domain reads as drained while it is still " +
				"running, and once the root exits nothing reaches it at all",
		},
		{
			name:           "an escapee whose parent is a live group member",
			plant:          "/bin/sh -c 'set -m; /bin/sleep 30 & echo $! > %[1]s/shape; set +m; /bin/sleep 30' &\n",
			parent:         parentIsMiddle,
			domainOccupied: [2]bool{true, true},
			seenFromRoot:   [2]bool{true, false},
			seenFromGroup:  [2]bool{true, true},
			because: "the only shape a walk seeded from live group members reaches after " +
				"root exit, and so the only reason to prefer it over the group census alone",
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			root, shapeProcessID, release := plantDescendantShape(t, shape.plant, shape.parent)

			assertCensusInstruments(t, "while the root is alive", root.Process.Pid, shapeProcessID,
				shape.domainOccupied[0], shape.seenFromRoot[0], shape.seenFromGroup[0], shape.because)

			releaseAndReapRoot(t, root, release)

			assertCensusInstruments(t, "after the root has exited", root.Process.Pid, shapeProcessID,
				shape.domainOccupied[1], shape.seenFromRoot[1], shape.seenFromGroup[1], shape.because)
		})
	}
}

func assertCensusInstruments(
	t *testing.T,
	state string,
	rootProcessID, shapeProcessID int,
	domainOccupied, seenFromRoot, seenFromGroup bool,
	because string,
) {
	t.Helper()

	occupied, err := darwinProcessGroupHasExecutableProcesses(rootProcessID)
	require.NoError(t, err)
	all, err := unix.SysctlKinfoProcSlice("kern.proc.all", 0)
	require.NoError(t, err)

	assert.Equal(t, domainOccupied, occupied,
		"%s: the production group census should report the domain %s -- %s",
		state, map[bool]string{true: "occupied", false: "drained"}[domainOccupied], because)
	assert.Equal(t, seenFromRoot, descendantsOf(all, []int{rootProcessID})[shapeProcessID],
		"%s: a parent-identity walk from the root should %s descendant %d -- %s",
		state, map[bool]string{true: "reach", false: "miss"}[seenFromRoot], shapeProcessID, because)
	assert.Equal(t, seenFromGroup, liveDomainMembersAndDescendants(all, rootProcessID)[shapeProcessID],
		"%s: a walk seeded from live group members should %s descendant %d -- %s",
		state, map[bool]string{true: "reach", false: "miss"}[seenFromGroup], shapeProcessID, because)
}

// descendantsOf returns every transitive descendant of the seeds, by parent
// identity. The seeds themselves are not included.
func descendantsOf(all []unix.KinfoProc, seeds []int) map[int]bool {
	children := map[int32][]int32{}
	for _, process := range all {
		children[process.Eproc.Ppid] = append(children[process.Eproc.Ppid], process.Proc.P_pid)
	}

	found := map[int]bool{}
	var walk func(int32)
	walk = func(parent int32) {
		for _, child := range children[parent] {
			if !found[int(child)] {
				found[int(child)] = true
				walk(child)
			}
		}
	}
	for _, seed := range seeds {
		walk(int32(seed)) //nolint:gosec // Process IDs are non-negative.
	}

	return found
}

// liveDomainMembersAndDescendants is the candidate drain instrument: every live
// member of the domain's process group, plus everything reachable from them by
// parent identity. It is what reaches an escapee that still has a live ancestor
// inside the group, which neither the group census nor a walk from an exited
// root can do.
func liveDomainMembersAndDescendants(all []unix.KinfoProc, group int) map[int]bool {
	var members []int
	reached := map[int]bool{}
	for _, process := range all {
		if int(process.Eproc.Pgid) == group && process.Proc.P_stat != darwinZombieProcessState {
			members = append(members, int(process.Proc.P_pid))
			reached[int(process.Proc.P_pid)] = true
		}
	}
	for descendant := range descendantsOf(all, members) {
		reached[descendant] = true
	}

	return reached
}

// plantDescendantShape starts a root that leads its own process group, waits
// until the descendant it plants has settled into the topology the shape
// describes, and returns both. Every process it creates is bounded by its own
// sleep and is terminated here whatever the assertions do.
func plantDescendantShape(t *testing.T, plant, parent string) (*exec.Cmd, int, io.Closer) {
	t.Helper()

	directory := t.TempDir()
	// The root is released by closing its standard input. A polling loop would
	// spawn children of its own inside the very group being censused, which
	// silently makes every shape look like an occupied domain.
	script := strings.ReplaceAll(plant, "%[1]s", directory) + "read discarded\nexit 0\n"
	//nolint:noctx // Bounded by its descendants' own sleeps and by the teardown below.
	root := exec.Command("/bin/sh", "-c", script)
	//nolint:exhaustruct // Other process attributes retain OS defaults.
	root.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := root.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, root.Start())

	t.Cleanup(func() {
		_ = unix.Kill(-root.Process.Pid, unix.SIGKILL)
		_ = stdin.Close()
		_, _ = root.Process.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	shapeProcessID := 0
	for time.Now().Before(deadline) && shapeProcessID <= 0 {
		contents, readErr := os.ReadFile(filepath.Join(directory, "shape"))
		if readErr == nil {
			shapeProcessID, _ = strconv.Atoi(strings.TrimSpace(string(contents)))
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.Positive(t, shapeProcessID, "the shape never reported its process ID")
	t.Cleanup(func() { _ = unix.Kill(shapeProcessID, unix.SIGKILL) })
	awaitDescendantParent(t, shapeProcessID, root.Process.Pid, parent)

	return root, shapeProcessID, stdin
}

// awaitDescendantParent blocks until the planted descendant has settled into the
// parentage its shape describes. Reparenting to init is not instantaneous -- it
// happens when the middle process exits -- so a fixture that observed the census
// immediately would be racing that transition rather than measuring it.
func awaitDescendantParent(t *testing.T, shapeProcessID, rootProcessID int, parent string) {
	t.Helper()

	settled := func(observed int) bool {
		switch parent {
		case parentIsRoot:
			return observed == rootProcessID
		case parentIsInit:
			return observed == 1
		default:
			return observed != rootProcessID && observed != 1
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	observed := 0
	for time.Now().Before(deadline) {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", shapeProcessID)
		require.NoError(t, err)
		require.Len(t, processes, 1, "descendant %d left the process table before it was observed", shapeProcessID)
		observed = int(processes[0].Eproc.Ppid)
		if settled(observed) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.FailNow(t, fmt.Sprintf(
		"descendant %d never settled to parent %q; last observed parent was %d",
		shapeProcessID, parent, observed))
}

func releaseAndReapRoot(t *testing.T, root *exec.Cmd, release io.Closer) {
	t.Helper()

	// Closing the write end is what delivers end-of-file to the root's read.
	// cmd.Stdin holds the read end, so closing that would leave it blocked.
	require.NoError(t, release.Close())
	require.NoError(t, root.Wait())

	// The root's exit and its reaping are both complete before the second
	// observation, so nothing below races the transition being measured.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		processes, err := unix.SysctlKinfoProcSlice("kern.proc.pid", root.Process.Pid)
		require.NoError(t, err)
		if len(processes) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.FailNow(t, "the root never left the process table")
}
