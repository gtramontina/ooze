package future

import "sync"

type DeferredFuture[T any] struct {
	resolved bool
	mutex    sync.Mutex
	once     sync.Once
	channel  chan T
	value    T
}

func (f *DeferredFuture[T]) Await() T {
	defer f.mutex.Unlock()
	f.mutex.Lock()

	if !f.resolved {
		f.value = <-f.channel
		f.resolved = true
	}

	return f.value
}

func (f *DeferredFuture[T]) Resolve(value T) {
	f.once.Do(func() {
		go func() {
			f.channel <- value
			close(f.channel)
		}()
	})
}
