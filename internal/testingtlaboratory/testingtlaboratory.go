package testingtlaboratory

import (
	"fmt"
	"testing"

	"github.com/gtramontina/ooze/internal/future"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type TestingTLaboratory struct {
	subtests subtests
	delegate ooze.Laboratory
	mode     execution
}

func New(t *testing.T, delegate ooze.Laboratory, runInParallel bool) *TestingTLaboratory {
	t.Helper()

	mode := serial
	if runInParallel {
		mode = parallel
	}

	return newWithSubtests(testingSubtests{t: t}, delegate, mode)
}

func newWithSubtests(
	subtests subtests,
	delegate ooze.Laboratory,
	mode execution,
) *TestingTLaboratory {
	return &TestingTLaboratory{
		subtests: subtests,
		delegate: delegate,
		mode:     mode,
	}
}

func (l *TestingTLaboratory) Test(
	repository ooze.Repository,
	file *gomutatedfile.GoMutatedFile,
) future.Future[result.Result[string]] {
	fut := future.Deferred[result.Result[string]]()

	started := l.subtests.Run(file.Label(), l.mode, func() {
		completed := false
		defer func() {
			if !completed {
				fut.Resolve(result.Err[string]("mutation execution aborted"))
			}
		}()

		fut.Resolve(l.delegate.Test(repository, file).Await())
		completed = true
	})
	if !started {
		panic(fmt.Sprintf("mutation subtest %q was filtered out", file.Label()))
	}

	return fut
}

type execution uint8

const (
	serial execution = iota
	parallel
)

type subtests interface {
	Run(name string, mode execution, body func()) bool
}

type testingSubtests struct {
	t *testing.T
}

func (s testingSubtests) Run(name string, mode execution, body func()) bool {
	started := false
	s.t.Run(name, func(t *testing.T) {
		started = true
		if mode == parallel {
			t.Parallel()
		}

		body()
	})

	return started
}
