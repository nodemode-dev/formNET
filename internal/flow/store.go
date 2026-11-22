package flow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// EventType says what happened to a flow.
type EventType string

const (
	EventAdded   EventType = "flow.added"
	EventUpdated EventType = "flow.updated"
	EventCleared EventType = "flow.cleared"
)

// Event is one change a subscriber receives.
type Event struct {
	Type EventType `json:"type"`
	Flow *Flow     `json:"flow,omitempty"`
}

// Query narrows List.
type Query struct {
	Host     string // exact host, or a *.suffix pattern
	Domain   string // Domain(host)
	Kind     string
	Client   string // client ip:port prefix
	Text     string // substring of the URL, method, status or content type, case-insensitive
	SinceSeq uint64 // only flows with a greater Seq
	Pending  *bool
	Limit    int // 0 means every match
}

// DefaultByteBudget bounds the captured bodies kept in memory.
const DefaultByteBudget int64 = 256 << 20

// Store keeps the newest flows in arrival order, dropping the oldest past its
// flow limit or its byte budget (the captured bodies of every kept flow).
type Store struct {
	limit  int
	budget int64
	epoch  string

	mu    sync.RWMutex
	seq   uint64
	order []string // ids in arrival order
	byID  map[string]*Flow
	bytes int64 // captured body bytes across kept flows
	subs  map[*subscriber]struct{}
}

type subscriber struct {
	ch      chan Event
	dropped atomic.Uint64
}

// NewStore keeps at most limit flows and DefaultByteBudget of captured bodies.
func NewStore(limit int) *Store {
	return NewStoreWithBudget(limit, DefaultByteBudget)
}

// NewStoreWithBudget keeps at most limit flows and budget bytes of captured bodies.
func NewStoreWithBudget(limit int, budget int64) *Store {
	if limit <= 0 {
		limit = 1
	}
	if budget <= 0 {
		budget = DefaultByteBudget
	}
	return &Store{limit: limit, budget: budget, epoch: newEpoch(), byID: map[string]*Flow{}, subs: map[*subscriber]struct{}{}}
}

func newEpoch() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Epoch identifies this store's lifetime; a client that sees a new epoch
// reloads instead of asking for flows since a sequence number it remembers.
func (s *Store) Epoch() string { return s.epoch }

func captured(f *Flow) int64 {
	n := int64(len(f.Request.Body))
	if f.Response != nil {
		n += int64(len(f.Response.Body))
	}
	return n
}

// Add stores a new flow (assigning its Seq) or, when the id is known, replaces it.
// Events are published under the lock so subscribers see them in store order.
func (s *Store) Add(f Flow) Flow {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byID[f.ID]; ok {
		s.replaceLocked(existing, &f)
		s.publishLocked(Event{Type: EventUpdated, Flow: &f})
		return f
	}
	s.seq++
	f.Seq = s.seq
	s.byID[f.ID] = &f
	s.order = append(s.order, f.ID)
	s.bytes += captured(&f)
	s.evictLocked()
	s.publishLocked(Event{Type: EventAdded, Flow: &f})
	return f
}

// Update replaces a stored flow. A flow that was already evicted is dropped, not re-added.
func (s *Store) Update(f Flow) (Flow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.byID[f.ID]
	if !ok {
		return f, false
	}
	s.replaceLocked(existing, &f)
	s.evictLocked()
	s.publishLocked(Event{Type: EventUpdated, Flow: &f})
	return f, true
}

func (s *Store) replaceLocked(old, now *Flow) {
	now.Seq = old.Seq
	s.bytes += captured(now) - captured(old)
	s.byID[now.ID] = now
}

// evictLocked drops the oldest flows while the store is over its flow limit
// or its byte budget. The newest flow is always kept.
func (s *Store) evictLocked() {
	for len(s.order) > 1 && (len(s.order) > s.limit || s.bytes > s.budget) {
		id := s.order[0]
		s.order = s.order[1:]
		if f, ok := s.byID[id]; ok {
			s.bytes -= captured(f)
			delete(s.byID, id)
		}
	}
}

// Stats counts what the store holds in one pass.
type Stats struct {
	Flows   int   `json:"flows"`
	Pending int   `json:"pending"`
	Hosts   int   `json:"hosts"`
	Bytes   int64 `json:"captured_bytes"`
	Budget  int64 `json:"byte_budget"`
	Limit   int   `json:"flow_limit"`
}

// Stats returns counts without copying flows.
func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Stats{Flows: len(s.order), Bytes: s.bytes, Budget: s.budget, Limit: s.limit}
	hosts := map[string]struct{}{}
	for _, id := range s.order {
		f := s.byID[id]
		if f.Pending {
			st.Pending++
		}
		hosts[f.Host] = struct{}{}
	}
	st.Hosts = len(hosts)
	return st
}

// Get returns one flow with its bodies.
func (s *Store) Get(id string) (Flow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.byID[id]
	if !ok {
		return Flow{}, false
	}
	return *f, true
}

// Len is the number of flows kept.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}

