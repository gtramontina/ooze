package gomutatedfile

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
