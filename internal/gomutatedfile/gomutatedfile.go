package gomutatedfile

type Repository interface {
	Overwrite(filePath string, data []byte)
}

type GoMutatedFile struct {
	relativePath string
	rawContent   []byte
}

func New(relativePath string, rawContent []byte) *GoMutatedFile {
	return &GoMutatedFile{
		relativePath: relativePath,
		rawContent:   rawContent,
	}
}

func (f *GoMutatedFile) WriteTo(repository Repository) {
	repository.Overwrite(f.relativePath, f.rawContent)
}
