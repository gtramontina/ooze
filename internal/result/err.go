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

func (e err[Type]) And(_ Result[Type]) Result[Type] {
	return Err[Type](e.errorMessage)
}
