package result

type err[Type any] struct {
	value error
}

func (err[Type]) seal() string {
	return "Err"
}
