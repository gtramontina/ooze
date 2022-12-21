package oozetesting

func AsChannel[T any](data T) <-chan T {
	channel := make(chan T, 1)
	channel <- data

	return channel
}
