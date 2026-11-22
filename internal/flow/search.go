package flow

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultBodyScan bounds the body bytes one search reads.
const DefaultBodyScan int64 = 64 << 20

// Filter narrows a search by a flow's properties and its captured bodies.
// Every set field must match (AND); the zero value matches every flow.
type Filter struct {
	StatusMin, StatusMax int      // status range, inclusive; 0 and 0 for any
	Methods              []string // upper case; any of them
	TypePrefix           string   // response content type prefix, lower case
	MinSize, MaxSize     int64    // response body bytes as sent; -1 for no bound
	MinMS, MaxMS         float64  // duration; -1 for no bound
	Error                *bool
	Rules                *bool
	Throttled            *bool
	FromSession          *bool
	Process              string            // lower case substring of the client process
	Focus                func(string) bool // the host is one of the focused hosts
	Body                 string            // case-insensitive substring of a decoded text body
	BodyScan             int64             // body bytes one search may read; 0 means DefaultBodyScan
	Ignore               func(string) bool // the host is one of the ignored hosts: left out, after Focus

	// Each Not inverts its one filter: the value given with a leading !.
	StatusNot, MethodsNot, TypeNot bool
	ProcessNot, BodyNot            bool
	NotHost                        string // a host or *.suffix pattern (Query.Host's matching) whose flows are left out
	NotDomain                      string // a Domain(host) whose flows are left out
}

// SearchStats says how much body text a search read.
type SearchStats struct {
	BodyScanned int64 `json:"body_scanned_bytes"`
	BodyCut     bool  `json:"body_scan_cut"` // the bound was reached: older flows were not searched and are left out
}

// NewFilter returns a Filter that bounds nothing.
func NewFilter() *Filter {
	return &Filter{MinSize: -1, MaxSize: -1, MinMS: -1, MaxMS: -1}
}

// SecretHeader reports a header whose value the HAR and cURL exports redact by default.
func SecretHeader(name string) bool { return secretHeader(name) }

// Redacted is what a redacted header value reads.
const Redacted = redacted

// Search is List with a Filter: it walks from the newest flow back, so a
// body search that reaches its bound has searched the newest flows, and
// stops once Limit flows matched. Flows come back in arrival order.
func (s *Store) Search(q Query, fl *Filter) ([]Flow, SearchStats) {
	var st SearchStats
	if fl == nil {
		return s.List(q), st
	}
	budget := fl.BodyScan
	if budget <= 0 {
		budget = DefaultBodyScan
	}
	needle := []byte(strings.ToLower(fl.Body))
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Flow
	for i := len(s.order) - 1; i >= 0; i-- {
		f := s.byID[s.order[i]]
		if f.Seq <= q.SinceSeq || !matches(f, q) || !fl.props(f, now) {
			continue
		}
		if len(needle) > 0 {
			if st.BodyCut {
				continue
			}
			found := false
			for _, m := range []*Message{&f.Request, f.Response} {
				if m == nil || len(m.Body) == 0 || found {
					continue
				}
				if st.BodyScanned+int64(len(m.Body)) > budget {
					st.BodyCut = true
					break
				}
				st.BodyScanned += int64(len(m.Body))
				found = utf8.Valid(m.Body) && bytes.Contains(bytes.ToLower(m.Body), needle)
			}
			if st.BodyCut || found == fl.BodyNot {
				continue
			}
		}
		out = append(out, stripBodies(*f))
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if out == nil {
		out = []Flow{}
	}
	return out, st
}

func (fl *Filter) props(f *Flow, now time.Time) bool {
	if fl.StatusMax > 0 && (f.Status >= fl.StatusMin && f.Status <= fl.StatusMax) == fl.StatusNot {
		return false
	}
	if len(fl.Methods) > 0 {
		hit := false
		for _, m := range fl.Methods {
			hit = hit || strings.EqualFold(f.Method, m)
		}
		if hit == fl.MethodsNot {
			return false
		}
	}
	if fl.TypePrefix != "" && (f.Response != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.Response.ContentType)), fl.TypePrefix)) == fl.TypeNot {
		return false
	}
	if fl.NotHost != "" && HostMatches(f.Host, fl.NotHost) {
		return false
	}
	if fl.NotDomain != "" && Domain(f.Host) == strings.ToLower(fl.NotDomain) {
		return false
	}
	if fl.MinSize >= 0 || fl.MaxSize >= 0 {
		size := f.BytesReceived
		if f.Response != nil {
			size = f.Response.BodyBytes
		}
		if (fl.MinSize >= 0 && size < fl.MinSize) || (fl.MaxSize >= 0 && size > fl.MaxSize) {
			return false
		}
	}
	if fl.MinMS >= 0 || fl.MaxMS >= 0 {
		ms := f.DurationMS
		if f.Pending {
			ms = float64(now.Sub(f.Start)) / float64(time.Millisecond)
		}
		if (fl.MinMS >= 0 && ms < fl.MinMS) || (fl.MaxMS >= 0 && ms > fl.MaxMS) {
			return false
		}
	}
	if fl.Error != nil && (f.Error != "") != *fl.Error {
		return false
	}
	if fl.Rules != nil && (len(f.Rules) > 0) != *fl.Rules {
		return false
	}
	if fl.Throttled != nil && f.Throttled != *fl.Throttled {
		return false
	}
	if fl.FromSession != nil && f.FromSession != *fl.FromSession {
		return false
	}
	if fl.Process != "" && strings.Contains(strings.ToLower(f.ClientProcess), fl.Process) == fl.ProcessNot {
		return false
	}
	if fl.Focus != nil && !fl.Focus(f.Host) {
		return false
	}
	if fl.Ignore != nil && fl.Ignore(f.Host) {
		return false
	}
	return true
}
