package flow

import (
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

// A session file (.formnet) is a recording saved whole: a gzip-compressed
// JSON document
//
//	{"format": "formnet-session", "version": 1, "app_version", "saved", "flow_count",
//	 "flows": [flow with "request_body" and "response_body"], "messages": {flow_id: [message]}}
//
// Bodies are {text} when they are UTF-8 without control bytes, {base64}
// otherwise, with decoded set when the body is the decoded form of its
// Content-Encoding. Messages have the shape of GET /api/flows/{id}/messages.
// Readers dispatch on version, so a later FormNET keeps reading version 1.
const (
	SessionFormat  = "formnet-session"
	SessionVersion = 1
	SessionExt     = ".formnet"
)

// SessionHead is what a session file says about itself before its flows.
type SessionHead struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	AppVersion string    `json:"app_version"`
	Saved      time.Time `json:"saved"`
	FlowCount  int       `json:"flow_count"`
}

// SessionBody is a captured body as a session file holds it.
type SessionBody struct {
	Text    *string `json:"text,omitempty"`
	Base64  *string `json:"base64,omitempty"`
	Decoded bool    `json:"decoded,omitempty"`
}

// SessionFlow is a flow with its bodies, as a session file holds it.
type SessionFlow struct {
	Flow
	RequestBody  *SessionBody `json:"request_body,omitempty"`
	ResponseBody *SessionBody `json:"response_body,omitempty"`
}

// SessionMessage is one WebSocket message in a session file, the shape the
// messages route lists.
type SessionMessage struct {
	Seq        uint64    `json:"seq"`
	Dir        string    `json:"dir"`
	Time       time.Time `json:"time"`
	Opcode     string    `json:"opcode"`
	Text       *string   `json:"text,omitempty"`
	Base64     *string   `json:"base64,omitempty"`
	Bytes      int64     `json:"bytes"`
	Truncated  bool      `json:"truncated"`
	Compressed bool      `json:"compressed"`
	NotDecoded bool      `json:"not_decoded,omitempty"`
	Inflated   int64     `json:"inflated_bytes,omitempty"`
	Frames     int       `json:"frames,omitempty"`
	CloseCode  int       `json:"close_code,omitempty"`
}

// Payload returns the bytes a message carried, as far as they were kept.
func (m SessionMessage) Payload() []byte {
	if m.Text != nil {
		return []byte(*m.Text)
	}
	if m.Base64 != nil {
		if b, err := base64.StdEncoding.DecodeString(*m.Base64); err == nil {
			return b
		}
	}
	return nil
}

func sessionBody(m *Message) *SessionBody {
	if m == nil || len(m.Body) == 0 {
		return nil
	}
	b := &SessionBody{Decoded: m.Decoded}
	if utf8.Valid(m.Body) && !controlBytes(m.Body) {
		s := string(m.Body)
		b.Text = &s
	} else {
		s := base64.StdEncoding.EncodeToString(m.Body)
		b.Base64 = &s
	}
	return b
}

func controlBytes(b []byte) bool {
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return true
		}
	}
	return false
}

