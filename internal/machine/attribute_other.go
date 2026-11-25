//go:build !linux && !darwin

package machine

import (
	"context"
	"net"
)

// attributes is false here: Windows names a client from the netstat
// snapshot only (Owner), and other systems have no machine view.
const attributes = false

func lookupPort(ctx context.Context, ip net.IP, port int) (int, string, bool) { return 0, "", false }
