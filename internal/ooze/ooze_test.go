package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakereporter"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/viruses/integerdecrement"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
)

func TestOoze_nothing_to_test(t *testing.T) {
	t.Parallel()

	source0 := oozetesting.Source(`
	|package dummy
	|`)

	source1 := oozetesting.Source(`
	|package source
	|
	|var text = "value"
	|`)

	t.Run("no files", func(t *testing.T) {
		t.Parallel()

		reporter := fakereporter.New()
		ooze.New(
			fakerepository.New(fakerepository.FS{}),
			fakelaboratory.New(),
			reporter,
		).Release(integerincrement.New())

		reporter.Summarize()
		assert.Equal(t, &fakereporter.Summary{
			Survived: 0,
			Killed:   0,
		}, reporter.GetSummary())
	})

	t.Run("no viruses yields failed result", func(t *testing.T) {
		t.Parallel()

		reporter := fakereporter.New()
		ooze.New(
			fakerepository.New(fakerepository.FS{"source0.go": source0}),
			fakelaboratory.New(),
			reporter,
		).Release()

		reporter.Summarize()
		assert.Equal(t, &fakereporter.Summary{
			Survived: 0,
			Killed:   0,
		}, reporter.GetSummary())
	})

	t.Run("one file, one virus and no infections yields failed result", func(t *testing.T) {
		t.Parallel()

		reporter := fakereporter.New()
		ooze.New(
			fakerepository.New(fakerepository.FS{"source1.go": source1}),
			fakelaboratory.New(),
			reporter,
		).Release(integerincrement.New())

		reporter.Summarize()
		assert.Equal(t, &fakereporter.Summary{
			Survived: 0,
			Killed:   0,
		}, reporter.GetSummary())
	})
}

