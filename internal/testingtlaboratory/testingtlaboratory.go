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
	reporter ooze.Reporter
}

func New(t TestingT, delegate ooze.Laboratory, reporter ooze.Reporter) *TestingTLaboratory {
	t.Helper()

	return &TestingTLaboratory{
		t:        t,
		delegate: delegate,
		reporter: reporter,
	}
}

func (l *TestingTLaboratory) Test(
	repository ooze.Repository,
	file *gomutatedfile.GoMutatedFile,
) result.Result[string] {
	l.t.Helper()

	l.t.Run(file.Label(), func(t *testing.T) { //nolint:thelper
		t.Parallel()

		diagnostic := l.delegate.Test(repository, file)
		l.reporter.AddDiagnostic(diagnostic)
	})

	return result.Ok[string]("ignored")
}
