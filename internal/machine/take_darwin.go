//go:build darwin

package machine

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const supported = true

var (
	lsofPath   = toolPath("/usr/sbin", "lsof")
	psPath     = toolPath("/bin", "ps")
	nettopPath = toolPath("/usr/bin", "nettop")
	scriptPath = toolPath("/usr/bin", "script")
)

func take(ctx context.Context) (Snapshot, error) {
	nettop.ensure(ctx)
	ctx, cancel := commandContext(ctx)
	defer cancel()
	// -b avoids kernel calls that can block; -w drops the warnings -b causes.
	out, err := exec.CommandContext(ctx, lsofPath, "-b", "-w", "-nP", "-iTCP", "-iUDP", "-F", "pcLPntT").Output()
	if err != nil && len(out) == 0 {
		return Snapshot{Taken: time.Now(), Source: "lsof", Supported: true, Error: err.Error()}, fmt.Errorf("lsof: %w", err)
	}
	conns, info := parseLsof(string(out))
	if raw, err := exec.CommandContext(ctx, psPath, "-axo", "pid=,user=,comm=").Output(); err == nil {
		info = withPS(info, parsePS(string(raw)))
		conns = renamed(conns, info)
	}
	source := "lsof"
	if counts, ok := nettop.counts(); ok {
		for pid, b := range counts {
			pi := info[pid]
			pi.bytesIn, pi.bytesOut = b.in, b.out
			info[pid] = pi
		}
		source = "lsof+nettop"
	}
	return assemble(source, conns, info), nil
}
