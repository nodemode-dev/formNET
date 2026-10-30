package machine

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrUnsupported is returned where no reader exists for this platform.
var ErrUnsupported = errors.New("machine view is not supported on this platform")

// Take reads the connection table once.
func Take(ctx context.Context) (Snapshot, error) {
	return Snapshot{Taken: time.Now(), Source: "none", Supported: false, Error: ErrUnsupported.Error()}, ErrUnsupported
}

// Monitor takes snapshots on an interval and keeps the latest.
type Monitor struct {
	interval time.Duration
	log      *slog.Logger
}

// NewMonitor prepares a monitor; Run starts it.
func NewMonitor(interval time.Duration, log *slog.Logger) *Monitor {
	return &Monitor{interval: interval, log: log}
}

// Run takes snapshots until ctx ends.
func (m *Monitor) Run(ctx context.Context) { <-ctx.Done() }

// Latest is the most recent snapshot, or an empty unsupported one before the first.
func (m *Monitor) Latest() Snapshot {
	return Snapshot{Taken: time.Now(), Source: "none", Error: ErrUnsupported.Error()}
}

// Subscribe delivers every new snapshot until ctx ends.
func (m *Monitor) Subscribe(ctx context.Context) <-chan Snapshot {
	ch := make(chan Snapshot)
	go func() { <-ctx.Done(); close(ch) }()
	return ch
}

// Owner names the process that holds a local ip:port, for the proxy to label its client.
func (m *Monitor) Owner(localAddr string) (pid int, name string, ok bool) { return 0, "", false }
