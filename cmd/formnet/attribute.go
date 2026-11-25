package main

import (
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/machine"
)

// clientOwner names the program behind a proxy client: from a background
// lookup of that one port (started when the proxy accepts the connection)
// whose answer names the stored flows when it comes, and meanwhile, or when
// it comes too late, from the machine view's latest snapshot.
func clientOwner(monitor *machine.Monitor, store *flow.Store) func(string) (int, string, bool) {
	lookup := machine.NewAttributor(func(client string, since time.Time, pid int, name string) { store.Attribute(client, since, pid, name) }, monitor.Owner)
	return func(client string) (int, string, bool) {
		if pid, name, ok := lookup.Owner(client); ok {
			return pid, name, ok
		}
		return monitor.Owner(client)
	}
}
