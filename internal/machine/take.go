package machine

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported is returned where no reader exists for this platform.
var ErrUnsupported = errors.New("machine view is not supported on this platform")

// Take reads the connection table once.
func Take(ctx context.Context) (Snapshot, error) {
	return take(ctx)
}

func unsupported() (Snapshot, error) {
	return Snapshot{Taken: time.Now(), Source: "none", Supported: false, Error: ErrUnsupported.Error()}, ErrUnsupported
}
