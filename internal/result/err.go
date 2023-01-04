package result

import "fmt"

type err[Type any] struct {
	errorMessage string
}

func (err[Type]) seal() string {
	return "Err"
}

func (e err[Type]) String() string {
	return e.seal() + "(" + fmt.Sprintf("%+v", e.errorMessage) + ")"
}

func (err[Type]) IsOk() bool {
	return false
}
