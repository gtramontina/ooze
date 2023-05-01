//go:build testdata

package source

import (
	"context"
)

func f() {
	_, cancel1 := context.WithCancelCause(context.Background())
	defer cancel1(nil)

	_, cancel2 := context.WithCancelCause(context.Background())
	defer cancel2(context.DeadlineExceeded)
}