// List returns matching flows in arrival order, without bodies.
func (s *Store) List(q Query) []Flow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Flow, 0, len(s.order))
	for _, id := range s.order {
		f := s.byID[id]
		if f.Seq <= q.SinceSeq || !matches(f, q) {
			continue
		}
		out = append(out, stripBodies(*f))
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out
}

func stripBodies(f Flow) Flow {
	f.Request.Body = nil
	if f.Response != nil {
		r := *f.Response
		r.Body = nil
		f.Response = &r
	}
	return f
}

func matches(f *Flow, q Query) bool {
	if q.Host != "" && !HostMatches(f.Host, q.Host) {
		return false
	}
	if q.Domain != "" && Domain(f.Host) != strings.ToLower(q.Domain) {
		return false
	}
	if q.Kind != "" && f.Kind != q.Kind {
		return false
	}
	if q.Client != "" && !strings.HasPrefix(f.Client, q.Client) {
		return false
	}
	if q.Pending != nil && f.Pending != *q.Pending {
		return false
	}
	if q.Text != "" {
		t := strings.ToLower(q.Text)
		hay := strings.ToLower(f.URL + " " + f.Method + " " + itoa(f.Status) + " " + f.ClientProcess)
		if f.Response != nil {
			hay += " " + strings.ToLower(f.Response.ContentType)
		}
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// HostMatches reports whether host equals pattern, or pattern is *.suffix and
// host is the suffix or ends with .suffix. Matching ignores case.
func HostMatches(host, pattern string) bool {
	host = strings.ToLower(host)
	pattern = strings.ToLower(pattern)
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		return host == suffix || strings.HasSuffix(host, "."+suffix)
	}
	return host == pattern
}

// Clear forgets every flow.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = nil
	s.byID = map[string]*Flow{}
	s.bytes = 0
	s.publishLocked(Event{Type: EventCleared})
}

// Subscribe delivers events until ctx ends. A slow subscriber loses events
// rather than slowing the proxy; it should re-list afterwards.
func (s *Store) Subscribe(ctx context.Context) <-chan Event {
	ch, _ := s.SubscribeCounted(ctx)
	return ch
}

// SubscribeCounted is Subscribe with a function that reports how many events
// this subscriber has lost so far, so a stream can tell its client to resync.
func (s *Store) SubscribeCounted(ctx context.Context) (<-chan Event, func() uint64) {
	sub := &subscriber{ch: make(chan Event, 256)}
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
	}()
	return sub.ch, sub.dropped.Load
}

func (s *Store) publishLocked(e Event) {
	for sub := range s.subs {
		select {
		case sub.ch <- e:
		default:
			sub.dropped.Add(1)
		}
	}
}

// HostSummary aggregates the flows to one host.
type HostSummary struct {
	Host          string    `json:"host"`
	Domain        string    `json:"domain"`
	Flows         int       `json:"flows"`
	Pending       int       `json:"pending"`
	Errors        int       `json:"errors"` // transport errors and 4xx/5xx
	BytesSent     int64     `json:"bytes_sent"`
	BytesReceived int64     `json:"bytes_received"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
	Intercepted   bool      `json:"intercepted"` // at least one flow was opened, not just counted
	Processes     []string  `json:"processes"`
	Category      string    `json:"category,omitempty"`
	Owner         string    `json:"owner,omitempty"`
}

// Hosts summarises every host seen, most traffic first.
func (s *Store) Hosts() []HostSummary {
	s.mu.RLock()
	acc := map[string]*HostSummary{}
	procs := map[string]map[string]struct{}{}
	for _, id := range s.order {
		f := s.byID[id]
		h, ok := acc[f.Host]
		if !ok {
			cat, owner := Classify(f.Host)
			h = &HostSummary{Host: f.Host, Domain: Domain(f.Host), FirstSeen: f.Start, LastSeen: f.Start, Category: cat, Owner: owner}
			acc[f.Host] = h
			procs[f.Host] = map[string]struct{}{}
		}
		h.Flows++
		if f.Pending {
			h.Pending++
		}
		if f.Error != "" || f.Status >= 400 {
			h.Errors++
		}
		h.BytesSent += f.BytesSent
		h.BytesReceived += f.BytesReceived
		if f.Start.Before(h.FirstSeen) {
			h.FirstSeen = f.Start
		}
		last := f.Start
		if f.End != nil {
			last = *f.End
		}
		if last.After(h.LastSeen) {
			h.LastSeen = last
		}
		if f.Kind != KindTunnel && f.Kind != KindRefused {
			h.Intercepted = true
		}
		if f.ClientProcess != "" {
			procs[f.Host][f.ClientProcess] = struct{}{}
		}
	}
	s.mu.RUnlock()
	out := make([]HostSummary, 0, len(acc))
	for host, h := range acc {
		names := make([]string, 0, len(procs[host]))
		for n := range procs[host] {
			names = append(names, n)
		}
		sort.Strings(names)
		h.Processes = names
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].BytesSent+out[i].BytesReceived, out[j].BytesSent+out[j].BytesReceived
		if ti != tj {
			return ti > tj
		}
		if out[i].Flows != out[j].Flows {
			return out[i].Flows > out[j].Flows
		}
		return out[i].Host < out[j].Host
	})
	return out
}
