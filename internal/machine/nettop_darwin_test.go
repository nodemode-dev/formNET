//go:build darwin

package machine

import (
	"testing"
	"time"
)

func TestNettopSamplerStaleness(t *testing.T) {
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	now := base
	s := &nettopSampler{now: func() time.Time { return now }}
	if _, ok := s.counts(); ok {
		t.Fatal("no counts before any block")
	}
	s.running = true
	s.started = base
	s.latest = map[int]byteCounts{1: {in: 1, out: 2}}
	s.at = base
	if c, ok := s.counts(); !ok || c[1].out != 2 {
		t.Fatalf("fresh counts: %v %v", c, ok)
	}
	now = base.Add(nettopStale + time.Second)
	if _, ok := s.counts(); ok {
		t.Fatal("counts must be dropped once the sampler goes quiet")
	}
	if !s.stale() {
		t.Fatal("stale must be true after three quiet intervals")
	}
	s.running = false
	now = base
	if !s.stale() || func() bool { _, ok := s.counts(); return ok }() {
		t.Fatal("an exited sampler is stale and has no counts")
	}
	s.running = true
	s.at = time.Time{}
	s.started = base
	if s.stale() {
		t.Fatal("a sampler that just started is not stale before its first block")
	}
}
