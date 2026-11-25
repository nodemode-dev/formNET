// Package terminal is the engine side of the Form family's shared terminal
// dock (module 2.2.1): a PTY per tab (ConPTY on Windows), started only when
// the page asks, attached over a WebSocket, and ended when its tab closes,
// when no page has been attached to it for the reload window, and when the
// server stops. It also answers the dock's agent program status through
// agentlookup. Shaped on form.md's internal/claudecode terminal (Apache-2.0).
package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Contract is the dock module version this engine implements.
const Contract = "2.2.1"

const (
	scrollback = 256 * 1024
	maxLive    = 12
	maxKept    = 32
	maxSockets = 24
	window     = 60 * time.Second
	hangup     = time.Second
)

type proc interface {
	io.ReadWriter
	resize(cols, rows uint16) error
	done() <-chan struct{}
	code() int
	end(grace time.Duration)
	release()
}

// Options are what the manager needs from the run.
type Options struct {
	APIAddr   string // FormNET's own address, given to children as FORMNET_URL
	ProxyAddr string // this run's proxy, for tabs opened with capture
	CAPath    string // this run's root certificate (ca.pem), for tabs opened with capture
	Log       *slog.Logger
}

// Manager holds the tabs of this run.
type Manager struct {
	o        Options
	mu       sync.Mutex
	sessions map[string]*Session
	stopped  bool
	sockets  atomic.Int32
	agents   agentCache
}

// New makes an empty manager.
func New(o Options) *Manager {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	return &Manager{o: o, sessions: map[string]*Session{}}
}

// Record is a tab as the dock reads it (clause 3).
type Record struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Alive   bool   `json:"alive"`
	Tip     string `json:"tip"`
	Status  string `json:"status"`
	Cwd     string `json:"cwd"`
	Capture bool   `json:"capture"`
	Code    *int   `json:"code"`
	Created int64  `json:"created_at"`
}

// Session is one tab.
type Session struct {
	m       *Manager
	id      string
	title   string
	kind    string
	cwd     string
	tip     string
	status  string
	capture bool
	created int64
	argv    []string
	env     []string

	mu       sync.Mutex
	p        proc
	alive    bool
	exit     *int
	gen      int
	buf      []byte
	subs     map[*sub]struct{}
	cols     uint16
	rows     uint16
	attached int
	idle     *time.Timer
	closed   bool
}

type sub struct{ ch chan []byte }

var (
	errStopped = errors.New("FormNET is stopping")
	errBusy    = errors.New("too many terminals are open: close one first")
)

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func clamp(v, lo, hi, def int) uint16 {
	if v <= 0 {
		v = def
	}
	return uint16(max(lo, min(hi, v)))
}

func (m *Manager) open(s *Session) error {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return errStopped
	}
	live := 0
	shells := 0
	for _, x := range m.sessions {
		if x.snapshot().Alive {
			live++
		}
		if x.kind == "shell" {
			shells++
		}
	}
	if live >= maxLive || len(m.sessions) >= maxKept {
		m.mu.Unlock()
		return errBusy
	}
	if s.title == "" {
		s.title = "Terminal " + strconv.Itoa(shells+1)
	}
	s.m, s.id, s.created, s.subs = m, newID(), time.Now().UnixMilli(), map[*sub]struct{}{}
	m.sessions[s.id] = s
	m.mu.Unlock()
	if err := s.spawn(); err != nil {
		m.mu.Lock()
		delete(m.sessions, s.id)
		m.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.idle = time.AfterFunc(window, func() { m.expire(s) })
	s.mu.Unlock()
	return nil
}

func (m *Manager) get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) list() []Record {
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()
	out := make([]Record, 0, len(all))
	for _, s := range all {
		out = append(out, s.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created < out[j].Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (m *Manager) live() int {
	n := 0
	for _, r := range m.list() {
		if r.Alive {
			n++
		}
	}
	return n
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s != nil {
		s.stop()
	}
}

func (m *Manager) expire(s *Session) {
	s.mu.Lock()
	idle := s.attached == 0 && !s.closed
	s.mu.Unlock()
	if idle {
		m.o.Log.Info("terminal ended: no page attached within the reload window", "id", s.id, "kind", s.kind)
		m.remove(s.id)
	}
}

// Close ends every tab's program and refuses new ones; FormNET calls it when it stops.
func (m *Manager) Close() {
	m.mu.Lock()
	m.stopped = true
	all := m.sessions
	m.sessions = map[string]*Session{}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.stop()
		}()
	}
	wg.Wait()
}

func (s *Session) snapshot() Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Record{ID: s.id, Title: s.title, Kind: s.kind, Alive: s.alive, Tip: s.tip, Status: s.status, Cwd: s.cwd, Capture: s.capture, Code: s.exit, Created: s.created}
}

