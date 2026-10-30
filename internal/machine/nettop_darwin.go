//go:build darwin

package machine

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// nettop needs about five seconds before its first sample, so one long
// logging run feeds every snapshot instead of a fresh run per snapshot; the
// first snapshots carry no byte counts and later ones pick them up. nettop
// block-buffers its output on a pipe, so it runs under a pseudo-terminal
// (script -q /dev/null) when that tool exists; the pty makes it line-buffered.
const (
	nettopInterval = "2"
	nettopPeriod   = 2 * time.Second
	// nettopStale is how long without a new block before the counts are
	// dropped and the sampler is started again.
	nettopStale = 3 * nettopPeriod
)

func nettopCommand(ctx context.Context) *exec.Cmd {
	args := []string{nettopPath, "-P", "-L", "0", "-s", nettopInterval, "-x", "-J", "bytes_in,bytes_out"}
	var cmd *exec.Cmd
	if filepath.IsAbs(scriptPath) {
		cmd = exec.CommandContext(ctx, scriptPath, append([]string{"-q", "/dev/null"}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, args[0], args[1:]...)
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

type nettopSampler struct {
	now func() time.Time

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	started time.Time
	latest  map[int]byteCounts
	at      time.Time
}

var nettop = &nettopSampler{now: time.Now}

// stale reports whether the sampler has gone quiet: not running, or no block
// for nettopStale after it started or after its last block.
func (s *nettopSampler) stale() bool {
	if !s.running {
		return true
	}
	last := s.at
	if last.IsZero() {
		last = s.started
	}
	return s.now().Sub(last) > nettopStale
}

// ensure starts the sampler with ctx when it is not running, and restarts a
// running one that has gone quiet.
func (s *nettopSampler) ensure(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		if !s.stale() {
			return
		}
		if s.cancel != nil {
			s.cancel()
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.running = true
	s.cancel = cancel
	s.started = s.now()
	s.latest = nil
	s.at = time.Time{}
	go s.run(runCtx)
}

func (s *nettopSampler) run(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.running = false
		s.mu.Unlock()
	}()
	cmd := nettopCommand(ctx)
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return
	}
	nettopBlocks(out, func(counts map[int]byteCounts) {
		s.mu.Lock()
		if ctx.Err() == nil {
			s.latest = counts
			s.at = s.now()
		}
		s.mu.Unlock()
	})
	_ = cmd.Wait()
}

// counts returns the latest sample without waiting; before the first one,
// and once the sampler has gone stale, there is none.
func (s *nettopSampler) counts() (map[int]byteCounts, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.latest == nil || s.stale() {
		return nil, false
	}
	out := make(map[int]byteCounts, len(s.latest))
	for k, v := range s.latest {
		out[k] = v
	}
	return out, true
}
