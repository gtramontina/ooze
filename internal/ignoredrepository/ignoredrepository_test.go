package ignoredrepository_test

import (
	"regexp"
	"testing"

	"github.com/gtramontina/ooze/internal/gosourcefile"
	"github.com/gtramontina/ooze/internal/ignoredrepository"
	"github.com/gtramontina/ooze/internal/oozetesting/fakerepository"
	"github.com/stretchr/testify/assert"
)

func assertGoSourceFilesEqual(t *testing.T, expected, actual []*gosourcefile.GoSourceFile) {
	t.Helper()
	assert.True(t, gosourcefile.EqualSlice(expected, actual))
}

func TestIgnoredRepository(t *testing.T) {
	t.Run("empty repository yields empty results", func(t *testing.T) {
		repository := ignoredrepository.New(
			[]*regexp.Regexp{regexp.MustCompile(".*")},
			fakerepository.New(fakerepository.FS{}),
		)

		assert.Equal(t, []*gosourcefile.GoSourceFile{}, repository.ListGoSourceFiles())
	})

	t.Run("multiple files with match all pattern yields no files", func(t *testing.T) {
		repository := ignoredrepository.New(
			[]*regexp.Regexp{regexp.MustCompile(".*")},
			fakerepository.New(fakerepository.FS{
				"source1.go": []byte("package p"),
				"source2.go": []byte("package p"),
				"source3.go": []byte("package p"),
			}),
		)

		assert.Equal(t, []*gosourcefile.GoSourceFile{}, repository.ListGoSourceFiles())
	})

	t.Run("multiple files with specific pattern yields filtered files", func(t *testing.T) {
		{
			repository := ignoredrepository.New(
				[]*regexp.Regexp{regexp.MustCompile(".*2.*")},
				fakerepository.New(fakerepository.FS{
					"source1.go": []byte("package p"),
					"source2.go": []byte("package p"),
					"source3.go": []byte("package p"),
				}),
			)

			assertGoSourceFilesEqual(t, []*gosourcefile.GoSourceFile{
				gosourcefile.Parse("source1.go", []byte("package p")),
				gosourcefile.Parse("source3.go", []byte("package p")),
			}, repository.ListGoSourceFiles())
		}

		{
			repository := ignoredrepository.New(
				[]*regexp.Regexp{regexp.MustCompile("^dir/source.*$")},
				fakerepository.New(fakerepository.FS{
					"source1.go":            []byte("package p"),
					"dir/source2.go":        []byte("package p"),
					"dir/source3.go":        []byte("package p"),
					"dir/subdir/source4.go": []byte("package p"),
					"dir/subdir/source5.go": []byte("package p"),
				}),
			)

			assertGoSourceFilesEqual(t, []*gosourcefile.GoSourceFile{
				gosourcefile.Parse("dir/subdir/source4.go", []byte("package p")),
				gosourcefile.Parse("dir/subdir/source5.go", []byte("package p")),
				gosourcefile.Parse("source1.go", []byte("package p")),
			}, repository.ListGoSourceFiles())
		}
	})

	t.Run("multiple files with multiple patterns", func(t *testing.T) {
		{
			repository := ignoredrepository.New(
				[]*regexp.Regexp{
					regexp.MustCompile(".*2.*"),
					regexp.MustCompile(".*3.*"),
					regexp.MustCompile(".*5.*"),
				},
				fakerepository.New(fakerepository.FS{
					"source1.go": []byte("package p"),
					"source2.go": []byte("package p"),
					"source3.go": []byte("package p"),
					"source4.go": []byte("package p"),
					"source5.go": []byte("package p"),
					"source6.go": []byte("package p"),
				}),
			)

			assertGoSourceFilesEqual(t, []*gosourcefile.GoSourceFile{
				gosourcefile.Parse("source1.go", []byte("package p")),
				gosourcefile.Parse("source4.go", []byte("package p")),
				gosourcefile.Parse("source6.go", []byte("package p")),
			}, repository.ListGoSourceFiles())
		}
	})
}

func TestIgnoredRepository_LinkAllToTemporaryRepository(t *testing.T) {
	t.Run("delegates to underlying repository", func(t *testing.T) {
		expectedTempRepository := fakerepository.NewTemporary()
		repository := ignoredrepository.New(
			[]*regexp.Regexp{regexp.MustCompile("dummy")},
			fakerepository.New(fakerepository.FS{}, expectedTempRepository),
		)

		actualTempRepository := repository.LinkAllToTemporaryRepository("temporary-path")

		assert.Equal(t, expectedTempRepository, actualTempRepository)
		assert.Equal(t, "temporary-path", expectedTempRepository.Root())
	})
}
