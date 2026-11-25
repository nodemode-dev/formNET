package flow

import "time"

// Attribute names the program behind a client address on every kept flow of
// that address that started at or after since and has no program yet, as a
// new value per flow, and returns how many it named. The machine view
// answers some lookups only after the flow was stored; since keeps an older
// connection that used the same port from being named.
func (s *Store) Attribute(client string, since time.Time, pid int, name string) int {
	if client == "" || pid <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for i := len(s.order) - 1; i >= 0; i-- {
		old := s.byID[s.order[i]]
		if old.FromSession {
			continue
		}
		if old.Start.Before(since) {
			break
		}
		if old.Client != client || old.ClientPID != 0 {
			continue
		}
		f := *old
		f.ClientPID, f.ClientProcess = pid, name
		s.byID[f.ID] = &f
		s.publishLocked(Event{Type: EventUpdated, Flow: &f})
		n++
	}
	return n
}
