package fakediffer

import "strings"

type FakeDiffer struct{}

func New() *FakeDiffer {
	return &FakeDiffer{}
}

func (d *FakeDiffer) Diff(a, b string, aData, bData []byte) string {
	return strings.Join([]string{
		"From: " + a,
		"To: " + b,
		"",
		"- " + string(aData),
		"+ " + string(bData),
	}, "\n")
}
