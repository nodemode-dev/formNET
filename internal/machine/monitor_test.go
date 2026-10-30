package machine

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func fakeTake(snaps ...Snapshot) (takeFunc, *atomic.Int32) {
	var n atomic.Int32
	return func(ctx context.Context) (Snapshot, error) {
		i := int(n.Add(1)) - 1
		if i >= len(snaps) {
			i = len(snaps) - 1
		}
		s := snaps[i]
		s.Taken = time.Now()
		return s, nil
	}, &n
}

func TestMonitorCarriesFirstSeenAndOwner(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Second)
	first := Snapshot{Supported: true, Source: "fake", Taken: t0,
		Connections: []Connection{
			{Proto: "tcp", PID: 42, Process: "curl", Local: "127.0.0.1:50000", Remote: "127.0.0.1:8761", State: "ESTABLISHED"},
			{Proto: "tcp6", PID: 7, Process: "node", Local: "[::1]:50001", Remote: "[::1]:8761", State: "ESTABLISHED"},
			{Proto: "tcp", PID: 42, Process: "curl", Local: "192.168.0.5:50002", Remote: "203.0.113.9:443", State: "ESTABLISHED"},
		},
		Processes: []Process{{PID: 42, Name: "curl", Connections: 2}, {PID: 7, Name: "node", Connections: 1}},
	}
	second := first
	second.Taken = t1
	second.Connections = []Connection{first.Connections[0], {Proto: "tcp", PID: 99, Process: "new", Local: "127.0.0.1:50003", Remote: "127.0.0.1:8761", State: "ESTABLISHED"}}

	lookups := make(chan string, 8)
	m := NewMonitorWithOptions(Options{Interval: time.Hour, ReverseDNS: true,
		Lookup: func(ctx context.Context, ip string) ([]string, error) {
			lookups <- ip
			return []string{"nine.example."}, nil
		},
		Forward: func(ctx context.Context, host string) ([]string, error) { return []string{"203.0.113.9"}, nil },
	})
	m.take = func(ctx context.Context) (Snapshot, error) { return first, nil }
	if l := m.Latest(); !l.Taken.IsZero() || l.Supported != supported {
		t.Fatalf("before first snapshot: %+v", l)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := m.Subscribe(ctx)

	s1 := m.step(ctx)
	if len(s1.Connections) != 3 || !s1.Connections[0].FirstSeen.Equal(t0) || !s1.Connections[0].LastSeen.Equal(t0) {
		t.Fatalf("first step: %+v", s1.Connections)
	}
	select {
	case got := <-ch:
		if got.Taken != t0 {
			t.Fatalf("subscriber got %+v", got.Taken)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber received nothing")
	}
	select {
	case ip := <-lookups:
		if ip != "203.0.113.9" {
			t.Fatalf("looked up %s", ip)
		}
	case <-time.After(time.Second):
		t.Fatal("public remote was not resolved")
	}
	for _, addr := range []string{"127.0.0.1:50000", "localhost:50000", "[::1]:50000", "[::ffff:127.0.0.1]:50000"} {
		pid, name, ok := m.Owner(addr)
		if !ok || pid != 42 || name != "curl" {
			t.Fatalf("Owner(%s) = %d %q %v", addr, pid, name, ok)
		}
	}
	if pid, _, ok := m.Owner("127.0.0.1:50001"); !ok || pid != 7 {
		t.Fatalf("ipv6 loopback owner: %d %v", pid, ok)
	}
	if _, _, ok := m.Owner("127.0.0.1:1"); ok {
		t.Fatal("unknown port must not resolve")
	}
	if _, _, ok := m.Owner("garbage"); ok {
		t.Fatal("garbage must not resolve")
	}

	m.take = func(ctx context.Context) (Snapshot, error) { return second, nil }
	s2 := m.step(ctx)
	if len(s2.Connections) != 2 {
		t.Fatalf("second step: %+v", s2.Connections)
	}
	if !s2.Connections[0].FirstSeen.Equal(t0) || !s2.Connections[0].LastSeen.Equal(t1) {
		t.Fatalf("carried connection: first %v last %v", s2.Connections[0].FirstSeen, s2.Connections[0].LastSeen)
	}
	if !s2.Connections[1].FirstSeen.Equal(t1) {
		t.Fatalf("new connection first seen: %v", s2.Connections[1].FirstSeen)
	}
	if _, _, ok := m.Owner("127.0.0.1:50001"); ok {
		t.Fatal("owner index must be rebuilt per snapshot")
	}
	if _, _, ok := m.Owner("127.0.0.1:50003"); !ok {
		t.Fatal("new owner missing")
	}
	// The published first snapshot was not changed by the second step.
	if !s1.Connections[0].LastSeen.Equal(t0) {
		t.Fatal("earlier snapshot was mutated")
	}
	cancel()
	closed := false
	deadline := time.After(time.Second)
	for !closed {
		select {
		case _, open := <-ch:
			closed = !open
		case <-deadline:
			t.Fatal("channel must close when the subscriber's context ends")
		}
	}
	m.step(context.Background()) // no panic with the subscriber gone
}

func TestMonitorRunPollsAndStopsWhenUnsupported(t *testing.T) {
	take, calls := fakeTake(Snapshot{Supported: true, Source: "fake"})
	m := NewMonitor(20*time.Millisecond, slog.Default())
	m.take = take
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	m.Run(ctx)
	if n := calls.Load(); n < 3 {
		t.Fatalf("snapshots taken: %d", n)
	}
	if m.Latest().Source != "fake" {
		t.Fatalf("latest: %+v", m.Latest())
	}

	m2 := NewMonitor(10*time.Millisecond, slog.Default())
	var n atomic.Int32
	m2.take = func(ctx context.Context) (Snapshot, error) { n.Add(1); return unsupported() }
	ctx2, cancel2 := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel2()
	m2.Run(ctx2)
	if n.Load() != 1 {
		t.Fatalf("an unsupported platform must be tried once, got %d", n.Load())
	}
	if l := m2.Latest(); l.Supported || l.Error == "" {
		t.Fatalf("latest after unsupported: %+v", l)
	}
}

func TestMonitorKeepsPartialSnapshotWithError(t *testing.T) {
	m := NewMonitor(time.Hour, slog.Default())
	m.take = func(ctx context.Context) (Snapshot, error) {
		return Snapshot{Supported: true, Source: "lsof"}, context.DeadlineExceeded
	}
	s := m.step(context.Background())
	if !s.Supported || s.Error == "" || s.Taken.IsZero() {
		t.Fatalf("%+v", s)
	}
}

func TestMonitorFailedTakeKeepsPreviousTable(t *testing.T) {
	t0 := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	good := Snapshot{Supported: true, Source: "fake", Taken: t0,
		Connections: []Connection{{Proto: "tcp", PID: 42, Process: "curl", Local: "127.0.0.1:50000", Remote: "127.0.0.1:8761", State: "ESTABLISHED"}},
		Processes:   []Process{{PID: 42, Name: "curl", Connections: 1}},
	}
	var n atomic.Int32
	m := NewMonitor(time.Hour, slog.Default())
	m.take = func(ctx context.Context) (Snapshot, error) {
		switch n.Add(1) {
		case 2:
			return Snapshot{Supported: true, Source: "fake", Taken: t0.Add(2 * time.Second)}, context.DeadlineExceeded
		default:
			g := good
			g.Taken = t0.Add(time.Duration(n.Load()-1) * 2 * time.Second)
			return g, nil
		}
	}
	ctx := context.Background()
	m.step(ctx)
	failed := m.step(ctx)
	if failed.Error == "" || len(failed.Connections) != 1 || len(failed.Processes) != 1 || !failed.Taken.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("failed step must keep the table: %+v", failed)
	}
	if pid, _, ok := m.Owner("127.0.0.1:50000"); !ok || pid != 42 {
		t.Fatalf("owner lost across a failed take: %d %v", pid, ok)
	}
	third := m.step(ctx)
	if third.Error != "" || !third.Connections[0].FirstSeen.Equal(t0) || !third.Connections[0].LastSeen.Equal(t0.Add(4*time.Second)) {
		t.Fatalf("first seen must survive a failed take: %+v", third.Connections[0])
	}
}

