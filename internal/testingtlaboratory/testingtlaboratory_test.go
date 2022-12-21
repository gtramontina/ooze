package testingtlaboratory_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestingt"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/testingtlaboratory"
	"github.com/stretchr/testify/assert"
)

func TestTestingTLaboratory(t *testing.T) {
	repository := fakerepository.New(fakerepository.FS{})
	mutatedFile := gomutatedfile.New(
		"test-infection",
		"some-path.go",
		nil,
		nil,
	)

	t.Run("tags the methods and function calls as T helpers", func(t *testing.T) {
		fakeT := faketestingt.New()
		laboratory := testingtlaboratory.New(
			fakeT,
			fakelaboratory.NewAlways(result.Ok("mutant killed")),
		)

		assert.Equal(t, 1, fakeT.HelperCalls())

		laboratory.Test(repository, mutatedFile)

		assert.Equal(t, 2, fakeT.HelperCalls())
	})

	t.Run("sets up a subtest named after the infected file that delegates the test execution", func(t *testing.T) {
		fakeT := faketestingt.New()

		outputChannel := testingtlaboratory.New(
			fakeT,
			fakelaboratory.NewAlways(result.Ok("mutant killed")),
		).Test(repository, mutatedFile)

		subtest := fakeT.GetSubtest("some-path.go~>test-infection")
		assert.NotNil(t, subtest)

		subtest.Run()

		assert.True(t, subtest.IsParallel())
		assert.Equal(t, result.Ok("mutant killed"), <-outputChannel)
	})

	t.Run("subtests never fail regardless of the laboratory results", func(t *testing.T) {
		{
			fakeT := faketestingt.New()

			testingtlaboratory.New(
				fakeT,
				fakelaboratory.NewAlways(result.Ok("mutant killed")),
			).Test(repository, mutatedFile)

			subtest := fakeT.GetSubtest("some-path.go~>test-infection")
			assert.NotNil(t, subtest)

			subtest.Run()
			assert.False(t, subtest.Failed())
		}

		{
			fakeT := faketestingt.New()

			testingtlaboratory.New(
				fakeT,
				fakelaboratory.NewAlways(result.Err[string]("mutant survived")),
			).Test(repository, mutatedFile)

			subtest := fakeT.GetSubtest("some-path.go~>test-infection")
			assert.NotNil(t, subtest)

			subtest.Run()
			assert.False(t, subtest.Failed())
		}
	})
}
