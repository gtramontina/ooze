package oozetesting

import (
	"os"
	"path"
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/gotextdiff"
	"github.com/gtramontina/ooze/viruses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	workDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

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
			actualMutatedFiles := mutate(
				scenes.virus,
				gosourcefile.New(path.Join(workDir, "testdata"), testcase.SourceFileName, source),
			)

			require.Equal(t,
				len(expectedMutatedFiles),
				len(actualMutatedFiles),
				"Expected %d mutated files; got %d", len(expectedMutatedFiles), len(actualMutatedFiles),
			)

			for i, actualMutatedFile := range actualMutatedFiles {
				expectedMutatedFile := expectedMutatedFiles[i]
				assert.Equal(t,
					expectedMutatedFile,
					actualMutatedFile,
					"Actual and expected (filename %s) mutants are not equal", testcase.MutantFileNames[i])

				if t.Failed() {
					t.Logf("Mutated filed diff:\n%s", actualMutatedFile.Diff(gotextdiff.New()))
				}
			}
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
