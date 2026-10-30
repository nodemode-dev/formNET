package flow

import (
	"context"
	"sort"
	"strings"
	"sync"
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

// Store keeps the newest flows in arrival order, dropping the oldest past its limit.
type Store struct {
	limit int

	mu     sync.RWMutex
	seq    uint64
	order  []string // ids in arrival order
	byID   map[string]*Flow
	subs   map[chan Event]struct{}
	hostFn func(string) string
}

// NewStore keeps at most limit flows.
func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = 1
	}
	return &Store{limit: limit, byID: map[string]*Flow{}, subs: map[chan Event]struct{}{}}
}

// Add stores a new flow (assigning its Seq) or, when the id is known, replaces it.
func (s *Store) Add(f Flow) Flow {
	s.mu.Lock()
	if existing, ok := s.byID[f.ID]; ok {
		f.Seq = existing.Seq
		s.byID[f.ID] = &f
		s.mu.Unlock()
		s.publish(Event{Type: EventUpdated, Flow: &f})
		return f
	}
	s.seq++
	f.Seq = s.seq
	s.byID[f.ID] = &f
	s.order = append(s.order, f.ID)
	s.evictLocked()
	s.mu.Unlock()
	s.publish(Event{Type: EventAdded, Flow: &f})
	return f
}

// Update replaces a stored flow. A flow that was already evicted is dropped, not re-added.
func (s *Store) Update(f Flow) (Flow, bool) {
	s.mu.Lock()
	existing, ok := s.byID[f.ID]
	if !ok {
		s.mu.Unlock()
		return f, false
	}
	f.Seq = existing.Seq
	s.byID[f.ID] = &f
	s.mu.Unlock()
	s.publish(Event{Type: EventUpdated, Flow: &f})
	return f, true
}

func (s *Store) evictLocked() {
	for len(s.order) > s.limit {
		id := s.order[0]
		s.order = s.order[1:]
		delete(s.byID, id)
	}
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
	s.order = nil
	s.byID = map[string]*Flow{}
	s.mu.Unlock()
	s.publish(Event{Type: EventCleared})
}

// Subscribe delivers events until ctx ends. A slow subscriber loses events
// rather than slowing the proxy; it should re-list with SinceSeq afterwards.
func (s *Store) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, 256)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()
	return ch
}

func (s *Store) publish(e Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
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
		if f.Kind != KindTunnel {
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
