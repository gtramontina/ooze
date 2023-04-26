//go:build testdata

package source

import (
	"context"
)

func f() {
	_, cancel := context.WithCancelCause(context.Background())
	defer cancel(context.DeadlineExceeded)
}
