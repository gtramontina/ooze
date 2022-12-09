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
	"github.com/gtramontina/ooze/internal/viruses/integerdecrement"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
)

func TestOoze_nothing_to_test(t *testing.T) {
	t.Parallel()

	source0 := gosourcefile.New("src.go", oozetesting.Source(`
	|package dummy
	|`),
	)

	source1 := gosourcefile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var text = "value"
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
			fakerepository.New(source0),
			fakelaboratory.New(),
		).Release()

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("one file, one virus and no infections yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(source1),
			fakelaboratory.New(),
		).Release(integerincrement.New())

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})
}

func TestOoze_with_mutations(t *testing.T) {
	t.Parallel()

	source2 := gosourcefile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var number = 0
	|`),
	)

	source2integerincrementMutation1 := gomutatedfile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var number = 1
	|`),
	)

	source2integerdecrementMutation1 := gomutatedfile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var number = -1
	|`),
	)

	source3 := gosourcefile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 0
	|var number1 = 1
	|`),
	)

	source3integerincrementMutation1 := gomutatedfile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 1
	|var number1 = 1
	|`),
	)

	source3integerincrementMutation2 := gomutatedfile.New("src.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 0
	|var number1 = 2
	|`),
	)

	t.Run("one file, one virus and one infection yields the laboratory result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(source2),
			fakelaboratory.New(
				fakelaboratory.NewTuple(source2integerincrementMutation1, result.Ok("mutant died")),
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
						fakerepository.New(source3),
						fakelaboratory.New(
							fakelaboratory.NewTuple(source3integerincrementMutation1, scene.firstMutationResult),
							fakelaboratory.NewTuple(source3integerincrementMutation2, scene.secondMutationResult),
						),
					).Release(integerincrement.New())

					assert.Equal(t, scene.expectedCombinedResult, diagnostic)
				})
			}(scene)
		}
	})

	t.Run("one file, two viri and one infection each file yields the combined laboratory result", func(t *testing.T) {
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
						fakerepository.New(source2),
						fakelaboratory.New(
							fakelaboratory.NewTuple(source2integerincrementMutation1, scene.firstMutationResult),
							fakelaboratory.NewTuple(source2integerdecrementMutation1, scene.secondMutationResult),
						),
					).Release(
						integerincrement.New(),
						integerdecrement.New(),
					)

					assert.Equal(t, scene.expectedCombinedResult, diagnostic)
				})
			}(scene)
		}
	})
}
