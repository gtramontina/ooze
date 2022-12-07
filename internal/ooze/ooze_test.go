package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/viruses"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
)

func TestOoze(t *testing.T) {
	t.Parallel()

	sourceDummy := gosourcefile.New("dummy.go", oozetesting.Source(`
	|package dummy
	|`),
	)

	sourceNoMutation := gosourcefile.New("no_mutation.go", oozetesting.Source(`
	|package source
	|
	|var text = "value"
	|`),
	)

	sourceOneMutation := gosourcefile.New("one_mutation.go", oozetesting.Source(`
	|package source
	|
	|var number = 0
	|`),
	)

	mutantOneMutation := gomutatedfile.New("one_mutation.go", oozetesting.Source(`
	|package source
	|
	|var number = 1
	|`),
	)

	t.Run("no files yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(),
			fakelaboratory.New(),
		).Release([]viruses.Virus{integerincrement.New()})

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("no viruses yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(sourceDummy),
			fakelaboratory.New(),
		).Release([]viruses.Virus{})

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("one file, one virus and no infections yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(sourceNoMutation),
			fakelaboratory.New(),
		).Release([]viruses.Virus{integerincrement.New()})

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("one file, one virus and one infection yields the laboratory result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(sourceOneMutation),
			fakelaboratory.New(
				fakelaboratory.NewTuple(mutantOneMutation, result.Ok("mutant died")),
			),
		).Release([]viruses.Virus{integerincrement.New()})

		assert.Equal(t, result.Ok("mutant died"), diagnostic)
	})
}
