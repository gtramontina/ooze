package gomutatedfile_test

import (
	"strings"
	"testing"

	"github.com/gtramontina/ooze/internal/gomutatedfile"
	"github.com/gtramontina/ooze/internal/oozetesting/fakediffer"
	"github.com/stretchr/testify/assert"
)

func TestGoMutatedFile(t *testing.T) {
	t.Parallel()

	t.Run("prints a diff", func(t *testing.T) {
		t.Parallel()

		diff := gomutatedfile.New(
			"some-infection",
			"some-path.go",
			[]byte("original"),
			[]byte("mutated"),
		).Diff(fakediffer.New())

		assert.Equal(t, []string{
			"From: some-path.go (original)",
			"To: some-path.go (mutated with 'some-infection')",
			"",
			"- original",
			"+ mutated",
		}, strings.Split(diff, "\n"))
	})
}
