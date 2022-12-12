package fsrepository

type FSTemporaryRepository struct {
	root string
}

func NewTemporary(root string) *FSTemporaryRepository {
	return &FSTemporaryRepository{
		root: root,
	}
}

func (r *FSTemporaryRepository) Root() string {
	panic("root: implement me")
}

func (r *FSTemporaryRepository) Overwrite(filePath string, data []byte) {
	panic("overwrite: implement me")
}

func (r *FSTemporaryRepository) Remove() {
	panic("remove: implement me")
}
