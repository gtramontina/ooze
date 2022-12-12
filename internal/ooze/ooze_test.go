package ooze_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
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

	source0 := oozetesting.Source(`
	|package dummy
	|`)

	source1 := oozetesting.Source(`
	|package source
	|
	|var text = "value"
	|`)

	t.Run("no files yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(fakerepository.FS{}),
			fakelaboratory.New(),
		).Release(integerincrement.New())

		assert.Equal(t, result.Err[string]("no mutations applied"), diagnostic)
	})

	t.Run("no viruses yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(fakerepository.FS{"source0.go": source0}),
			fakelaboratory.New(),
		).Release()

		assert.Equal(t, result.Err[string]("no mutations applied"), diagnostic)
	})

	t.Run("one file, one virus and no infections yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(fakerepository.FS{"source1.go": source1}),
			fakelaboratory.New(),
		).Release(integerincrement.New())

		assert.Equal(t, result.Err[string]("no mutations applied"), diagnostic)
	})
}

func TestOoze_with_mutations(t *testing.T) {
	t.Parallel()

	type scenario struct {
		description            string
		firstMutationResult    result.Result[string]
		secondMutationResult   result.Result[string]
		expectedCombinedResult result.Result[string]
	}

	source2 := oozetesting.Source(`
	|package source
	|
	|var number = 0
	|`)

	source2integerincrementMutation1 := gomutatedfile.New("source2.go", oozetesting.Source(`
	|package source
	|
	|var number = 1
	|`),
	)

	source2integerdecrementMutation1 := gomutatedfile.New("source2.go", oozetesting.Source(`
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

	source3integerincrementMutation1 := gomutatedfile.New("source3.go", oozetesting.Source(`
	|package source
	|
	|var number0 = 1
	|var number1 = 1
	|`),
	)

	source3integerincrementMutation2 := gomutatedfile.New("source3.go", oozetesting.Source(`
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

	source4integerincrementMutation1 := gomutatedfile.New("source4.go", oozetesting.Source(`
	|package source
	|
	|var anotherNumber = 1
	|`),
	)

	t.Run("one file, one virus and one infection yields the laboratory result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(
			fakerepository.New(fakerepository.FS{"source2.go": source2}),
			fakelaboratory.New(
				fakelaboratory.NewTuple(source2integerincrementMutation1, result.Ok("mutant died")),
			),
		).Release(integerincrement.New())

		assert.Equal(t, result.Ok("mutant died"), diagnostic)
	})

	t.Run("one file, one virus and two infections yields the combined laboratory result", func(t *testing.T) {
		t.Parallel()

		for _, scene := range []scenario{
			{
				description:            "both mutants died",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Ok("second mutant died"),
			},
			{
				description:            "first mutant survived, second died",
				firstMutationResult:    result.Err[string]("first mutant survived"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Err[string]("first mutant survived"),
			},
			{
				description:            "first mutant died, second survived",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Err[string]("second mutant survived"),
				expectedCombinedResult: result.Err[string]("second mutant survived"),
			},
			{
				description:            "both mutants survived",
				firstMutationResult:    result.Err[string]("first mutant survived"),
				secondMutationResult:   result.Err[string]("second mutant survived"),
				expectedCombinedResult: result.Err[string]("first mutant survived"),
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					diagnostic := ooze.New(
						fakerepository.New(fakerepository.FS{"source3.go": source3}),
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

		for _, scene := range []scenario{
			{
				description:            "both mutants died",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Ok("second mutant died"),
			},
			{
				description:            "first mutant survived, second died",
				firstMutationResult:    result.Err[string]("first mutant survived"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Err[string]("first mutant survived"),
			},
			{
				description:            "first mutant died, second survived",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Err[string]("second mutant survived"),
				expectedCombinedResult: result.Err[string]("second mutant survived"),
			},
			{
				description:            "both mutants survived",
				firstMutationResult:    result.Err[string]("first mutant survived"),
				secondMutationResult:   result.Err[string]("second mutant survived"),
				expectedCombinedResult: result.Err[string]("first mutant survived"),
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					diagnostic := ooze.New(
						fakerepository.New(fakerepository.FS{"source2.go": source2}),
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

	t.Run("two files, one virus and one infection each file yields the combined laboratory result", func(t *testing.T) {
		t.Parallel()

		for _, scene := range []scenario{
			{
				description:            "both mutants died",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Ok("second mutant died"),
			},
			{
				description:            "first mutant survived, second died",
				firstMutationResult:    result.Err[string]("first mutant survived"),
				secondMutationResult:   result.Ok("second mutant died"),
				expectedCombinedResult: result.Err[string]("first mutant survived"),
			},
			{
				description:            "first mutant died, second survived",
				firstMutationResult:    result.Ok("first mutant died"),
				secondMutationResult:   result.Err[string]("second mutant survived"),
				expectedCombinedResult: result.Err[string]("second mutant survived"),
			},
			{
				description:            "both mutants survived",
				firstMutationResult:    result.Err[string]("first mutant survived"),
				secondMutationResult:   result.Err[string]("second mutant survived"),
				expectedCombinedResult: result.Err[string]("first mutant survived"),
			},
		} {
			func(scene scenario) {
				t.Run(scene.description, func(t *testing.T) {
					t.Parallel()

					diagnostic := ooze.New(
						fakerepository.New(fakerepository.FS{
							"source2.go": source2,
							"source4.go": source4,
						}),
						fakelaboratory.New(
							fakelaboratory.NewTuple(source2integerincrementMutation1, scene.firstMutationResult),
							fakelaboratory.NewTuple(source4integerincrementMutation1, scene.secondMutationResult),
						),
					).Release(
						integerincrement.New(),
					)

					assert.Equal(t, scene.expectedCombinedResult, diagnostic)
				})
			}(scene)
		}
	})
}
