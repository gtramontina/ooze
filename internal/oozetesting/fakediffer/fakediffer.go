package fakediffer

import "strings"

type FakeDiffer struct{}

func New() *FakeDiffer {
	return &FakeDiffer{}
}

func (d *FakeDiffer) Diff(leftName, rightName string, leftData, rightData []byte) string {
	return strings.Join([]string{
		"From: " + leftName,
		"To: " + rightName,
		"",
		"- " + string(leftData),
		"+ " + string(rightData),
	}, "\n")
}
