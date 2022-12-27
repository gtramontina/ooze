package iologger

import (
	"fmt"
	"io"
)

type IOLogger struct {
	writer io.Writer
}

func New(writer io.Writer) *IOLogger {
	return &IOLogger{
		writer: writer,
	}
}

func (l *IOLogger) Logf(message string, args ...any) {
	_, err := fmt.Fprintf(l.writer, message+"\n", args...)
	if err != nil {
		panic(err)
	}
}
