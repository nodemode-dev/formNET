// Package throttle slows the proxy's traffic to a chosen network: a rate
// each way, kept with one token bucket per direction shared by every
// throttled connection (as one slow link would be), and a latency added once
// per request or per new tunnel. An optional host list, matched with the
// rules matcher, limits it to those hosts.
package throttle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nodemode-dev/formnet/internal/rules"
)

// Preset is a named network.
type Preset struct {
	DownKbps  int `json:"down_kbps"`
	UpKbps    int `json:"up_kbps"`
	LatencyMS int `json:"latency_ms"`
}

// Presets are the named networks; custom takes the numbers given.
var Presets = map[string]Preset{
	"3g":        {DownKbps: 750, UpKbps: 250, LatencyMS: 100},
	"4g":        {DownKbps: 4000, UpKbps: 3000, LatencyMS: 20},
	"slow-wifi": {DownKbps: 1500, UpKbps: 750, LatencyMS: 40},
}

// Custom is the preset name that takes the numbers given.
const Custom = "custom"

// Config is the throttle setting. Rates are kilobits per second (1000 bits);
// zero is no limit that way.
type Config struct {
	Enabled   bool     `json:"enabled"`
	Preset    string   `json:"preset"`
	DownKbps  int      `json:"down_kbps"`
	UpKbps    int      `json:"up_kbps"`
	LatencyMS int      `json:"latency_ms"`
	Hosts     []string `json:"hosts"` // empty: every host
}

// Status is what the API reports: the setting and the presets offered.
type Status struct {
	Config
	Presets map[string]Preset `json:"presets"`
}

// Defaults is the first start: off, 3g chosen.
func Defaults() Config {
	p := Presets["3g"]
	return Config{Preset: "3g", DownKbps: p.DownKbps, UpKbps: p.UpKbps, LatencyMS: p.LatencyMS, Hosts: []string{}}
}

// Normalize checks c and fills a named preset's numbers.
func Normalize(c Config) (Config, []*rules.Matcher, error) {
	c.Preset = strings.ToLower(strings.TrimSpace(c.Preset))
	if c.Preset == "" {
		c.Preset = Custom
	}
	if p, ok := Presets[c.Preset]; ok {
		c.DownKbps, c.UpKbps, c.LatencyMS = p.DownKbps, p.UpKbps, p.LatencyMS
	} else if c.Preset != Custom {
		return c, nil, &rules.FieldError{Field: "preset", Reason: "must be 3g, 4g, slow-wifi or custom"}
	}
	if c.DownKbps < 0 || c.DownKbps > 10_000_000 {
		return c, nil, &rules.FieldError{Field: "down_kbps", Reason: "must be between 0 (no limit) and 10000000"}
	}
	if c.UpKbps < 0 || c.UpKbps > 10_000_000 {
		return c, nil, &rules.FieldError{Field: "up_kbps", Reason: "must be between 0 (no limit) and 10000000"}
	}
	if c.LatencyMS < 0 || c.LatencyMS > 10_000 {
		return c, nil, &rules.FieldError{Field: "latency_ms", Reason: "must be between 0 and 10000"}
	}
	if len(c.Hosts) > 200 {
		return c, nil, &rules.FieldError{Field: "hosts", Reason: "at most 200 entries"}
	}
	hosts := make([]string, 0, len(c.Hosts))
	var ms []*rules.Matcher
	for i, h := range c.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		m, err := rules.Compile(rules.Match{Host: h}, fmt.Sprintf("hosts[%d]", i))
		if err != nil {
			return c, nil, err
		}
		hosts = append(hosts, h)
		ms = append(ms, m)
	}
	c.Hosts = hosts
	return c, ms, nil
}

// Shaper applies the current setting.
type Shaper struct {
	mu  sync.Mutex
	cur atomic.Pointer[shaping]
}

type shaping struct {
	cfg      Config
	hosts    []*rules.Matcher
	down, up *bucket // nil: no limit that way
}

// New starts a shaper with c; an invalid c gives the defaults and the error.
func New(c Config) (*Shaper, error) {
	s := &Shaper{}
	if _, err := s.Set(c); err != nil {
		_, _ = s.Set(Defaults())
		return s, err
	}
	return s, nil
}

