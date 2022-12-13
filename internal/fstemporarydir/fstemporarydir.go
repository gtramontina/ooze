package fstemporarydir

import "os"

type FSTemporaryDir struct {
	prefix string
}

func New(prefix string) *FSTemporaryDir {
	return &FSTemporaryDir{prefix: prefix}
}

func (f *FSTemporaryDir) New() string {
	temporaryDir, err := os.MkdirTemp("", f.prefix)
	if err != nil {
		panic(err)
	}

	return temporaryDir
}