func TestOoze_with_mutations(t *testing.T) {
	t.Parallel()

	type scenario struct {
		description          string
		firstMutationResult  result.Result[string]
		secondMutationResult result.Result[string]
		expectedSummary      *fakereporter.Summary
	}

	source2 := oozetesting.Source(`
	|package source
	|
	|var number = 0
	|`)

	source2integerincrementMutation1 := gomutatedfile.New("Integer Increment", "source2.go", source2, oozetesting.Source(`
	|package source
	|
	|var number = 1
	|`),
	)

	source2integerdecrementMutation1 := gomutatedfile.New("Integer Decrement", "source2.go", source2, oozetesting.Source(`
	|package source
	|
	|var number = -1
	|`),
	)

	source3 := oozetesting.Source(`
	|package source
	|
	|var number0 = 0
	|var number1 = 1
	|`)

	source3integerincrementMutation1 := gomutatedfile.New("Integer Increment", "source3.go", source3, oozetesting.Source(`
	|package source
	|
	|var number0 = 1
	|var number1 = 1
	|`),
	)

	source3integerincrementMutation2 := gomutatedfile.New("Integer Increment", "source3.go", source3, oozetesting.Source(`
	|package source
	|
	|var number0 = 0
	|var number1 = 2
	|`),
	)

	source4 := oozetesting.Source(`
	|package source
	|
	|var anotherNumber = 0
	|`)

	source4integerincrementMutation1 := gomutatedfile.New("Integer Increment", "source4.go", source4, oozetesting.Source(`
	|package source
	|
	|var anotherNumber = 1
	|`),
	)

	t.Run("one file, one virus and one infection", func(t *testing.T) {
		t.Parallel()

		reporter := fakereporter.New()
		repository := fakerepository.New(fakerepository.FS{"source2.go": source2})
		ooze.New(
			repository,
			fakelaboratory.New(
				fakelaboratory.NewResult(
					repository,
					source2integerincrementMutation1,
					result.Ok("mutant died"),
				),
			),
			reporter,
		).Release(integerincrement.New())

		reporter.Summarize()
		assert.Equal(t, &fakereporter.Summary{
			Survived: 0,
			Killed:   1,
		}, reporter.GetSummary())
	})

	t.Run("one file, one virus and two infections", func(t *testing.T) {
		t.Parallel()

		for _, scene := range []scenario{
			{
				description:          "both mutants died",
				firstMutationResult:  result.Ok("first mutant died"),
				secondMutationResult: result.Ok("second mutant died"),
				expectedSummary:      &fakereporter.Summary{Survived: 0, Killed: 2},
			},
			{
				description:          "first mutant survived, second died",
				firstMutationResult:  result.Err[string]("first mutant survived"),
				secondMutationResult: result.Ok("second mutant died"),
				expectedSummary:      &fakereporter.Summary{Survived: 1, Killed: 1},
			},
			{
				description:          "first mutant died, second survived",
				firstMutationResult:  result.Ok("first mutant died"),
				secondMutationResult: result.Err[string]("second mutant survived"),
				expectedSummary:      &fakereporter.Summary{Survived: 1, Killed: 1},
			},
			{
				description:          "both mutants survived",
				firstMutationResult:  result.Err[string]("first mutant survived"),
				secondMutationResult: result.Err[string]("second mutant survived"),
				expectedSummary:      &fakereporter.Summary{Survived: 2, Killed: 0},
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					reporter := fakereporter.New()
					repository := fakerepository.New(fakerepository.FS{"source3.go": source3})
					ooze.New(
						repository,
						fakelaboratory.New(
							fakelaboratory.NewResult(
								repository,
								source3integerincrementMutation1,
								scene.firstMutationResult,
							),
							fakelaboratory.NewResult(
								repository,
								source3integerincrementMutation2,
								scene.secondMutationResult,
							),
						),
						reporter,
					).Release(integerincrement.New())

					reporter.Summarize()
					assert.Equal(t, scene.expectedSummary, reporter.GetSummary())
				})
			}(scene)
		}
	})

	t.Run("one file, two viri and one infection each file", func(t *testing.T) {
		t.Parallel()

		for _, scene := range []scenario{
			{
				description:          "both mutants died",
				firstMutationResult:  result.Ok("first mutant died"),
				secondMutationResult: result.Ok("second mutant died"),
				expectedSummary:      &fakereporter.Summary{Survived: 0, Killed: 2},
			},
			{
				description:          "first mutant survived, second died",
				firstMutationResult:  result.Err[string]("first mutant survived"),
				secondMutationResult: result.Ok("second mutant died"),
				expectedSummary:      &fakereporter.Summary{Survived: 1, Killed: 1},
			},
			{
				description:          "first mutant died, second survived",
				firstMutationResult:  result.Ok("first mutant died"),
				secondMutationResult: result.Err[string]("second mutant survived"),
				expectedSummary:      &fakereporter.Summary{Survived: 1, Killed: 1},
			},
			{
				description:          "both mutants survived",
				firstMutationResult:  result.Err[string]("first mutant survived"),
				secondMutationResult: result.Err[string]("second mutant survived"),
				expectedSummary:      &fakereporter.Summary{Survived: 2, Killed: 0},
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					reporter := fakereporter.New()
					repository := fakerepository.New(fakerepository.FS{"source2.go": source2})
					ooze.New(
						repository,
						fakelaboratory.New(
							fakelaboratory.NewResult(
								repository,
								source2integerincrementMutation1,
								scene.firstMutationResult,
							),
							fakelaboratory.NewResult(
								repository,
								source2integerdecrementMutation1,
								scene.secondMutationResult,
							),
						),
						reporter,
					).Release(
						integerincrement.New(),
						integerdecrement.New(),
					)

					reporter.Summarize()
					assert.Equal(t, scene.expectedSummary, reporter.GetSummary())
				})
			}(scene)
		}
	})

	t.Run("two files, one virus and one infection each file", func(t *testing.T) {
		t.Parallel()

		for _, scene := range []scenario{
			{
				description:          "both mutants died",
				firstMutationResult:  result.Ok("first mutant died"),
				secondMutationResult: result.Ok("second mutant died"),
				expectedSummary:      &fakereporter.Summary{Survived: 0, Killed: 2},
			},
			{
				description:          "first mutant survived, second died",
				firstMutationResult:  result.Err[string]("first mutant survived"),
				secondMutationResult: result.Ok("second mutant died"),
				expectedSummary:      &fakereporter.Summary{Survived: 1, Killed: 1},
			},
			{
				description:          "first mutant died, second survived",
				firstMutationResult:  result.Ok("first mutant died"),
				secondMutationResult: result.Err[string]("second mutant survived"),
				expectedSummary:      &fakereporter.Summary{Survived: 1, Killed: 1},
			},
			{
				description:          "both mutants survived",
				firstMutationResult:  result.Err[string]("first mutant survived"),
				secondMutationResult: result.Err[string]("second mutant survived"),
				expectedSummary:      &fakereporter.Summary{Survived: 2, Killed: 0},
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					reporter := fakereporter.New()
					repository := fakerepository.New(fakerepository.FS{
						"source2.go": source2,
						"source4.go": source4,
					})
					ooze.New(
						repository,
						fakelaboratory.New(
							fakelaboratory.NewResult(
								repository,
								source2integerincrementMutation1,
								scene.firstMutationResult,
							),
							fakelaboratory.NewResult(
								repository,
								source4integerincrementMutation1,
								scene.secondMutationResult,
							),
						),
						reporter,
					).Release(
						integerincrement.New(),
					)

					reporter.Summarize()
					assert.Equal(t, scene.expectedSummary, reporter.GetSummary())
				})
			}(scene)
		}
	})
}
