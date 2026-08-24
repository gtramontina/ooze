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
