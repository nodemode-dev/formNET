package machine

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// LookupFunc answers a reverse DNS query for one IP.
type LookupFunc func(ctx context.Context, ip string) ([]string, error)

// ForwardFunc answers a forward query for one name, to confirm a reverse answer.
type ForwardFunc func(ctx context.Context, host string) ([]string, error)

const (
	resolveTimeout  = 500 * time.Millisecond
	resolveTTL      = 10 * time.Minute
	resolveInFlight = 8
	resolveCacheMax = 4096
)

type resolveEntry struct {
	name     string
	verified bool
	until    time.Time
}

// resolver names remote addresses in the background, remembering answers
// and refusals for a while, and never asks about addresses that are not
// routable on the internet. A reverse answer is forward-confirmed: the name
// is looked up and marked verified only when its addresses include the IP.
type resolver struct {
	lookup  LookupFunc
	forward ForwardFunc
	now     func() time.Time

	mu       sync.Mutex
	cache    map[string]resolveEntry
	inflight map[string]struct{}
	sem      chan struct{}
	wg       sync.WaitGroup
}

func newResolver(lookup LookupFunc, forward ForwardFunc) *resolver {
	if lookup == nil {
		lookup = net.DefaultResolver.LookupAddr
	}
	if forward == nil {
		forward = net.DefaultResolver.LookupHost
	}
	return &resolver{lookup: lookup, forward: forward, now: time.Now, cache: map[string]resolveEntry{}, inflight: map[string]struct{}{}, sem: make(chan struct{}, resolveInFlight)}
}

// name returns the known name for ip and whether it was forward-confirmed,
// or "" while scheduling a lookup.
func (r *resolver) name(ctx context.Context, ip string) (string, bool) {
	parsed := net.ParseIP(ip)
	if parsed == nil || !shouldResolve(parsed) {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.cache[ip]; ok && r.now().Before(e.until) {
		return e.name, e.verified
	}
	if _, busy := r.inflight[ip]; busy {
		return "", false
	}
	select {
	case r.sem <- struct{}{}:
	default:
		return "", false // too many in flight; a later snapshot asks again
	}
	r.inflight[ip] = struct{}{}
	r.wg.Add(1)
	go r.run(ctx, ip)
	return "", false
}

func (r *resolver) run(ctx context.Context, ip string) {
	defer r.wg.Done()
	defer func() { <-r.sem }()
	name, verified := r.query(ctx, ip)
	r.mu.Lock()
	r.cache[ip] = resolveEntry{name: name, verified: verified, until: r.now().Add(resolveTTL)}
	delete(r.inflight, ip)
	r.mu.Unlock()
}

// query does the reverse lookup and then the forward confirmation.
func (r *resolver) query(ctx context.Context, ip string) (string, bool) {
	lctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	names, err := r.lookup(lctx, ip)
	if err != nil || len(names) == 0 {
		return "", false
	}
	name := strings.TrimSuffix(names[0], ".")
	fctx, fcancel := context.WithTimeout(ctx, resolveTimeout)
	defer fcancel()
	addrs, err := r.forward(fctx, name)
	if err != nil {
		return name, false
	}
	want := net.ParseIP(ip)
	for _, a := range addrs {
		if got := net.ParseIP(a); got != nil && got.Equal(want) {
			return name, true
		}
	}
	return name, false
}

// prune drops expired entries and, past the cap, the ones expiring soonest.
func (r *resolver) prune() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for ip, e := range r.cache {
		if !now.Before(e.until) {
			delete(r.cache, ip)
		}
	}
	if len(r.cache) <= resolveCacheMax {
		return
	}
	ips := make([]string, 0, len(r.cache))
	for ip := range r.cache {
		ips = append(ips, ip)
	}
	sort.Slice(ips, func(i, j int) bool { return r.cache[ips[i]].until.Before(r.cache[ips[j]].until) })
	for _, ip := range ips[:len(ips)-resolveCacheMax] {
		delete(r.cache, ip)
	}
}

// shouldResolve is true for addresses that could have a public name.
func shouldResolve(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 {
		return false // carrier-grade NAT, 100.64.0.0/10
	}
	return true
}
