package fakerepository

type FakeTemporaryRepository struct {
	root    string
	fs      FS
	removed bool
}

func NewTemporary() *FakeTemporaryRepository {
	return &FakeTemporaryRepository{
		root:    "<unset>",
		fs:      FS{},
		removed: false,
	}
}

func (r *FakeTemporaryRepository) Root() string {
	if r.removed {
		panic("repository already removed!")
	}

	return r.root
}

func (r *FakeTemporaryRepository) Overwrite(filePath string, data []byte) {
	if r.removed {
		panic("repository already removed!")
	}

	r.fs[filePath] = data
}

func (r *FakeTemporaryRepository) Remove() {
	if r.removed {
		panic("repository already removed!")
	}

	r.removed = true
}

func (r *FakeTemporaryRepository) Removed() bool {
	return r.removed
}

func (r *FakeTemporaryRepository) ListFiles() FS {
	return r.fs.copy()
}
