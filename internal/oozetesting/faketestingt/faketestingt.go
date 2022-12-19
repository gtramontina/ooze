package faketestingt

import (
	"fmt"
	"testing"
)

type FakeTestingT struct {
	helperCalls int
	subtests    map[string]func(*testing.T)
	logOutput   []string
}

func New() *FakeTestingT {
	return &FakeTestingT{
		helperCalls: 0,
		subtests:    map[string]func(*testing.T){},
		logOutput:   []string{},
	}
}

func (t *FakeTestingT) Helper() {
	t.helperCalls++
}

func (t *FakeTestingT) HelperCalls() int {
	return t.helperCalls
}

func (t *FakeTestingT) Run(name string, fn func(*testing.T)) bool {
	t.subtests[name] = fn

	return true
}

func (t *FakeTestingT) GetSubtest(name string) *SubTest {
	subtest, found := t.subtests[name]
	if !found {
		return nil
	}

	return NewSubTest(subtest)
}

func (t *FakeTestingT) Logf(format string, args ...any) {
	t.logOutput = append(t.logOutput, fmt.Sprintf(format, args...))
}

func (t *FakeTestingT) LogOutput() []string {
	return t.logOutput
}
