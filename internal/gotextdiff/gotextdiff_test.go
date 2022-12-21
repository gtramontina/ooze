package gotextdiff_test

import (
	"strings"
	"testing"

	"github.com/gtramontina/ooze/internal/gotextdiff"
	"github.com/stretchr/testify/assert"
)

func TestName(t *testing.T) {
	t.Run("no diffs", func(t *testing.T) {
		differ := gotextdiff.New()
		diff := differ.Diff("a", "b", []byte("same\n"), []byte("same\n"))
		assert.Equal(t, "", diff)
	})

	t.Run("simple diff", func(t *testing.T) {
		differ := gotextdiff.New()
		diff := differ.Diff("a", "b", []byte("same\n"), []byte("different\n"))
		assert.Equal(t, []string{
			"--- a",
			"+++ b",
			"@@ -1 +1 @@",
			"-same",
			"+different",
			"",
		}, strings.Split(diff, "\n"))
	})
}
