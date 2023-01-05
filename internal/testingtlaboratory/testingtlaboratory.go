package testingtlaboratory

import (
	"testing"

	"github.com/gtramontina/ooze/internal/future"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type TestingT interface {
	Helper()
	Run(string, func(*testing.T)) bool
}

type TestingTLaboratory struct {
	t        TestingT
	delegate ooze.Laboratory
	parallel bool
}

func New(t TestingT, delegate ooze.Laboratory, parallel bool) *TestingTLaboratory {
	t.Helper()

	return &TestingTLaboratory{
		t:        t,
		delegate: delegate,
		parallel: parallel,
	}
}

func (l *TestingTLaboratory) Test(
	repository ooze.Repository,
	file *gomutatedfile.GoMutatedFile,
) future.Future[result.Result[string]] {
	l.t.Helper()

	fut := future.Deferred[result.Result[string]]()

	l.t.Run(file.Label(), func(t *testing.T) { //nolint:thelper
		if l.parallel {
			t.Parallel()
		}

		fut.Resolve(l.delegate.Test(repository, file).Await())
	})

	return fut
}
