package result

import (
	"fmt"
	"reflect"
)

type ok[Type any] struct {
	value Type
}

func (ok[Type]) seal() string {
	return "Ok"
}

func (o ok[Type]) String() string {
	kind := reflect.TypeOf(o.value).String()

	return o.seal() + "[" + kind + "](" + fmt.Sprintf("%+v", o.value) + ")"
}

func (ok[Type]) And(and Result[Type]) Result[Type] {
	return and
}

func (ok[Type]) IsOk() bool {
	return true
}
