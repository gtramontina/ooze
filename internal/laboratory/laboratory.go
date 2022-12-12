package laboratory

import (
	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/result"
)

type Repository interface {
	LinkAllToTemporaryRepository(temporaryPath string) TemporaryRepository
}
type TemporaryRepository interface {
	Root() string
	Overwrite(filePath string, data []byte)
	Remove()
}
type TestRunner interface {
	Test(repository TemporaryRepository) result.Result[string]
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

func (l *Laboratory) Test(repository Repository, infectedFile *goinfectedfile.GoInfectedFile) result.Result[string] {
	tempRepository := repository.LinkAllToTemporaryRepository(l.temporaryDirectory.New())
	defer tempRepository.Remove()

	infectedFile.Mutate().WriteTo(tempRepository)

	return l.testRunner.Test(tempRepository)
}
