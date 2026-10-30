//go:build !darwin && !linux && !windows

package machine

import "context"

const supported = false

func take(ctx context.Context) (Snapshot, error) { return unsupported() }
