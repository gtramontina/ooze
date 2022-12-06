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
	"github.com/gtramontina/ooze/internal/viruses"
	"github.com/gtramontina/ooze/internal/viruses/integerincrement"
	"github.com/stretchr/testify/assert"
)

var errNotImplemented = errors.New("not implemented")

func TestOoze(t *testing.T) {
	t.Parallel()

	t.Run("no files yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(fakerepository.New(), fakelaboratory.New(
			fakelaboratory.NewTuple(
				gomutatedfile.New("dummy.go", []byte{}),
				result.Err[string](errNotImplemented),
			),
		)).Release(
			[]viruses.Virus{
				integerincrement.New(),
			},
		)

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("no viruses yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(fakerepository.New(
			gosourcefile.New("source.go", oozetesting.Source(`
				|package source
				|`),
			),
		), fakelaboratory.New(
			fakelaboratory.NewTuple(
				gomutatedfile.New("dummy.go", []byte{}),
				result.Err[string](errNotImplemented),
			),
		)).Release(
			[]viruses.Virus{},
		)

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("single file single virus with no possible infections yields failed result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(fakerepository.New(
			gosourcefile.New("source.go", oozetesting.Source(`
				|package source
				|
				|var text = "value"
				|`),
			),
		), fakelaboratory.New(
			fakelaboratory.NewTuple(
				gomutatedfile.New("dummy.go", []byte{}),
				result.Err[string](errNotImplemented),
			),
		)).Release(
			[]viruses.Virus{
				integerincrement.New(),
			},
		)

		assert.Equal(t, result.Err[string](ooze.ErrNoMutationsApplied), diagnostic)
	})

	t.Run("single file single virus with one possible infection yields the laboratory result", func(t *testing.T) {
		t.Parallel()
		diagnostic := ooze.New(fakerepository.New(
			gosourcefile.New("source.go", oozetesting.Source(`
				|package source
				|
				|var number = 0
				|`),
			),
		), fakelaboratory.New(
			fakelaboratory.NewTuple(
				gomutatedfile.New("source.go", oozetesting.Source(`
					|package source
					|
					|var number = 1
					|`),
				),
				result.Ok("mutant died"),
			),
		)).Release(
			[]viruses.Virus{
				integerincrement.New(),
			},
		)

		assert.Equal(t, result.Ok("mutant died"), diagnostic)
	})
}