func (s *Session) spawn() error {
	s.mu.Lock()
	cols, rows := s.cols, s.rows
	s.mu.Unlock()
	p, err := start(s.argv, s.cwd, s.env, cols, rows)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		p.end(hangup)
		p.release()
		return errStopped
	}
	s.gen++
	gen := s.gen
	s.p, s.alive, s.exit = p, true, nil
	s.mu.Unlock()
	read := make(chan struct{})
	go s.pump(p, gen, read)
	go s.watch(p, gen, read)
	return nil
}

func (s *Session) pump(p proc, gen int, read chan struct{}) {
	defer close(read)
	chunk := make([]byte, 32*1024)
	for {
		n, err := p.Read(chunk)
		if n > 0 {
			data := append([]byte(nil), chunk[:n]...)
			s.mu.Lock()
			if s.gen == gen {
				s.keep(data)
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) keep(data []byte) {
	s.buf = append(s.buf, data...)
	if over := len(s.buf) - scrollback; over > 0 {
		s.buf = append([]byte(nil), s.buf[over:]...)
	}
	for x := range s.subs {
		select {
		case x.ch <- data:
		default:
		}
	}
}

func (s *Session) watch(p proc, gen int, read chan struct{}) {
	<-p.done()
	select {
	case <-read:
	case <-time.After(drain):
	}
	p.end(0)
	p.release()
	select {
	case <-read:
	case <-time.After(2 * time.Second):
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return
	}
	code := p.code()
	s.alive, s.exit = false, &code
	for x := range s.subs {
		close(x.ch)
		delete(s.subs, x)
	}
}

func (s *Session) halt() {
	s.mu.Lock()
	s.gen++
	p := s.p
	s.p, s.alive = nil, false
	s.mu.Unlock()
	if p != nil {
		p.end(hangup)
		p.release()
	}
}

func (s *Session) stop() {
	s.mu.Lock()
	s.closed = true
	if s.idle != nil {
		s.idle.Stop()
	}
	for x := range s.subs {
		close(x.ch)
		delete(s.subs, x)
	}
	s.mu.Unlock()
	s.halt()
}

func (s *Session) restart() error {
	s.halt()
	s.mu.Lock()
	s.buf = []byte("\x1bc")
	for x := range s.subs {
		select {
		case x.ch <- []byte("\x1bc"):
		default:
		}
	}
	s.mu.Unlock()
	return s.spawn()
}

func (s *Session) attach() (*sub, []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := &sub{ch: make(chan []byte, 512)}
	backlog := append([]byte{}, s.buf...)
	if s.alive && !s.closed {
		s.subs[x] = struct{}{}
	} else {
		close(x.ch)
	}
	s.attached++
	if s.idle != nil {
		s.idle.Stop()
	}
	return x, backlog, s.alive
}

func (s *Session) detach(x *sub) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.subs[x]; ok {
		delete(s.subs, x)
		close(x.ch)
	}
	s.attached--
	if s.attached == 0 && !s.closed {
		s.idle = time.AfterFunc(window, func() { s.m.expire(s) })
	}
}

func (s *Session) ended() (bool, *int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed, s.exit
}

func (s *Session) write(b []byte) {
	s.mu.Lock()
	p, alive := s.p, s.alive
	s.mu.Unlock()
	if alive && p != nil {
		_, _ = p.Write(b)
	}
}

func (s *Session) resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = clamp(cols, 10, 500, 100), clamp(rows, 4, 200, 30)
	if s.alive && s.p != nil {
		_ = s.p.resize(s.cols, s.rows)
	}
}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return string(os.PathSeparator)
}