// WriteSession writes listed (flows as List returns them) as an uncompressed
// session document, reading each flow's bodies from s one at a time; a flow
// evicted meanwhile is written as listed. messages gives a WebSocket flow's
// kept messages. It returns the number of flows written.
func WriteSession(w io.Writer, s *Store, listed []Flow, appVersion string, saved time.Time, messages func(id string) []SessionMessage) (int, error) {
	head := SessionHead{Format: SessionFormat, Version: SessionVersion, AppVersion: appVersion, Saved: saved.UTC(), FlowCount: len(listed)}
	raw, err := json.Marshal(head)
	if err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(w, "%s,\"flows\":[\n", raw[:len(raw)-1]); err != nil {
		return 0, err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	var sockets []string
	for i, f := range listed {
		if g, ok := s.Get(f.ID); ok {
			f = g
		}
		if i > 0 {
			if _, err := io.WriteString(w, ","); err != nil {
				return i, err
			}
		}
		if err := enc.Encode(SessionFlow{Flow: f, RequestBody: sessionBody(&f.Request), ResponseBody: sessionBody(f.Response)}); err != nil {
			return i, err
		}
		if f.Kind == KindWebSocket {
			sockets = append(sockets, f.ID)
		}
	}
	if _, err := io.WriteString(w, "],\"messages\":{"); err != nil {
		return len(listed), err
	}
	first := true
	for _, id := range sockets {
		var msgs []SessionMessage
		if messages != nil {
			msgs = messages(id)
		}
		if len(msgs) == 0 {
			continue
		}
		key, _ := json.Marshal(id)
		sep := "\n"
		if !first {
			sep = ",\n"
		}
		first = false
		if _, err := fmt.Fprintf(w, "%s%s:", sep, key); err != nil {
			return len(listed), err
		}
		if err := enc.Encode(msgs); err != nil {
			return len(listed), err
		}
	}
	_, err = io.WriteString(w, "}}\n")
	return len(listed), err
}

// Session is a session file read back, its flows ready for the Store.
type Session struct {
	SessionHead
	Flows    []Flow
	Messages map[string][]SessionMessage // by the new flow ids
	Dropped  int                         // the oldest flows left out to stay inside the Store's limits
}

// SessionLimits bound reading a session file.
type SessionLimits struct {
	MaxBytes  int64 // the most JSON bytes read, after decompression
	Flows     int   // the most flows kept (the Store's flow limit)
	BodyBytes int64 // the most body bytes kept (the Store's byte budget)
}

// ErrSessionTooLarge is returned when a file decompresses past SessionLimits.MaxBytes.
var ErrSessionTooLarge = errors.New("the session file is larger than FormNET reads")

type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, ErrSessionTooLarge
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// ReadSession reads a session file, gzip-compressed or plain JSON. Every
// flow gets a new id (WebSocket messages and resent_from follow it) and is
// marked from_session, so it can never collide with a live flow.
func ReadSession(r io.Reader, lim SessionLimits) (*Session, error) {
	br := bufio.NewReader(r)
	var src io.Reader = br
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("the file is not a readable gzip stream: %w", err)
		}
		defer zr.Close()
		src = zr
	}
	if lim.MaxBytes > 0 {
		src = &cappedReader{r: src, left: lim.MaxBytes}
	}
	dec := json.NewDecoder(src)
	sess := &Session{Messages: map[string][]SessionMessage{}}
	if err := expectDelim(dec, '{'); err != nil {
		return nil, notSession(err)
	}
	var flows []Flow
	var kept int64
	ids := map[string]string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, notSession(err)
		}
		key, _ := tok.(string)
		switch key {
		case "format":
			err = dec.Decode(&sess.Format)
			if err == nil && sess.Format != SessionFormat {
				return nil, errors.New("this is not a FormNET session file: its format is not formnet-session")
			}
		case "version":
			err = dec.Decode(&sess.Version)
			if err == nil && sess.Version > SessionVersion {
				return nil, fmt.Errorf("this session was saved by a newer FormNET (file version %d); this FormNET reads version %d", sess.Version, SessionVersion)
			}
		case "app_version":
			err = dec.Decode(&sess.AppVersion)
		case "saved":
			err = dec.Decode(&sess.Saved)
		case "flow_count":
			err = dec.Decode(&sess.FlowCount)
		case "flows":
			if err = expectDelim(dec, '['); err != nil {
				break
			}
			for dec.More() {
				var sf SessionFlow
				if err = dec.Decode(&sf); err != nil {
					break
				}
				f, ferr := sf.toFlow()
				if ferr != nil {
					return nil, fmt.Errorf("flow %d: %w", len(flows)+sess.Dropped+1, ferr)
				}
				old := f.ID
				f.ID = newEpoch()
				if old != "" {
					ids[old] = f.ID
				}
				flows = append(flows, f)
				kept += captured(&f)
				for len(flows) > 1 && ((lim.Flows > 0 && len(flows) > lim.Flows) || (lim.BodyBytes > 0 && kept > lim.BodyBytes)) {
					kept -= captured(&flows[0])
					flows = flows[1:]
					sess.Dropped++
				}
			}
			if err == nil {
				err = expectDelim(dec, ']')
			}
		case "messages":
			if err = expectDelim(dec, '{'); err != nil {
				break
			}
			for dec.More() {
				var t json.Token
				if t, err = dec.Token(); err != nil {
					break
				}
				id, _ := t.(string)
				var msgs []SessionMessage
				if err = dec.Decode(&msgs); err != nil {
					break
				}
				sess.Messages[id] = msgs
			}
			if err == nil {
				err = expectDelim(dec, '}')
			}
		default:
			var skip json.RawMessage
			err = dec.Decode(&skip)
		}
		if err != nil {
			if errors.Is(err, ErrSessionTooLarge) {
				return nil, err
			}
			return nil, fmt.Errorf("the session file is damaged near %q: %v", key, err)
		}
	}
	if sess.Format != SessionFormat {
		return nil, errors.New("this is not a FormNET session file: it has no format formnet-session")
	}
	if sess.Version < 1 {
		return nil, errors.New("this session file has no version")
	}
	live := map[string]bool{}
	for i := range flows {
		live[flows[i].ID] = true
		if n, ok := ids[flows[i].ResentFrom]; ok {
			flows[i].ResentFrom = n
		}
	}
	byNew := map[string][]SessionMessage{}
	for old, msgs := range sess.Messages {
		if n, ok := ids[old]; ok && live[n] {
			byNew[n] = msgs
		}
	}
	sess.Messages = byNew
	sess.Flows = flows
	return sess, nil
}

