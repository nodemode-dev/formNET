package machine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestToolPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "netstat.exe"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := toolPath(dir, "netstat.exe"); got != filepath.Join(dir, "netstat.exe") {
		t.Fatalf("present tool: %q", got)
	}
	if got := toolPath(dir, "tasklist.exe"); got != "tasklist.exe" {
		t.Fatalf("missing tool must fall back to the bare name: %q", got)
	}
	if got := toolPath("", "netstat.exe"); got != "netstat.exe" {
		t.Fatalf("no directory: %q", got)
	}
}

func TestCommandContextIsBounded(t *testing.T) {
	ctx, cancel := commandContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > takeTimeout || time.Until(deadline) < takeTimeout-time.Second {
		t.Fatalf("deadline %v ok=%v", deadline, ok)
	}
}
