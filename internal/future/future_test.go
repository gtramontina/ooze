package future_test

import (
	"sync"
	"testing"
	"time"

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
	t.Run("defers resolution", func(t *testing.T) {
		fut := future.Deferred[string]()

		go func() { time.Sleep(100 * time.Millisecond); fut.Resolve("ok") }()

		assert.Eventually(t, func() bool { return fut.Await() == "ok" }, time.Second, time.Microsecond)
	})

	t.Run("resolves only once", func(t *testing.T) {
		fut := future.Deferred[int]()

		go func() { time.Sleep(10 * time.Millisecond); fut.Resolve(1) }()
		go func() { time.Sleep(20 * time.Millisecond); fut.Resolve(10) }()

		assert.Eventually(t, func() bool { return fut.Await() == 1 }, time.Second, time.Microsecond)

		fut.Resolve(100)
		assert.Equal(t, 1, fut.Await())
	})

	t.Run("allows for concurrent awaits", func(t *testing.T) {
		fut := future.Deferred[int]()

		group := sync.WaitGroup{}
		group.Add(3)
		go func() { defer group.Done(); assert.Equal(t, 10, fut.Await()) }()
		go func() { defer group.Done(); assert.Equal(t, 10, fut.Await()) }()
		go func() { defer group.Done(); assert.Equal(t, 10, fut.Await()) }()

		time.Sleep(10 * time.Millisecond)
		fut.Resolve(10)

		assert.Eventually(t, func() bool {
			group.Wait()

			return true
		}, time.Second, time.Microsecond)
	})

	t.Run("only one concurrent resolve calls win", func(t *testing.T) {
		for range 1000 { // arbitrary number of repetitions to try and catch concurrency issues
			fut := future.Deferred[int]()

			go func() { fut.Resolve(10) }()
			go func() { fut.Resolve(100) }()
			go func() { fut.Resolve(1000) }()

			assert.Eventually(t, func() bool { return fut.Await()%10 == 0 }, time.Second, time.Microsecond)
		}
	})
}
