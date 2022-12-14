package verboserepository_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/oozetesting/fakelogger"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/gtramontina/ooze/internal/verboserepository"
	"github.com/stretchr/testify/assert"
)

func TestVerboseRepository(t *testing.T) {
	t.Parallel()

	t.Run("logs when listing source files", func(t *testing.T) {
		t.Parallel()

		logger := fakelogger.New()

		verboserepository.New(
			logger,
			fakerepository.New(fakerepository.FS{
				"file_a.go":     []byte("contents a"),
				"file_b.go":     []byte("contents b"),
				"dir/file_c.go": []byte("contents c"),
			}),
		).ListGoSourceFiles()

		assert.Equal(t, []string{
			"listing go source files…",
			"found 3 source files: [dir/file_c.go file_a.go file_b.go]",
		}, logger.LoggedLines())
	})

	t.Run("logs when linking to temporary path", func(t *testing.T) {
		t.Parallel()

		logger := fakelogger.New()

		verboserepository.New(
			logger,
			fakerepository.New(
				fakerepository.FS{
					"file_a.go": []byte("contents a"),
					"file_b.go": []byte("contents b"),
				},
				fakerepository.NewTemporaryAt("dummy"),
			),
		).LinkAllToTemporaryRepository("some-path")

		assert.Equal(t, []string{
			"linking all files to temporary path 'some-path'…",
			"linked all files to temporary path 'some-path'",
		}, logger.LoggedLines())
	})
}
