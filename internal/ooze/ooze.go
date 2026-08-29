package ooze

import (
	"github.com/gtramontina/ooze/internal/gosourcefile"
)

type Logger interface {
	Logf(message string, args ...any)
}

type Repository interface {
	ListGoSourceFiles() []*gosourcefile.GoSourceFile
	MaterializeTemporaryRepository(temporaryPath string) TemporaryRepository
}

type TemporaryRepository interface {
	Repository
	Root() string
	Overwrite(filePath string, data []byte)
	Remove()
}
