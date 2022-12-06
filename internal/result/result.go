package result

type Result[Type any] interface {
	seal() string
}

func Ok[Type any](value Type) Result[Type] {
	return ok[Type]{value}
}

func Err[Type any](value error) Result[Type] {
	return err[Type]{value}
}
