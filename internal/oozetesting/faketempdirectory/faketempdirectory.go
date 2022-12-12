package faketempdirectory

import "fmt"

type FakeTemporaryDirectory struct {
	prefix  string
	counter int
}

func NewFakeTemporaryDirectory(prefix string) *FakeTemporaryDirectory {
	return &FakeTemporaryDirectory{
		prefix:  prefix,
		counter: 0,
	}
}

func (d *FakeTemporaryDirectory) New() string {
	d.counter++
	temporaryDirectory := fmt.Sprintf("%s-%d", d.prefix, d.counter)

	return temporaryDirectory
}
