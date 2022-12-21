package laboratory_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/laboratory"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/oozetesting/faketempdirectory"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestrunner"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/stretchr/testify/assert"
)

func TestLaboratory(t *testing.T) {
	t.Parallel()

	source := oozetesting.Source(`
	|package source
	|
	|var number = 1
	|`)

	sourceIntegerdecrementMutation1 := oozetesting.Source(`
	|package source
	|
	|var number = 0
	|`)

	tempRepository := fakerepository.NewTemporary()
	repository := fakerepository.New(
		fakerepository.FS{
			"readme.md": []byte("read me"),
			"source.go": source,
		},
		tempRepository,
	)

	outputChannel := laboratory.New(
		faketestrunner.New(
			faketestrunner.NewResult("tmpdir-1", result.Ok("mutants died")),
		),
		faketempdirectory.NewFakeTemporaryDirectory("tmpdir"),
	).Test(
		repository,
		gomutatedfile.New("dummy-infection", "source.go", source, sourceIntegerdecrementMutation1),
	)

	t.Run("copy all files to temporary repository replacing the mutated file", func(t *testing.T) {
		t.Parallel()

		actual := tempRepository.ListFiles()
		expected := fakerepository.FS{
			"readme.md": []byte("read me"),
			"source.go": sourceIntegerdecrementMutation1,
		}
		assert.Equal(t, expected, actual)
	})

	t.Run("removes the temporary repository used", func(t *testing.T) {
		t.Parallel()

		assert.True(t, tempRepository.Removed())
	})

	t.Run("reports the result of the test runner", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, result.Ok("mutants died"), <-outputChannel)
	})
}
