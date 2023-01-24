package oozetesting

import (
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
)

func Mutate(virus viruses.Virus, source *gosourcefile.GoSourceFile) []*gomutatedfile.GoMutatedFile {
	mutatedFiles := []*gomutatedfile.GoMutatedFile{}

	for _, infectedFile := range source.Incubate(virus) {
		mutatedFiles = append(mutatedFiles, infectedFile.Mutate())
	}

	return mutatedFiles
}
