package oozetesting

import (
	"os"
	"path"
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/viruses"
	"github.com/stretchr/testify/assert"
)

type Mutations map[string]struct {
	SourceFileName  string
	MutantFileNames []string
}

type Scenarios struct {
	virusName string
	virus     viruses.Virus
	mutations Mutations
}

func NewScenarios(virusName string, virus viruses.Virus, mutations Mutations) *Scenarios {
	return &Scenarios{
		virusName: virusName,
		virus:     virus,
		mutations: mutations,
	}
}

func Run(t *testing.T, scenes *Scenarios) {
	t.Helper()

	for name, testcase := range scenes.mutations {
		source, err := os.ReadFile(path.Join("testdata", testcase.SourceFileName))
		assert.NoError(t, err)

		expectedMutatedFiles := []*gomutatedfile.GoMutatedFile{}

		for _, mutantFileName := range testcase.MutantFileNames {
			mutant, err := os.ReadFile(path.Join("testdata", mutantFileName))
			assert.NoError(t, err)

			mutatedFile := gomutatedfile.New(scenes.virusName, testcase.SourceFileName, source, mutant)
			expectedMutatedFiles = append(expectedMutatedFiles, mutatedFile)
		}

		t.Run(name, func(t *testing.T) {
			assert.Equal(t,
				expectedMutatedFiles,
				mutate(
					scenes.virus,
					gosourcefile.New(testcase.SourceFileName, source),
				),
			)
		})
	}
}

func mutate(virus viruses.Virus, source *gosourcefile.GoSourceFile) []*gomutatedfile.GoMutatedFile {
	mutatedFiles := []*gomutatedfile.GoMutatedFile{}

	for _, infectedFile := range source.Incubate(virus) {
		mutatedFiles = append(mutatedFiles, infectedFile.Mutate())
	}

	return mutatedFiles
}
