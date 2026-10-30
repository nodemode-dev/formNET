package machine

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// takeFunc reads one snapshot; Take is the default, tests inject their own.
type takeFunc func(ctx context.Context) (Snapshot, error)

// Monitor takes snapshots on an interval, keeps the latest, carries first-seen
// times forward and names the process behind a local port.
type Monitor struct {
	interval   time.Duration
	log        *slog.Logger
	take       takeFunc
	resolver   *resolver
	reverseDNS atomic.Bool

	mu     sync.Mutex
	latest Snapshot
	owners map[string]Connection
	subs   map[chan Snapshot]struct{}
}

// Options configure a Monitor. The zero value polls every two seconds with
// the default logger and never queries DNS.
type Options struct {
	Interval time.Duration
	Log      *slog.Logger
	// ReverseDNS turns on naming of remote addresses. It is off by default
	// because every PTR query tells the configured resolver which peers this
	// computer talks to.
	ReverseDNS bool
	// Lookup and Forward replace the system resolver, for tests.
	Lookup  LookupFunc
	Forward ForwardFunc
}

// NewMonitor prepares a monitor with reverse DNS off; Run starts it.
func NewMonitor(interval time.Duration, log *slog.Logger) *Monitor {
	return NewMonitorWithOptions(Options{Interval: interval, Log: log})
}

// NewMonitorWithOptions prepares a monitor from opts; Run starts it.
func NewMonitorWithOptions(opts Options) *Monitor {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	m := &Monitor{
		interval: opts.Interval,
		log:      opts.Log,
		take:     Take,
		resolver: newResolver(opts.Lookup, opts.Forward),
		latest:   Snapshot{Supported: supported},
		owners:   map[string]Connection{},
		subs:     map[chan Snapshot]struct{}{},
	}
	m.reverseDNS.Store(opts.ReverseDNS)
	return m
}

// SetReverseDNS turns naming of remote addresses on or off for later snapshots.
func (m *Monitor) SetReverseDNS(on bool) { m.reverseDNS.Store(on) }

// ReverseDNS reports whether remote addresses are being named.
func (m *Monitor) ReverseDNS() bool { return m.reverseDNS.Load() }

// Run takes snapshots until ctx ends. On a platform without a reader it
// publishes one unsupported snapshot and waits for ctx.
func (m *Monitor) Run(ctx context.Context) {
	m.log.Info("machine view started", "interval", m.interval)
	defer m.log.Info("machine view stopped")
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		snap := m.step(ctx)
		if !snap.Supported {
			m.log.Info("machine view unsupported here", "err", snap.Error)
			<-ctx.Done()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// step takes one snapshot, merges it with the previous one and publishes it.
// A reading that fails outright keeps the previous table and records the error.
func (m *Monitor) step(ctx context.Context) Snapshot {
	snap, err := m.take(ctx)
	if err != nil && snap.Error == "" {
		snap.Error = err.Error()
	}
	if snap.Taken.IsZero() {
		snap.Taken = time.Now()
	}
	m.mu.Lock()
	prev := m.latest
	m.mu.Unlock()
	if err != nil && snap.Supported && len(snap.Connections) == 0 && !prev.Taken.IsZero() {
		kept := prev
		kept.Taken = snap.Taken
		kept.Error = snap.Error
		m.publish(kept)
		m.log.Debug("machine snapshot failed; keeping the previous table", "err", snap.Error)
		return kept
	}
	merged := m.merge(ctx, prev, snap)
	m.publish(merged)
	m.log.Debug("machine snapshot", "processes", len(merged.Processes), "connections", len(merged.Connections), "source", merged.Source)
	return merged
}

// merge carries FirstSeen forward and, with reverse DNS on, fills remote
// names from the resolver cache.
func (m *Monitor) merge(ctx context.Context, prev, next Snapshot) Snapshot {
	resolve := m.reverseDNS.Load()
	if resolve {
		m.resolver.prune()
	}
	seen := make(map[string]time.Time, len(prev.Connections))
	for _, c := range prev.Connections {
		seen[connKey(c)] = c.FirstSeen
	}
	conns := make([]Connection, 0, len(next.Connections))
	for _, c := range next.Connections {
		c.LastSeen = next.Taken
		if first, ok := seen[connKey(c)]; ok && !first.IsZero() {
			c.FirstSeen = first
		} else if c.FirstSeen.IsZero() {
			c.FirstSeen = next.Taken
		}
		if resolve && c.Remote != "" && c.RemoteHost == "" {
			if host, _, err := net.SplitHostPort(c.Remote); err == nil {
				c.RemoteHost, c.RemoteHostVerified = m.resolver.name(ctx, host)
			}
		}
		conns = append(conns, c)
	}
	out := next
	out.Connections = conns
	out.Processes = append([]Process(nil), next.Processes...)
	return out
}

func connKey(c Connection) string {
	return c.Proto + "|" + itoa(c.PID) + "|" + c.Local + "|" + c.Remote
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// publish stores the snapshot, rebuilds the owner index and fans out.
func (m *Monitor) publish(snap Snapshot) {
	owners := make(map[string]Connection, len(snap.Connections))
	for _, c := range snap.Connections {
		if key, ok := ownerKey(c.Local); ok {
			if _, dup := owners[key]; !dup || c.Remote != "" {
				owners[key] = c
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latest = snap
	m.owners = owners
	for ch := range m.subs {
		select {
		case ch <- snap:
		default:
		}
	}
}

// Latest is the most recent snapshot, or an empty one before the first.
func (m *Monitor) Latest() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latest
}

// Subscribe delivers every new snapshot until ctx ends; the channel is closed then.
func (m *Monitor) Subscribe(ctx context.Context) <-chan Snapshot {
	ch := make(chan Snapshot, 8)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		delete(m.subs, ch)
		close(ch)
		m.mu.Unlock()
	}()
	return ch
}

// Owner names the process that holds a local ip:port, for the proxy to label its client.
func (m *Monitor) Owner(localAddr string) (pid int, name string, ok bool) {
	key, valid := ownerKey(localAddr)
	if !valid {
		return 0, "", false
	}
	m.mu.Lock()
	c, found := m.owners[key]
	m.mu.Unlock()
	if !found {
		return 0, "", false
	}
	return c.PID, c.Process, true
}

// ownerKey folds every loopback spelling into one so 127.0.0.1, ::1 and
// ::ffff:127.0.0.1 with the same port meet.
func ownerKey(addr string) (string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", false
	}
	if host == "localhost" {
		return "lo|" + port, true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host + "|" + port, true
	}
	if ip.IsLoopback() {
		return "lo|" + port, true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "|" + port, true
	}
	return ip.String() + "|" + port, true
}
