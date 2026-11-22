package machine

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"
)

// The snapshot behind Owner is taken every two seconds, so a short request
// (curl, a script) has come and gone between two readings. An Attributor
// asks the system about one client port at the moment the proxy sees it, in
// the background (on Linux /proc, on macOS lsof), and hands the answer back
// when it arrives; the proxy is never made to wait. Only loopback clients are
// asked about: a client on another device has no process on this computer.
const (
	attributeTTL      = 5 * time.Second        // an answer is reused this long for the same port
	attributeMissTTL  = 2 * time.Second        // a port nobody was found for is not asked again before this
	attributeTimeout  = 2 * time.Second        // one lookup at most
	attributeInFlight = 4                      // lookups at once; past that a port is left for the next request
	attributeCacheMax = 4096                   // entries kept before old ones are pruned
	attributeSince    = 30 * time.Second       // how far back an answer names stored flows
	attributeBudget   = 300 * time.Millisecond // a /proc walk stops after this
)

type attributeEntry struct {
	pid     int
	name    string
	found   bool
	expires time.Time
	pending bool
}

// Attributor names the program behind a loopback client port.
type Attributor struct {
	found    func(client string, since time.Time, pid int, name string)
	fallback func(client string) (int, string, bool)
	slots    chan struct{}

	mu    sync.Mutex
	cache map[string]attributeEntry
}

// NewAttributor returns an Attributor that calls found when a background
// lookup names the program behind a client address. When the lookup comes
// too late (the client has exited), fallback (the machine view's latest
// snapshot, which may have been taken meanwhile) is asked; nil skips it. On
// systems without a per-port lookup (Windows and others) it never answers
// and Owner alone is used.
func NewAttributor(found func(client string, since time.Time, pid int, name string), fallback func(client string) (int, string, bool)) *Attributor {
	return &Attributor{found: found, fallback: fallback, slots: make(chan struct{}, attributeInFlight), cache: map[string]attributeEntry{}}
}

// Owner answers from the cache, or starts a background lookup and answers
// false. found is called once that lookup names a program.
func (a *Attributor) Owner(client string) (int, string, bool) {
	if a == nil || !attributes {
		return 0, "", false
	}
	host, portText, err := net.SplitHostPort(client)
	ip := net.ParseIP(host)
	port, perr := strconv.Atoi(portText)
	if err != nil || perr != nil || ip == nil || !ip.IsLoopback() || port <= 0 {
		return 0, "", false
	}
	now := time.Now()
	a.mu.Lock()
	e, ok := a.cache[client]
	if ok && (e.pending || now.Before(e.expires)) {
		a.mu.Unlock()
		return e.pid, e.name, e.found
	}
	select {
	case a.slots <- struct{}{}:
	default:
		a.mu.Unlock()
		return 0, "", false
	}
	if len(a.cache) >= attributeCacheMax {
		for k, old := range a.cache {
			if !old.pending && now.After(old.expires) {
				delete(a.cache, k)
			}
		}
	}
	a.cache[client] = attributeEntry{pending: true}
	a.mu.Unlock()
	go func() {
		defer func() { <-a.slots }()
		ctx, cancel := context.WithTimeout(context.Background(), attributeTimeout)
		pid, name, found := lookupPort(ctx, ip, port)
		cancel()
		if !found && a.fallback != nil {
			pid, name, found = a.fallback(client)
		}
		ttl := attributeMissTTL
		if found {
			ttl = attributeTTL
		}
		a.mu.Lock()
		a.cache[client] = attributeEntry{pid: pid, name: name, found: found, expires: time.Now().Add(ttl)}
		a.mu.Unlock()
		if found && a.found != nil {
			a.found(client, now.Add(-attributeSince), pid, name)
		}
	}()
	return 0, "", false
}
