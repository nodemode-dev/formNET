//go:build linux

package machine

import "context"

const supported = true

func take(ctx context.Context) (Snapshot, error) {
	conns, info, err := readProc("/proc")
	if err != nil {
		return Snapshot{Source: "proc", Supported: true, Error: err.Error()}, err
	}
	return assemble("proc", conns, info), nil
}
