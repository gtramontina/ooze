package loopbreak_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/oozetesting"
	"github.com/gtramontina/ooze/internal/viruses/loopbreak"
	"github.com/stretchr/testify/assert"
)

func TestLoopBreak_ContinueBreak(t *testing.T) {
	t.Run("no mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|}
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|}
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Loop Break", "source.go", source, mutation1),
			},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("two mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Loop Break", "source.go", source, mutation1),
				gomutatedfile.New("Loop Break", "source.go", source, mutation2),
			},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("many mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		continue
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		continue
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		break
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		mutation3 := oozetesting.Source(`
				|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		continue
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Loop Break", "source.go", source, mutation1),
				gomutatedfile.New("Loop Break", "source.go", source, mutation2),
				gomutatedfile.New("Loop Break", "source.go", source, mutation3),
			},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})
}

func TestLoopBreak_BreakContinue(t *testing.T) {
	t.Run("no mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("one mutation", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|}
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|}
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Loop Break", "source.go", source, mutation1),
			},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("two mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Loop Break", "source.go", source, mutation1),
				gomutatedfile.New("Loop Break", "source.go", source, mutation2),
			},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})

	t.Run("many mutations", func(t *testing.T) {
		source := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		break
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		mutation1 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		continue
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		break
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		mutation2 := oozetesting.Source(`
		|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		continue
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		break
		|	}
		|}
		|`)

		mutation3 := oozetesting.Source(`
				|package source
		|
		|func main() {
		|	for {
		|		break
		|	}
		|	for {
		|		var a = 1
		|	}
		|	for {
		|		break
		|	}
		|	for {
		|		var b = 2
		|	}
		|	for {
		|		continue
		|	}
		|}
		|`)

		assert.Equal(t,
			[]*gomutatedfile.GoMutatedFile{
				gomutatedfile.New("Loop Break", "source.go", source, mutation1),
				gomutatedfile.New("Loop Break", "source.go", source, mutation2),
				gomutatedfile.New("Loop Break", "source.go", source, mutation3),
			},
			oozetesting.Mutate(
				loopbreak.New(),
				gosourcefile.New("source.go", source),
			),
		)
	})
}
