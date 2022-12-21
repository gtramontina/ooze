package testingtlaboratory

import (
	"testing"

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
}

func New(t TestingT, delegate ooze.Laboratory) *TestingTLaboratory {
	t.Helper()

	return &TestingTLaboratory{
		t:        t,
		delegate: delegate,
	}
}

func (l *TestingTLaboratory) Test(
	repository ooze.Repository,
	file *gomutatedfile.GoMutatedFile,
) <-chan result.Result[string] {
	l.t.Helper()

	diagnostic := make(chan result.Result[string], 1)

	l.t.Run(file.Label(), func(t *testing.T) { //nolint:thelper
		t.Parallel()
		diagnostic <- <-l.delegate.Test(repository, file)
	})

	return diagnostic
}
