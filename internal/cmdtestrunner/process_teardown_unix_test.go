//go:build darwin || linux

package cmdtestrunner_test

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Fixture teardown has to survive the case where the descendant it killed is
// not reaped: it stays in the process table as a zombie, which kill(pid, 0)
// reports as present and cannot distinguish from a live process. Teardown must
// recognize that such a process can no longer execute and return, rather than
// waiting out its whole budget and reporting a descendant that outlived the
// fixture. That path is reached whenever a fixture fails before the supervised
// root exits, which is exactly when teardown has to be trustworthy.
//
// The holder execs into a sleep rather than staying a shell, and that detail is
// what makes this fixture discriminate. A shell reaps its children, so the
// zombie exists for well under a millisecond and teardown's first poll simply
// finds the PID gone -- which it would also do with the zombie handling removed
// entirely. Replacing the shell leaves nothing to reap, so the zombie persists
// and the zombie handling is the only thing that can end the wait.
func TestFixtureTeardownTreatsAnUnreapedDescendantAsDrained(t *testing.T) {
	// Own process group, so teardown below reaches the holder and every
	// descendant it has, whatever this test does or fails to do.
	//nolint:noctx // Bounded by the sleeps themselves and by the group kill below.
	holder := exec.Command("/bin/sh", "-c", "/bin/sleep 25 & echo $!; exec /bin/sleep 25")
	//nolint:exhaustruct // Other process attributes retain OS defaults.
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := holder.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, holder.Start())
	t.Cleanup(func() {
		_ = unix.Kill(-holder.Process.Pid, unix.SIGKILL)
		_, _ = holder.Process.Wait()
	})

	buffer := make([]byte, 32)
	read, err := stdout.Read(buffer)
	require.NoError(t, err)
	descendantProcessID, err := strconv.Atoi(strings.TrimSpace(string(buffer[:read])))
	require.NoError(t, err)

	canExecute, err := descendantCanStillExecute(descendantProcessID)
	require.NoError(t, err)
	require.True(t, canExecute, "the descendant should be executable before teardown runs")

	started := time.Now()
	terminateDescendant(t, descendantProcessID)
	elapsed := time.Since(started)

	assert.Less(t, elapsed, time.Second, "teardown must not wait out its budget on an unreaped descendant")
	canExecute, err = descendantCanStillExecute(descendantProcessID)
	require.NoError(t, err)
	assert.False(t, canExecute, "the descendant must no longer be able to execute")
}
