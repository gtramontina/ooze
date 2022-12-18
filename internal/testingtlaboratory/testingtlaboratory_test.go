package testingtlaboratory_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/basicreporter"
	"github.com/gtramontina/ooze/internal/goinfectedfile"
	"github.com/gtramontina/ooze/internal/ooze"
	"github.com/gtramontina/ooze/internal/oozetesting/fakelaboratory"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/oozetesting/faketestingt"
	"github.com/gtramontina/ooze/internal/result"
	"github.com/gtramontina/ooze/internal/testingtlaboratory"
	"github.com/gtramontina/ooze/internal/viruses"
	"github.com/stretchr/testify/assert"
)

func TestTestingTLaboratory(t *testing.T) {
	t.Parallel()

	noop := func() {}
	repository := fakerepository.New(fakerepository.FS{})
	infectedFile := goinfectedfile.New(
		"some-path.go",
		viruses.NewInfection("test-infection", noop, noop),
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

		laboratory.Test(repository, infectedFile)

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
		).Test(repository, infectedFile)

		assert.Equal(t, &ooze.ReportSummary{
			Total:    0,
			Survived: 0,
			Killed:   0,
			Score:    -1,
		}, reporter.Summarize())

		subtest := fakeT.GetSubtest("some-path.go~>test-infection")
		assert.NotNil(t, subtest)

		subtest.Run()
		assert.True(t, subtest.IsParallel())
		assert.Equal(t, &ooze.ReportSummary{
			Total:    1,
			Survived: 0,
			Killed:   1,
			Score:    1,
		}, reporter.Summarize())
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
			).Test(repository, infectedFile)

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
			).Test(repository, infectedFile)

			subtest := fakeT.GetSubtest("some-path.go~>test-infection")
			assert.NotNil(t, subtest)

			subtest.Run()
			assert.False(t, subtest.Failed())
		}
	})
}