func TestMonitorReverseDNSOffByDefault(t *testing.T) {
	var calls atomic.Int32
	snap := Snapshot{Supported: true, Source: "fake",
		Connections: []Connection{{Proto: "tcp", PID: 1, Process: "a", Local: "192.168.0.5:1", Remote: "203.0.113.9:443", State: "ESTABLISHED"}}}
	m := NewMonitorWithOptions(Options{Interval: time.Hour,
		Lookup: func(ctx context.Context, ip string) ([]string, error) {
			calls.Add(1)
			return []string{"nine.example."}, nil
		},
		Forward: func(ctx context.Context, host string) ([]string, error) { return []string{"203.0.113.9"}, nil },
	})
	m.take = func(ctx context.Context) (Snapshot, error) { return snap, nil }
	if m.ReverseDNS() {
		t.Fatal("reverse DNS must be off by default")
	}
	ctx := context.Background()
	s := m.step(ctx)
	m.resolver.wg.Wait()
	if calls.Load() != 0 || s.Connections[0].RemoteHost != "" {
		t.Fatalf("lookups with reverse DNS off: %d, host %q", calls.Load(), s.Connections[0].RemoteHost)
	}
	m.SetReverseDNS(true)
	m.step(ctx)
	m.resolver.wg.Wait()
	s = m.step(ctx)
	if calls.Load() != 1 || s.Connections[0].RemoteHost != "nine.example" || !s.Connections[0].RemoteHostVerified {
		t.Fatalf("after turning it on: calls %d, %+v", calls.Load(), s.Connections[0])
	}
}
