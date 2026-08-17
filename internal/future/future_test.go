package future_test

import (
	"testing"

	"github.com/gtramontina/ooze/internal/future"
	"github.com/stretchr/testify/assert"
)

func TestResolved(t *testing.T) {
	t.Run("is always resolved", func(t *testing.T) {
		fut := future.Resolved("ok")
		assert.Equal(t, "ok", fut.Await())
		assert.Equal(t, "ok", fut.Await())
		assert.Equal(t, "ok", fut.Await())
	})
}

func TestDeferred(t *testing.T) {
	t.Run("awaits resolution", func(t *testing.T) {
		fut := future.Deferred[string]()
		resolved := make(chan string, 1)

		go func() { resolved <- fut.Await() }()
		fut.Resolve("ok")

		assert.Equal(t, "ok", <-resolved)
	})

	t.Run("resolves only once", func(t *testing.T) {
		fut := future.Deferred[int]()

		fut.Resolve(1)
		fut.Resolve(10)
		fut.Resolve(100)

		assert.Equal(t, 1, fut.Await())
	})

	t.Run("allows for concurrent awaits", func(t *testing.T) {
		fut := future.Deferred[int]()
		results := make(chan int, 3)

		go func() { results <- fut.Await() }()
		go func() { results <- fut.Await() }()
		go func() { results <- fut.Await() }()
		fut.Resolve(10)

		assert.Equal(t, 10, <-results)
		assert.Equal(t, 10, <-results)
		assert.Equal(t, 10, <-results)
	})

	t.Run("only one concurrent resolve call wins", func(t *testing.T) {
		for range 1000 { // arbitrary number of repetitions to try and catch concurrency issues
			fut := future.Deferred[int]()
			start := make(chan struct{})

			go func() { <-start; fut.Resolve(10) }()
			go func() { <-start; fut.Resolve(100) }()
			go func() { <-start; fut.Resolve(1000) }()
			close(start)

			assert.Contains(t, []int{10, 100, 1000}, fut.Await())
		}
	})
}
