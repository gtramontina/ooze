package future

import "sync"

type DeferredFuture[T any] struct {
	once  sync.Once
	ready chan struct{}
	value T
}

func (f *DeferredFuture[T]) Await() T {
	<-f.ready

	return f.value
}

func (f *DeferredFuture[T]) Resolve(value T) {
	f.once.Do(func() {
		f.value = value
		close(f.ready)
	})
}
