package ignoredrepository_test

import (
	"regexp"
	"testing"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ignoredrepository"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/stretchr/testify/assert"
)

func TestIgnoredRepository(t *testing.T) {
	t.Run("empty repository yields empty results", func(t *testing.T) {
		repository := ignoredrepository.New(
			regexp.MustCompile(".*"),
			fakerepository.New(fakerepository.FS{}),
		)

		assert.Equal(t, []*gosourcefile.GoSourceFile{}, repository.ListGoSourceFiles())
	})

	t.Run("multiple files with match all pattern yields no files", func(t *testing.T) {
		repository := ignoredrepository.New(
			regexp.MustCompile(".*"),
			fakerepository.New(fakerepository.FS{
				"source1.go": []byte("source 1"),
				"source2.go": []byte("source 2"),
				"source3.go": []byte("source 3"),
			}),
		)

		assert.Equal(t, []*gosourcefile.GoSourceFile{}, repository.ListGoSourceFiles())
	})

	t.Run("multiple files with specific pattern yields filtered files", func(t *testing.T) {
		{
			repository := ignoredrepository.New(
				regexp.MustCompile(".*2.*"),
				fakerepository.New(fakerepository.FS{
					"source1.go": []byte("source 1"),
					"source2.go": []byte("source 2"),
					"source3.go": []byte("source 3"),
				}),
			)

			assert.Equal(t, []*gosourcefile.GoSourceFile{
				gosourcefile.New("", "source1.go", []byte("source 1")),
				gosourcefile.New("", "source3.go", []byte("source 3")),
			}, repository.ListGoSourceFiles())
		}

		{
			repository := ignoredrepository.New(
				regexp.MustCompile("^dir/source.*$"),
				fakerepository.New(fakerepository.FS{
					"source1.go":            []byte("source 1"),
					"dir/source2.go":        []byte("source 2"),
					"dir/source3.go":        []byte("source 3"),
					"dir/subdir/source4.go": []byte("source 4"),
					"dir/subdir/source5.go": []byte("source 5"),
				}),
			)

			assert.Equal(t, []*gosourcefile.GoSourceFile{
				gosourcefile.New("", "dir/subdir/source4.go", []byte("source 4")),
				gosourcefile.New("", "dir/subdir/source5.go", []byte("source 5")),
				gosourcefile.New("", "source1.go", []byte("source 1")),
			}, repository.ListGoSourceFiles())
		}
	})
}

func TestIgnoredRepository_LinkAllToTemporaryRepository(t *testing.T) {
	t.Run("delegates to underlying repository", func(t *testing.T) {
		expectedTempRepository := fakerepository.NewTemporary()
		repository := ignoredrepository.New(
			regexp.MustCompile("dummy"),
			fakerepository.New(fakerepository.FS{}, expectedTempRepository),
		)

		actualTempRepository := repository.LinkAllToTemporaryRepository("temporary-path")

		assert.Equal(t, expectedTempRepository, actualTempRepository)
		assert.Equal(t, "temporary-path", expectedTempRepository.Root())
	})
}
