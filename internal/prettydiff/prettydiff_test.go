package prettydiff_test

import (
	"testing"

	"github.com/fatih/color"
	"github.com/gtramontina/ooze/internal/oozetesting/stubdiffer"
	"github.com/gtramontina/ooze/internal/prettydiff"
	"github.com/stretchr/testify/assert"
)

func TestPrettyDiff(t *testing.T) {
	defer func(noColor bool) { color.NoColor = noColor }(color.NoColor)
	color.NoColor = false

	t.Run("delegates to the underlying differ", func(t *testing.T) {
		differ := prettydiff.New(stubdiffer.New("some diff"))
		diff := differ.Diff("dummy-a.go", "dummy-b.go", []byte("dummy-a"), []byte("dummy-b"))

		assert.Equal(t, "some diff", diff)
	})

	t.Run("boldens and colorizes removals", func(t *testing.T) {
		differ := prettydiff.New(stubdiffer.New("- some removal"))
		diff := differ.Diff("dummy-a.go", "dummy-b.go", []byte("dummy-a"), []byte("dummy-b"))

		assert.Equal(t, "\033[1;31m- some removal\033[0m", diff)
	})

	t.Run("colorizes additions", func(t *testing.T) {
		differ := prettydiff.New(stubdiffer.New("+ some addition"))
		diff := differ.Diff("dummy-a.go", "dummy-b.go", []byte("dummy-a"), []byte("dummy-b"))

		assert.Equal(t, "\033[32m+ some addition\033[0m", diff)
	})

	t.Run("colorizes ranges", func(t *testing.T) {
		differ := prettydiff.New(stubdiffer.New("@ some range"))
		diff := differ.Diff("dummy-a.go", "dummy-b.go", []byte("dummy-a"), []byte("dummy-b"))

		assert.Equal(t, "\033[34m@ some range\033[0m", diff)
	})
}
