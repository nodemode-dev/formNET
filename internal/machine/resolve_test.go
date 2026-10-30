package machine

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolverCachesAndSkipsPrivate(t *testing.T) {
	var calls atomic.Int32
	lookup := func(ctx context.Context, ip string) ([]string, error) {
		calls.Add(1)
		if ip == "8.8.8.8" {
			return []string{"dns.google."}, nil
		}
		return nil, errors.New("no name")
	}
	r := newResolver(lookup, func(ctx context.Context, host string) ([]string, error) { return []string{"8.8.8.8"}, nil })
	ctx := context.Background()
	for _, private := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.5", "172.16.9.9", "fe80::1", "::1", "100.64.3.3", "224.0.0.251", "0.0.0.0"} {
		if got, _ := r.name(ctx, private); got != "" {
			t.Fatalf("%s resolved to %q", private, got)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("private addresses were looked up %d times", calls.Load())
	}
	if got, _ := r.name(ctx, "8.8.8.8"); got != "" {
		t.Fatalf("first ask should schedule, got %q", got)
	}
	r.name(ctx, "8.8.8.8") // in flight: no second lookup
	r.name(ctx, "1.1.1.1")
	r.wg.Wait()
	if got, verified := r.name(ctx, "8.8.8.8"); got != "dns.google" || !verified {
		t.Fatalf("cached name: %q verified %v", got, verified)
	}
	if got, _ := r.name(ctx, "1.1.1.1"); got != "" {
		t.Fatalf("negative cache: %q", got)
	}
	r.wg.Wait()
	if calls.Load() != 2 {
		t.Fatalf("lookups: %d, want 2 (one per address, cached after)", calls.Load())
	}
	r.now = func() time.Time { return time.Now().Add(resolveTTL + time.Second) }
	r.name(ctx, "8.8.8.8")
	r.wg.Wait()
	if calls.Load() != 3 {
		t.Fatalf("expired entry must be looked up again: %d", calls.Load())
	}
}

func TestResolverLimitsInFlight(t *testing.T) {
	release := make(chan struct{})
	lookup := func(ctx context.Context, ip string) ([]string, error) {
		<-release
		return []string{ip + ".example."}, nil
	}
	r := newResolver(lookup, func(ctx context.Context, host string) ([]string, error) { return nil, errors.New("no forward") })
	ctx := context.Background()
	for i := 0; i < resolveInFlight+3; i++ {
		r.name(ctx, net.IPv4(203, 0, 113, byte(i+1)).String())
	}
	r.mu.Lock()
	inflight := len(r.inflight)
	r.mu.Unlock()
	if inflight != resolveInFlight {
		t.Fatalf("in flight: %d, want %d", inflight, resolveInFlight)
	}
	close(release)
	r.wg.Wait()
	if got, verified := r.name(ctx, "203.0.113.1"); got != "203.0.113.1.example" || verified {
		t.Fatalf("after release: %q verified %v (forward failed, so unverified)", got, verified)
	}
}

func TestResolverForwardConfirmation(t *testing.T) {
	lookup := func(ctx context.Context, ip string) ([]string, error) { return []string{"claimed.example."}, nil }
	forward := func(ctx context.Context, host string) ([]string, error) {
		if host != "claimed.example" {
			t.Errorf("forward asked for %q", host)
		}
		return []string{"2001:db8::1", "198.51.100.7"}, nil
	}
	r := newResolver(lookup, forward)
	ctx := context.Background()
	r.name(ctx, "198.51.100.7")
	r.name(ctx, "198.51.100.8")
	r.wg.Wait()
	if name, ok := r.name(ctx, "198.51.100.7"); name != "claimed.example" || !ok {
		t.Fatalf("matching forward: %q %v", name, ok)
	}
	if name, ok := r.name(ctx, "198.51.100.8"); name != "claimed.example" || ok {
		t.Fatalf("name whose forward does not include the ip must be kept but unverified: %q %v", name, ok)
	}
}

func TestResolverPrune(t *testing.T) {
	r := newResolver(func(ctx context.Context, ip string) ([]string, error) { return nil, errors.New("x") }, nil)
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return base }
	for i := 0; i < resolveCacheMax+100; i++ {
		ip := net.IPv4(198, 51, byte(i>>8), byte(i)).String()
		r.cache[ip] = resolveEntry{until: base.Add(time.Duration(i) * time.Second)}
	}
	r.cache["expired"] = resolveEntry{until: base.Add(-time.Second)}
	r.prune()
	if _, ok := r.cache["expired"]; ok {
		t.Fatal("expired entry kept")
	}
	if len(r.cache) != resolveCacheMax {
		t.Fatalf("cache size after prune: %d", len(r.cache))
	}
	if _, ok := r.cache[net.IPv4(198, 51, 0, 1).String()]; ok {
		t.Fatal("the soonest-expiring entries must go first")
	}
	last := resolveCacheMax + 99
	if _, ok := r.cache[net.IPv4(198, 51, byte(last>>8), byte(last)).String()]; !ok {
		t.Fatal("the latest-expiring entry must stay")
	}
}
