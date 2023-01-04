package future

type resolved[T any] struct {
	value T
}

func (f *resolved[T]) Await() T {
	return f.value
}