// Set puts c in force for new reads and writes; it returns the normalized setting.
func (s *Shaper) Set(c Config) (Config, error) {
	c, hosts, err := Normalize(c)
	if err != nil {
		return c, err
	}
	sh := &shaping{cfg: c, hosts: hosts, down: newBucket(c.DownKbps), up: newBucket(c.UpKbps)}
	s.mu.Lock()
	s.cur.Store(sh)
	s.mu.Unlock()
	return c, nil
}

// Config returns the setting in force.
func (s *Shaper) Config() Config {
	c := s.cur.Load().cfg
	c.Hosts = append([]string{}, c.Hosts...)
	return c
}

// Status is the setting and the presets.
func (s *Shaper) Status() Status {
	return Status{Config: s.Config(), Presets: Presets}
}

// Applies reports whether traffic to host is throttled now.
func (s *Shaper) Applies(host string) bool {
	if s == nil {
		return false
	}
	sh := s.cur.Load()
	if !sh.cfg.Enabled {
		return false
	}
	if len(sh.hosts) == 0 {
		return true
	}
	for _, m := range sh.hosts {
		if m.MatchHost(host) {
			return true
		}
	}
	return false
}

// Latency waits the configured latency once, or until ctx ends.
func (s *Shaper) Latency(ctx context.Context) {
	d := time.Duration(s.cur.Load().cfg.LatencyMS) * time.Millisecond
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// Down slows what is read from r towards the client.
func (s *Shaper) Down(r io.Reader) io.Reader { return &reader{r: r, b: s.cur.Load().down} }

// Up slows what is read from r towards the server.
func (s *Shaper) Up(r io.Reader) io.Reader { return &reader{r: r, b: s.cur.Load().up} }

// DownWriter slows what is written to w towards the client.
func (s *Shaper) DownWriter(w io.Writer) io.Writer { return &writer{w: w, b: s.cur.Load().down} }

// Conn slows an upstream connection: reads from it are the down direction,
// writes to it the up direction.
func (s *Shaper) Conn(c net.Conn) net.Conn {
	sh := s.cur.Load()
	return &conn{Conn: c, r: &reader{r: c, b: sh.down}, w: &writer{w: c, b: sh.up}}
}

// bucket is a token bucket of bytes: rate bytes a second, at most burst kept.
type bucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newBucket(kbps int) *bucket {
	if kbps <= 0 {
		return nil
	}
	rate := float64(kbps) * 1000 / 8
	burst := rate / 20 // 50 ms of traffic
	if burst < 1500 {
		burst = 1500
	}
	return &bucket{rate: rate, burst: burst, tokens: burst, last: time.Now()}
}

// chunk is the most one wait covers.
func (b *bucket) chunk() int { return int(b.burst) }

// take reserves n bytes and waits until they are covered.
func (b *bucket) take(n int) {
	b.mu.Lock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	b.tokens -= float64(n)
	var wait time.Duration
	if b.tokens < 0 {
		wait = time.Duration(-b.tokens / b.rate * float64(time.Second))
	}
	b.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

type reader struct {
	r io.Reader
	b *bucket
}

func (r *reader) Read(p []byte) (int, error) {
	if r.b == nil {
		return r.r.Read(p)
	}
	if c := r.b.chunk(); len(p) > c {
		p = p[:c]
	}
	n, err := r.r.Read(p)
	if n > 0 {
		r.b.take(n)
	}
	return n, err
}

// Close closes the underlying reader when it has a Close method.
func (r *reader) Close() error {
	if c, ok := r.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

type writer struct {
	w io.Writer
	b *bucket
}

func (w *writer) Write(p []byte) (int, error) {
	if w.b == nil {
		return w.w.Write(p)
	}
	total := 0
	for len(p) > 0 {
		n := len(p)
		if c := w.b.chunk(); n > c {
			n = c
		}
		w.b.take(n)
		m, err := w.w.Write(p[:n])
		total += m
		if err != nil {
			return total, err
		}
		p = p[n:]
	}
	return total, nil
}

type conn struct {
	net.Conn
	r *reader
	w *writer
}

func (c *conn) Read(p []byte) (int, error)  { return c.r.Read(p) }
func (c *conn) Write(p []byte) (int, error) { return c.w.Write(p) }

// CloseWrite half-closes the underlying connection when it can.
func (c *conn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return errors.New("no half-close")
}