func notSession(err error) error {
	if errors.Is(err, ErrSessionTooLarge) {
		return err
	}
	return fmt.Errorf("this is not a FormNET session file: %v", err)
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q", string(want))
	}
	return nil
}

func (b *SessionBody) bytes() ([]byte, error) {
	switch {
	case b == nil:
		return nil, nil
	case b.Text != nil:
		return []byte(*b.Text), nil
	case b.Base64 != nil:
		raw, err := base64.StdEncoding.DecodeString(*b.Base64)
		if err != nil {
			return nil, errors.New("a body is not valid base64")
		}
		return raw, nil
	}
	return nil, nil
}

func (sf SessionFlow) toFlow() (Flow, error) {
	f := sf.Flow
	body, err := sf.RequestBody.bytes()
	if err != nil {
		return f, err
	}
	f.Request.Body = body
	f.Request.Decoded = sf.RequestBody != nil && sf.RequestBody.Decoded
	if f.Response != nil {
		r := *f.Response
		if r.Body, err = sf.ResponseBody.bytes(); err != nil {
			return f, err
		}
		r.Decoded = sf.ResponseBody != nil && sf.ResponseBody.Decoded
		f.Response = &r
	}
	f.Seq, f.Pending, f.Paused, f.FromSession = 0, false, "", true
	if f.Kind == "" {
		f.Kind = KindHTTP
	}
	return f, nil
}

// ReadSessionHead reads only what a session file says before its flows, for
// listing saved sessions without reading them whole.
func ReadSessionHead(r io.Reader) (SessionHead, error) {
	var head SessionHead
	zr, err := gzip.NewReader(r)
	if err != nil {
		return head, err
	}
	defer zr.Close()
	dec := json.NewDecoder(&cappedReader{r: zr, left: 1 << 20})
	if err := expectDelim(dec, '{'); err != nil {
		return head, err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return head, err
		}
		switch tok {
		case "format":
			err = dec.Decode(&head.Format)
		case "version":
			err = dec.Decode(&head.Version)
		case "app_version":
			err = dec.Decode(&head.AppVersion)
		case "saved":
			err = dec.Decode(&head.Saved)
		case "flow_count":
			err = dec.Decode(&head.FlowCount)
		default:
			return head, nil
		}
		if err != nil {
			return head, err
		}
	}
	return head, nil
}

// Replace forgets every flow and keeps flows instead, in their order, as
// one cleared event and an added event per flow (a subscriber that falls
// behind gets a resync). The Store's limits apply as for live flows.
func (s *Store) Replace(flows []Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = nil
	s.byID = map[string]*Flow{}
	s.bytes = 0
	s.publishLocked(Event{Type: EventCleared})
	s.appendLocked(flows)
}

// Append adds flows after the ones kept, as imported flows are.
func (s *Store) Append(flows []Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendLocked(flows)
}

func (s *Store) appendLocked(flows []Flow) {
	for i := range flows {
		f := flows[i]
		if _, dup := s.byID[f.ID]; dup || f.ID == "" {
			f.ID = newEpoch()
		}
		s.seq++
		f.Seq = s.seq
		s.byID[f.ID] = &f
		s.order = append(s.order, f.ID)
		s.bytes += captured(&f)
		s.evictLocked()
		s.publishLocked(Event{Type: EventAdded, Flow: &f})
	}
}

// Limits returns the Store's flow limit and byte budget.
func (s *Store) Limits() (int, int64) {
	return s.limit, s.budget
}
