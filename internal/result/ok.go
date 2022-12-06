package result

type ok[Type any] struct {
	value Type
}

func (ok[Type]) seal() string {
	return "Ok"
}
