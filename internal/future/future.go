package future

type Future[T any] interface {
	Await() T
}

func Resolved[T any](value T) Future[T] {
	return &resolved[T]{value: value}
}

func Deferred[T any]() *DeferredFuture[T] {
	return &DeferredFuture[T]{
		channel: make(chan T),
	}
}
