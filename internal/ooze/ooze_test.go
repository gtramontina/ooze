package ooze_test

import (
	"errors"
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
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

	sourceTwoMutations := gosourcefile.New("source.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 0
	|var number1 = 1
	|`),
	)

	mutantTwoMutationsFirst := gomutatedfile.New("source.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 1
	|var number1 = 1
	|`),
	)

	mutantTwoMutationsSecond := gomutatedfile.New("source.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 0
	|var number1 = 2
	|`),
	)

	t.Run("no files yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(),
			fakelaboratory.New(),
		).Release(integerincrement.New())

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("no viruses yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(sourceDummy),
			fakelaboratory.New(),
		).Release()

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("one file, one virus and no infections yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(sourceNoMutation),
			fakelaboratory.New(),
		).Release(integerincrement.New())

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("one file, one virus and one infection yields the laboratory result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(sourceOneMutation),
			fakelaboratory.New(
				fakelaboratory.NewTuple(mutantOneMutation, result.Ok("mutant died")),
			),
		).Release(integerincrement.New())

		assert.Equal(t, result.Ok("mutant died"), diagnostic)
	})

	t.Run("one file, one virus and two infections yields the combined laboratory result", func(t *testing.T) {
		t.Parallel()
		errFirstMutantSurvived := errors.New("first mutant survived")
		errSecondMutantSurvived := errors.New("second mutant survived")

		type scenario struct {
			description            string
			firstMutationResult    result.Result[string]
			secondMutationResult   result.Result[string]
			expectedCombinedResult result.Result[string]
		}

		for _, scene := range []scenario{
			{
				description:            "both mutants died",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Ok("second mutant died"),
			},
			{
				description:            "first mutant survived, second died",
				firstMutationResult:    result.Err[string](errFirstMutantSurvived),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Err[string](errFirstMutantSurvived),
			},
			{
				description:            "first mutant died, second survived",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Err[string](errSecondMutantSurvived),
				expectedCombinedResult: result.Err[string](errSecondMutantSurvived),
			},
			{
				description:            "both mutants survived",
				firstMutationResult:    result.Err[string](errFirstMutantSurvived),
				secondMutationResult:   result.Err[string](errSecondMutantSurvived),
				expectedCombinedResult: result.Err[string](errFirstMutantSurvived),
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					diagnostic := ooze.New(
						fakerepository.New(sourceTwoMutations),
						fakelaboratory.New(
							fakelaboratory.NewTuple(mutantTwoMutationsFirst, scene.firstMutationResult),
							fakelaboratory.NewTuple(mutantTwoMutationsSecond, scene.secondMutationResult),
						),
					).Release(integerincrement.New())

					assert.Equal(t, scene.expectedCombinedResult, diagnostic)
				})
			}(scene)
		}
	})
}
