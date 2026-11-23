package machine

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// ErrUnsupported is returned where no reader exists for this platform.
var ErrUnsupported = errors.New("machine view is not supported on this platform")

// Take reads the connection table once.
func Take(ctx context.Context) (Snapshot, error) {
	return take(ctx)
}

// Listening finds the TCP socket listening on port and the process that
// holds it, when this platform tells this user.
func Listening(ctx context.Context, port string) (Connection, bool) {
	snap, _ := take(ctx)
	for _, c := range snap.Connections {
		if c.State != "LISTEN" || c.PID <= 0 || !strings.HasPrefix(c.Proto, "tcp") {
			continue
		}
		if _, p, err := net.SplitHostPort(c.Local); err == nil && p == port {
			return c, true
		}
	}
	return Connection{}, false
}

func unsupported() (Snapshot, error) {
	return Snapshot{Taken: time.Now(), Source: "none", Supported: false, Error: ErrUnsupported.Error()}, ErrUnsupported
}
