//go:build windows

package machine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const supported = true

func system32() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return ""
	}
	return filepath.Join(root, "System32")
}

var (
	netstatPath  = toolPath(system32(), "netstat.exe")
	tasklistPath = toolPath(system32(), "tasklist.exe")
)

func take(ctx context.Context) (Snapshot, error) {
	ctx, cancel := commandContext(ctx)
	defer cancel()
	var text string
	for _, proto := range []string{"tcp", "udp"} {
		out, err := exec.CommandContext(ctx, netstatPath, "-ano", "-p", proto).Output()
		if err != nil {
			return Snapshot{Taken: time.Now(), Source: "netstat", Supported: true, Error: err.Error()}, fmt.Errorf("netstat: %w", err)
		}
		text += string(out) + "\n"
	}
	conns := parseNetstat(text)
	info := map[int]procInfo{}
	if raw, err := exec.CommandContext(ctx, tasklistPath, "/fo", "csv", "/nh").Output(); err == nil {
		for pid, name := range parseTasklist(string(raw)) {
			info[pid] = procInfo{name: name}
		}
	}
	return assemble("netstat", conns, info), nil
}
