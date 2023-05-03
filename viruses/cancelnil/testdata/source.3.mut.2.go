package source

import (
	"context"
)

func f() {
	_, cancel1 := context.WithCancelCause(context.Background())
	defer cancel1(context.DeadlineExceeded)

	_, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	_, cancel3 := context.WithCancelCause(context.Background())
	defer cancel3(nil)

	_, cancel4 := context.WithCancel(context.Background())
	defer cancel4()

	_, cancel5 := context.WithCancelCause(context.Background())
	defer cancel5(nil)

	_, cancel6 := context.WithCancelCause(context.Background())
	defer cancel6(context.DeadlineExceeded)
}
