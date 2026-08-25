//go:build !windows

package fsrepository

func transientRepositoryRemoveError(error) bool { return false }
