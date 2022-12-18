package gomutatedfile

type Repository interface {
	Overwrite(filePath string, data []byte)
}

type GoMutatedFile struct {
	infectionName     string
	relativePath      string
	rawSourceContent  []byte
	rawMutatedContent []byte
}

func New(infectionName, relativePath string, rawSourceContent, rawMutatedContent []byte) *GoMutatedFile {
	return &GoMutatedFile{
		infectionName:     infectionName,
		relativePath:      relativePath,
		rawSourceContent:  rawSourceContent,
		rawMutatedContent: rawMutatedContent,
	}
}

func (f *GoMutatedFile) WriteTo(repository Repository) {
	repository.Overwrite(f.relativePath, f.rawMutatedContent)
}

func (f *GoMutatedFile) String() string {
	return f.relativePath
}

func (f *GoMutatedFile) Label() string {
	return f.relativePath + "~>" + f.infectionName
}
