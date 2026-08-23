package cmdtestrunner_test

import "testing"

// The supervised execution domain reaches the descendants its platform contract
// covers, and no further. Where a platform offers no primitive that reaches a
// descendant, that boundary is contract rather than defect, and this fixture
// states which side of it each platform is on. It belongs in the default suite
// precisely because both outcomes are expected: it records what containment is
// promised, and fails if a platform silently changes side.
func TestSupervisedDomainPlatformContract(t *testing.T) {
	// A descendant double-forked into its own session is not a child of the
	// supervised root, so no walk of parent identity from that root reaches it
	// even while the root is alive, and it is in neither the root's process group
	// nor its session. That combination is what distinguishes this from a
	// descendant that merely left the process group: the latter is still a child
	// of a live root and so remains countable, which is why it is treated as a
	// census defect and covered separately.
	//
	// The shared setup verifies the escape rather than assuming it -- that the
	// descendant names a parent other than the supervised root, that the kernel
	// agrees about that parent, and that its session is not this process's. So
	// this fixture cannot quietly degrade into a second copy of the process-group
	// case, which is the regression that would matter once that defect is fixed.
	//
	// On macOS nothing reaches such a descendant, and the fixture requires that
	// it is still able to run once the runner has returned. On Linux the guardian
	// arms PR_SET_CHILD_SUBREAPER before it starts the command, so the orphan
	// reparents to the guardian rather than to process 1 and is found by wait4
	// whatever session it moved to; there the same fixture requires containment.
	t.Run("a descendant double-forked into its own session", func(t *testing.T) {
		if sessionEscapeDefeatsContainment {
			assertSupervisionLeavesDescendantRunning(t, true, descendantEscapesSession)

			return
		}

		assertDoesNotReturnWhileDescendantCanWrite(t, true, descendantEscapesSession)
	})
}
