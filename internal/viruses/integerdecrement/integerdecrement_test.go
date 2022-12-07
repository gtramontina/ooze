package integerdecrement_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/viruses/integerdecrement"
	"github.com/stretchr/testify/assert"
)

func TestIntegerDecrement(t *testing.T) {
	t.Parallel()

	t.Run("no mutations", func(t *testing.T) {
		t.Parallel()
		source := oozetesting.Source(`
		|package source
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{},
			oozetesting.Infect(
				integerdecrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation", func(t *testing.T) {
		t.Parallel()
		source := oozetesting.Source(`
		|package source
		|
		|var number int = 0
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number int = -1
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("source.go", mutation1),
			},
			oozetesting.Infect(
				integerdecrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("two mutations", func(t *testing.T) {
		t.Parallel()
		source := oozetesting.Source(`
		|package source
		|
		|var number int = 9
		|var other int = 99
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number int = 8
		|var other int = 99
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|var number int = 9
		|var other int = 98
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("source.go", mutation1),
				gomutatedfile.New("source.go", mutation2),
			},
			oozetesting.Infect(
				integerdecrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("many mutations", func(t *testing.T) {
		t.Parallel()
		source := oozetesting.Source(`
		|package source
		|
		|var number int = 100
		|var text string = "text"
		|var other int = 25
		|var point float = 3.0
		|var another int = 41
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|var number int = 99
		|var text string = "text"
		|var other int = 25
		|var point float = 3.0
		|var another int = 41
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|var number int = 100
		|var text string = "text"
		|var other int = 24
		|var point float = 3.0
		|var another int = 41
		|`)

		mutation3 := oozetesting.Source(`
		|package source
		|
		|var number int = 100
		|var text string = "text"
		|var other int = 25
		|var point float = 3.0
		|var another int = 40
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("source.go", mutation1),
				gomutatedfile.New("source.go", mutation2),
				gomutatedfile.New("source.go", mutation3),
			},
			oozetesting.Infect(
				integerdecrement.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})
}
