package floatincrement_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/viruses/floatincrement"
	"github.com/stretchr/testify/assert"
)

func TestFloatIncrement_32(t *testing.T) {
	t.Run("no mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float32 = 0.0
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 1
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation fine precision", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float32 = 0.1e-14
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 1.000000000000001
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("two mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float32 = 9.9
		|var other float32 = 99.9
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 10.9
		|var other float32 = 99.9
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 9.9
		|var other float32 = 100.9
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
				gomutatedfile.New("Float Increment", "source.go", source, mutation2),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("many mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float32 = 100.0
		|var text string = "text"
		|var other float32 = 25.5
		|var point int = 3
		|var another float32 = 41.1
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 101
		|var text string = "text"
		|var other float32 = 25.5
		|var point int = 3
		|var another float32 = 41.1
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 100.0
		|var text string = "text"
		|var other float32 = 26.5
		|var point int = 3
		|var another float32 = 41.1
		|`)

		mutation3 := oozetesting.Source(`
		|package source
		|
		|var number float32 = 100.0
		|var text string = "text"
		|var other float32 = 25.5
		|var point int = 3
		|var another float32 = 42.1
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
				gomutatedfile.New("Float Increment", "source.go", source, mutation2),
				gomutatedfile.New("Float Increment", "source.go", source, mutation3),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})
}

func TestFloatIncrement_64(t *testing.T) {
	t.Run("no mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float64 = 0.0
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 1
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation fine precision", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float64 = 0.1e-14
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 1.000000000000001
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("two mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float64 = 9.9
		|var other float64 = 99.9
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 10.9
		|var other float64 = 99.9
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 9.9
		|var other float64 = 100.9
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
				gomutatedfile.New("Float Increment", "source.go", source, mutation2),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("many mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|var number float64 = 100.0
		|var text string = "text"
		|var other float64 = 25.5
		|var point int = 3
		|var another float64 = 41.1
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 101
		|var text string = "text"
		|var other float64 = 25.5
		|var point int = 3
		|var another float64 = 41.1
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 100.0
		|var text string = "text"
		|var other float64 = 26.5
		|var point int = 3
		|var another float64 = 41.1
		|`)

		mutation3 := oozetesting.Source(`
		|package source
		|
		|var number float64 = 100.0
		|var text string = "text"
		|var other float64 = 25.5
		|var point int = 3
		|var another float64 = 42.1
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Float Increment", "source.go", source, mutation1),
				gomutatedfile.New("Float Increment", "source.go", source, mutation2),
				gomutatedfile.New("Float Increment", "source.go", source, mutation3),
			},
			oozetesting.Mutate(
				floatincrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})
}
