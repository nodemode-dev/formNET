package machine

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// toolPath returns the absolute path of a system tool when that file exists,
// otherwise the bare name (and PATH decides). Readers name their tools by
// absolute path so a program earlier on PATH cannot stand in for them.
func toolPath(dir, name string) string {
	if dir == "" {
		return name
	}
	full := filepath.Join(dir, name)
	if _, err := os.Stat(full); err != nil {
		return name
	}
	return full
}

// takeTimeout bounds one reading of the table, so a hung tool cannot stop the monitor.
const takeTimeout = 10 * time.Second

// commandContext derives the context the platform tools run under.
func commandContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, takeTimeout)
}
