package machine

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestTakeOnThisPlatform runs the real reader where one exists and checks the shape.
func TestTakeOnThisPlatform(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	snap, err := Take(ctx)
	if errors.Is(err, ErrUnsupported) {
		t.Skip("no reader on this platform")
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.Is(err, exec.ErrNotFound) || errors.As(err, &exitErr) || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "permission") {
			t.Skipf("reader tool unavailable here: %v", err)
		}
		t.Fatal(err)
	}
	if !snap.Supported || snap.Taken.IsZero() || snap.Source == "" {
		t.Fatalf("header: supported=%v taken=%v source=%q", snap.Supported, snap.Taken, snap.Source)
	}
	for _, c := range snap.Connections {
		if c.Proto == "" || c.Local == "" || c.State == "" {
			t.Fatalf("incomplete connection: %+v", c)
		}
	}
	for _, p := range snap.Processes {
		if p.PID < 0 || p.Connections == 0 {
			t.Fatalf("odd process: %+v", p)
		}
	}
	t.Logf("%s: %d processes, %d connections", snap.Source, len(snap.Processes), len(snap.Connections))
}
