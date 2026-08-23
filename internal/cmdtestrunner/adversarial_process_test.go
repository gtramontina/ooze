//go:build adversarial

package cmdtestrunner_test

import "testing"

// Adversarial process fixtures reproduce descendant behaviour that defeats
// containment. They are separated from the default suite by the `adversarial`
// build tag because they are not all expected to pass yet: each one states
// below which platforms contain the behaviour it exercises and which do not.
//
// Run them with `make test.adversarial`, or, from a linked git worktree where
// that target's hook prerequisite cannot run, with:
//
//	go test -count=1 -tags=adversarial ./internal/cmdtestrunner/...
//
// A fixture here must never be made to pass by weakening what it asserts. The
// contract they hold the supervisor to is fixed: root exit is never drainage,
// and a drained execution domain is one authoritatively observed to contain no
// process that can execute or create descendants.
func TestAdversarialProcessFixtures(t *testing.T) {
	// A descendant that makes itself a process-group leader leaves the group the
	// macOS supervisor uses as its census, so the supervisor reports drainage
	// while that descendant is still able to run. It must also close its output
	// handles: one still holding them blocks the runner on its output pipe,
	// which would hold the test open for a reason unrelated to the census.
	//
	// Measured on macOS at the moment the census runs: the supervised root's
	// group contains exactly one entry, the root's own unreaped zombie, which
	// the census skips both because its PID equals the group ID and because it
	// is a zombie. The descendant is absent from that group entirely, so the
	// census concludes the domain is drained. The descendant's parent is by then
	// process 1, so a parent-identity walk from the exited root also finds
	// nothing -- counting by parent identity fixes the live-root census the fuse
	// needs, but not this post-root-exit case.
	//
	// A session census is not an alternative on macOS, and not because the
	// interface is missing: KERN_PROC_SESSION is declared in the SDK header but
	// the kernel returns ENOENT for it, and getsid is callable on any PID. It
	// fails because the supervisor never calls setsid, so its launcher shares
	// the caller's session and a session census would sweep unrelated
	// processes -- and because a descendant that can call setpgid can call
	// setsid, escaping a session boundary by the same move.
	//
	// Contained on Linux, where the guardian arms PR_SET_CHILD_SUBREAPER before
	// starting the command, so an orphaned descendant reparents to the guardian
	// rather than to process 1; the guardian then detects it with wait4(-1) and
	// builds its kill list from /proc/self/task/*/children, falling back to the
	// PPid field of /proc/*/status. Nothing on Linux is keyed on the process
	// group. This is read from that implementation, not measured here.
	t.Run("does not return while a descendant that left the process group can write", func(t *testing.T) {
		assertDoesNotReturnWhileDescendantCanWrite(t, true, descendantEscapesProcessGroup)
	})

	// This escapee is reachable when draining begins, and the drain itself is
	// what makes it unreachable. It sits outside the supervised process group
	// behind an intermediate that is inside it, so a census that enumerated live
	// group members and walked from them would find it. The sweep kills group
	// members, the intermediate among them, and the escapee is then orphaned and
	// in no group the supervisor holds -- so the next census reports the domain
	// empty and drainage is declared over a process that is still running.
	//
	// That distinguishes it from the escapee above, whose parent is the root and
	// which is already unreachable by anything before the drain starts. This one
	// is not a platform limit: the information needed to reach it exists, and the
	// drain destroys it. Reproduced against the supervisor -- drainage reported
	// while the escapee was alive, reparented to process 1.
	t.Run("does not report drainage after its own sweep orphans an escapee", func(t *testing.T) {
		assertDoesNotReturnWhileDescendantCanWrite(t, true, descendantEscapesBehindParent)
	})
}
