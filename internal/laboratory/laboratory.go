package laboratory

import (
	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/result"
)

type TestRunner interface {
	Test(repository ooze.TemporaryRepository) result.Result[string]
}

type TemporaryDirectory interface {
	New() string
}

type Laboratory struct {
	testRunner         TestRunner
	temporaryDirectory TemporaryDirectory
}

func New(testRunner TestRunner, temporaryDirectory TemporaryDirectory) *Laboratory {
	return &Laboratory{
		testRunner:         testRunner,
		temporaryDirectory: temporaryDirectory,
	}
}

func (l *Laboratory) Test(repository ooze.Repository, file *goinfectedfile.GoInfectedFile) result.Result[string] {
	tempRepository := repository.LinkAllToTemporaryRepository(l.temporaryDirectory.New())
	defer tempRepository.Remove()

	file.Mutate().WriteTo(tempRepository)

	return l.testRunner.Test(tempRepository)
}
