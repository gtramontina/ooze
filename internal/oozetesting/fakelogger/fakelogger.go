package fakelogger

import "fmt"

type FakeLogger struct {
	lines []string
}

func New() *FakeLogger {
	return &FakeLogger{
		lines: []string{},
	}
}

func (l *FakeLogger) Logf(message string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(message, args...))
}

func (l *FakeLogger) LoggedLines() []string {
	return l.lines
}

func (l *FakeLogger) Clear() {
	l.lines = []string{}
}
