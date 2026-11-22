package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/nodemode-dev/formnet/internal/app"
	"github.com/nodemode-dev/formnet/internal/config"
	"github.com/nodemode-dev/formnet/internal/mcp"
)

// mcpCommand serves MCP on stdin and stdout. Only protocol messages reach
// stdout; everything else goes to stderr.
func mcpCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintf(stderr, "formnet mcp takes no arguments (got %q); FORMNET_DATA chooses the data folder\n", args[0])
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(stderr, "formnet mcp:", err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(stderr, "formnet mcp: protocol %s on stdio, data folder %s\n", mcp.Modern, cfg.Data)
	if err := mcp.New(cfg.Data, app.Version, stderr).Serve(ctx, stdin, stdout); err != nil && ctx.Err() == nil {
		fmt.Fprintln(stderr, "formnet mcp:", err)
		return 1
	}
	return 0
}
