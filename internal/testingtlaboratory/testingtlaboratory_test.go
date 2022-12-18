package testingtlaboratory_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/basicreporter"
	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestingt"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/testingtlaboratory"
	"github.com/stretchr/testify/assert"
)

func TestTestingTLaboratory(t *testing.T) {
	t.Parallel()

	repository := fakerepository.New(fakerepository.FS{})
	mutatedFile := gomutatedfile.New(
		"test-infection",
		"some-path.go",
		nil,
		nil,
	)

	t.Run("tags the methods and function calls as T helpers", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		laboratory := testingtlaboratory.New(
			fakeT,
			fakelaboratory.NewAlways(result.Ok("mutant killed")),
			basicreporter.New(),
		)

		assert.Equal(t, 1, fakeT.HelperCalls())

		laboratory.Test(repository, mutatedFile)

		assert.Equal(t, 2, fakeT.HelperCalls())
	})

	t.Run("sets up a subtest named after the infected file that delegates the test execution", func(t *testing.T) {
		t.Parallel()

		fakeT := faketestingt.New()
		reporter := basicreporter.New()

		testingtlaboratory.New(
			fakeT,
			fakelaboratory.NewAlways(result.Ok("mutant killed")),
			reporter,
		).Test(repository, mutatedFile)
		reporter.Summarize()

		assert.Equal(t, &basicreporter.Summary{
			Total:    0,
			Survived: 0,
			Killed:   0,
			Score:    -1,
		}, reporter.GetSummary())

		subtest := fakeT.GetSubtest("some-path.go~>test-infection")
		assert.NotNil(t, subtest)

		subtest.Run()
		reporter.Summarize()
		assert.True(t, subtest.IsParallel())
		assert.Equal(t, &basicreporter.Summary{
			Total:    1,
			Survived: 0,
			Killed:   1,
			Score:    1,
		}, reporter.GetSummary())
	})

	t.Run("subtests never fail regardless of the laboratory results", func(t *testing.T) {
		t.Parallel()

		{
			fakeT := faketestingt.New()
			reporter := basicreporter.New()

			testingtlaboratory.New(
				fakeT,
				fakelaboratory.NewAlways(result.Ok("mutant killed")),
				reporter,
			).Test(repository, mutatedFile)

			subtest := fakeT.GetSubtest("some-path.go~>test-infection")
			assert.NotNil(t, subtest)

			subtest.Run()
			assert.False(t, subtest.Failed())
		}

		{
			fakeT := faketestingt.New()
			reporter := basicreporter.New()

			testingtlaboratory.New(
				fakeT,
				fakelaboratory.NewAlways(result.Err[string]("mutant survived")),
				reporter,
			).Test(repository, mutatedFile)

			subtest := fakeT.GetSubtest("some-path.go~>test-infection")
			assert.NotNil(t, subtest)

			subtest.Run()
			assert.False(t, subtest.Failed())
		}
	})
}
