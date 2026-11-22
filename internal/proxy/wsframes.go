package proxy

import (
	"bytes"
	"compress/flate"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// WebSocket message logs. The relay copies bytes exactly as before; each
// direction's bytes also pass through a frame reader (RFC 6455) that never
// changes, holds or slows them. A frame it cannot follow stops the reading
// for that direction, not the relay. Rules never change frames.
const (
	wsMaxMessages    = 1000     // kept per flow; later ones are counted only
	wsMaxPayload     = 1 << 20  // payload bytes kept per flow
	wsMessageCap     = 64 << 10 // payload bytes kept per message
	wsInflateInput   = 4 << 20  // a compressed message larger than this is not inflated
	wsInflateOutput  = 8 << 20  // nor one that inflates past this
	wsMaxLogs        = 256      // flows whose messages are kept
	wsRegistryBudget = 32 << 20 // payload bytes kept across all flows
	wsEventEvery     = 100 * time.Millisecond
	wsSummaryEvery   = time.Second
	deflateWindow    = 32 << 10
)

// WSMessage is one WebSocket message as GET /api/flows/{id}/messages lists it.
type WSMessage struct {
	Seq        uint64    `json:"seq"`
	Dir        string    `json:"dir"` // client or server: who sent it
	Time       time.Time `json:"time"`
	Opcode     string    `json:"opcode"` // text, binary, close, ping, pong
	Text       *string   `json:"text,omitempty"`
	Base64     *string   `json:"base64,omitempty"`
	Bytes      int64     `json:"bytes"` // payload bytes as sent
	Truncated  bool      `json:"truncated"`
	Compressed bool      `json:"compressed"`
	NotDecoded bool      `json:"not_decoded,omitempty"`    // compressed and FormNET could not inflate it: base64 holds the compressed bytes
	Inflated   int64     `json:"inflated_bytes,omitempty"` // payload bytes after inflating
	Frames     int       `json:"frames,omitempty"`         // more than 1 when the message was fragmented
	CloseCode  int       `json:"close_code,omitempty"`
}

// MessagePage is the answer of GET /api/flows/{id}/messages.
type MessagePage struct {
	FlowID   string      `json:"flow_id"`
	Messages []WSMessage `json:"messages"`
	Next     uint64      `json:"next"` // pass as since to read on
	flow.MessageSummary
	Open    bool `json:"open"`    // the socket is still relayed
	Evicted bool `json:"evicted"` // the messages were forgotten to keep memory bounded; the counts remain on the flow
}

// MessageEvent is the stream's flow.message event, at most 10 a second per flow.
type MessageEvent struct {
	FlowID    string `json:"flow_id"`
	Seq       uint64 `json:"seq"`
	Dir       string `json:"dir"`
	Opcode    string `json:"opcode"`
	Bytes     int64  `json:"bytes"`
	Coalesced uint64 `json:"coalesced"` // messages since the previous event, this one included
}

type wsRegistry struct {
	mu    sync.Mutex
	logs  map[string]*wsLog
	order []string
	kept  int64
	subs  map[chan MessageEvent]struct{}
}

type wsLog struct {
	flowID string
	reg    *wsRegistry
	ended  atomic.Bool
	kept   atomic.Int64

	mu      sync.Mutex
	msgs    []WSMessage
	sum     flow.MessageSummary
	lastDir string
	lastOp  string
	lastLen int64
}

// relayFrames relays an upgraded connection like pipe and records its messages.
func (p *Proxy) relayFrames(client, upstream net.Conn, f flow.Flow, respHeader http.Header) (int64, int64, *flow.MessageSummary) {
	log := p.newWSLog(f.ID)
	clientTakeover, serverTakeover, deflate := deflateParams(respHeader)
	log.mu.Lock()
	log.sum.Deflate = deflate
	log.mu.Unlock()
	up := &wsParser{log: log, dir: "client"}
	down := &wsParser{log: log, dir: "server"}
	if deflate {
		up.infl = &inflater{takeover: clientTakeover}
		down.infl = &inflater{takeover: serverTakeover}
	}
	first := log.summary()
	f.Messages = &first
	f, _ = p.opts.Store.Update(f)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		p.followMessages(log, f, done)
	}()
	sent, received := pipe(&tapConn{Conn: client, tap: up}, &tapConn{Conn: upstream, tap: down}, p.opts.TunnelIdle)
	close(done)
	wg.Wait()
	log.ended.Store(true)
	final := log.summary()
	return sent, received, &final
}

// followMessages publishes flow.message events at most every wsEventEvery
// and the flow's message counts at most every wsSummaryEvery.
func (p *Proxy) followMessages(log *wsLog, f flow.Flow, done <-chan struct{}) {
	tick := time.NewTicker(wsEventEvery)
	defer tick.Stop()
	var told uint64
	lastSummary := time.Now()
	var summarised uint64
	for {
		stop := false
		select {
		case <-done:
			stop = true
		case <-tick.C:
		}
		log.mu.Lock()
		seq, dir, op, n := log.sum.Count, log.lastDir, log.lastOp, log.lastLen
		log.mu.Unlock()
		if seq > told {
			p.wire.ws.publish(MessageEvent{FlowID: log.flowID, Seq: seq, Dir: dir, Opcode: op, Bytes: n, Coalesced: seq - told})
			told = seq
		}
		if !stop && seq > summarised && time.Since(lastSummary) >= wsSummaryEvery {
			s := log.summary()
			g := f
			g.Messages = &s
			p.opts.Store.Update(g)
			summarised, lastSummary = seq, time.Now()
		}
		if stop {
			return
		}
	}
}

func (r *wsRegistry) publish(e MessageEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// SubscribeMessages delivers flow.message events until ctx ends. A slow
// subscriber loses events; it reads the messages route again.
func (p *Proxy) SubscribeMessages(ctx context.Context) <-chan MessageEvent {
	r := &p.wire.ws
	ch := make(chan MessageEvent, 64)
	r.mu.Lock()
	if r.subs == nil {
		r.subs = map[chan MessageEvent]struct{}{}
	}
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	go func() {
		<-ctx.Done()
		r.mu.Lock()
		delete(r.subs, ch)
		r.mu.Unlock()
	}()
	return ch
}

// Messages lists a flow's messages with seq above since, at most limit of
// them. ok is false when no messages are kept for the flow.
func (p *Proxy) Messages(flowID string, since uint64, limit int) (MessagePage, bool) {
	r := &p.wire.ws
	r.mu.Lock()
	log := r.logs[flowID]
	r.mu.Unlock()
	page := MessagePage{FlowID: flowID, Messages: []WSMessage{}, Next: since}
	if log == nil {
		return page, false
	}
	if limit <= 0 || limit > wsMaxMessages {
		limit = wsMaxMessages
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, m := range log.msgs {
		if m.Seq <= since {
			continue
		}
		if len(page.Messages) >= limit {
			break
		}
		page.Messages = append(page.Messages, m)
		page.Next = m.Seq
	}
	if len(page.Messages) < limit && log.sum.Count > page.Next {
		page.Next = log.sum.Count
	}
	page.MessageSummary = log.sum
	page.Open = !log.ended.Load()
	return page, true
}

// newWSLog starts the log of one flow, forgetting logs of flows the Store
// no longer holds and, past wsMaxLogs, the oldest finished ones.
func (p *Proxy) newWSLog(id string) *wsLog {
	r := &p.wire.ws
	log := &wsLog{flowID: id, reg: r}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.logs == nil {
		r.logs = map[string]*wsLog{}
	}
	keep := r.order[:0]
	for _, old := range r.order {
		l := r.logs[old]
		if l == nil {
			continue
		}
		if l.ended.Load() {
			if _, ok := p.opts.Store.Get(old); !ok {
				r.dropLocked(old)
				continue
			}
		}
		keep = append(keep, old)
	}
	r.order = keep
	for len(r.order) >= wsMaxLogs && r.evictOneLocked("") {
	}
	r.logs[id] = log
	r.order = append(r.order, id)
	return log
}

func (r *wsRegistry) dropLocked(id string) {
	if l := r.logs[id]; l != nil {
		r.kept -= l.kept.Load()
		delete(r.logs, id)
	}
}

// evictOneLocked forgets the oldest finished log other than keep.
func (r *wsRegistry) evictOneLocked(keep string) bool {
	for i, id := range r.order {
		l := r.logs[id]
		if id == keep || l == nil || !l.ended.Load() {
			continue
		}
		r.dropLocked(id)
		r.order = append(r.order[:i:i], r.order[i+1:]...)
		return true
	}
	return false
}

// reserve makes room for n payload bytes across all logs.
func (r *wsRegistry) reserve(owner string, n int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.kept+n > wsRegistryBudget {
		if !r.evictOneLocked(owner) {
			return false
		}
	}
	r.kept += n
	return true
}

func (l *wsLog) summary() flow.MessageSummary {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sum
}

func (l *wsLog) fail(dir, reason string) {
	l.mu.Lock()
	if l.sum.ParseError == "" {
		l.sum.ParseError = dir + " frames: " + reason + "; later " + dir + " messages are relayed but not listed"
	}
	l.mu.Unlock()
}

// add records one message; payload is what it carried, inflated when it could be.
func (l *wsLog) add(m WSMessage, payload []byte, truncated bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sum.Count++
	l.sum.Bytes += m.Bytes
	m.Seq = l.sum.Count
	l.lastDir, l.lastOp, l.lastLen = m.Dir, m.Opcode, m.Bytes
	if m.Opcode == "close" && l.sum.ClosedBy == "" {
		l.sum.ClosedBy = m.Dir
		l.sum.CloseCode = m.CloseCode
		if utf8.Valid(payload) {
			l.sum.CloseReason = string(payload)
		}
	}
	room := int64(wsMaxPayload) - l.kept.Load()
	if len(l.msgs) >= wsMaxMessages || room <= 0 {
		l.sum.Dropped++
		return
	}
	limit := min(int64(wsMessageCap), room)
	if int64(len(payload)) > limit {
		payload, truncated = payload[:limit], true
	}
	if !l.reg.reserve(l.flowID, int64(len(payload))) {
		l.sum.Dropped++
		return
	}
	l.kept.Add(int64(len(payload)))
	m.Truncated = truncated
	textual := m.Opcode != "binary" && !m.NotDecoded
	if textual {
		shown := payload
		if truncated {
			for i := 0; i < 3 && len(shown) > 0 && !utf8.Valid(shown); i++ {
				shown = shown[:len(shown)-1]
			}
		}
		if utf8.Valid(shown) {
			s := string(shown)
			m.Text = &s
		}
	}
	if m.Text == nil && len(payload) > 0 {
		s := base64.StdEncoding.EncodeToString(payload)
		m.Base64 = &s
	}
	l.msgs = append(l.msgs, m)
}

// deflateParams reads permessage-deflate (RFC 7692) from the server's answer:
// whether each side keeps its compression context between messages.
func deflateParams(h http.Header) (clientTakeover, serverTakeover, on bool) {
	for _, v := range h.Values("Sec-WebSocket-Extensions") {
		for _, ext := range strings.Split(v, ",") {
			parts := strings.Split(ext, ";")
			if !strings.EqualFold(strings.TrimSpace(parts[0]), "permessage-deflate") {
				continue
			}
			clientTakeover, serverTakeover = true, true
			for _, param := range parts[1:] {
				name, _, _ := strings.Cut(strings.TrimSpace(param), "=")
				switch strings.ToLower(strings.TrimSpace(name)) {
				case "client_no_context_takeover":
					clientTakeover = false
				case "server_no_context_takeover":
					serverTakeover = false
				}
			}
			return clientTakeover, serverTakeover, true
		}
	}
	return false, false, false
}

// inflater undoes permessage-deflate for one direction. With context
// takeover the last 32 KiB inflated are the dictionary of the next message;
// once a message cannot be inflated that history is lost, so later messages
// are marked not decoded.
type inflater struct {
	takeover bool
	hist     []byte
	lost     bool
}

func (in *inflater) inflate(data []byte) ([]byte, bool) {
	if in.lost {
		return nil, false
	}
	src := make([]byte, 0, len(data)+9)
	src = append(append(src, data...), 0x00, 0x00, 0xff, 0xff, 0x01, 0x00, 0x00, 0xff, 0xff)
	var r io.ReadCloser
	if in.takeover && len(in.hist) > 0 {
		r = flate.NewReaderDict(bytes.NewReader(src), in.hist)
	} else {
		r = flate.NewReader(bytes.NewReader(src))
	}
	out, err := io.ReadAll(io.LimitReader(r, wsInflateOutput+1))
	_ = r.Close()
	if err != nil || len(out) > wsInflateOutput {
		in.lose()
		return nil, false
	}
	if in.takeover {
		in.hist = append(in.hist, out...)
		if len(in.hist) > deflateWindow {
			in.hist = append([]byte(nil), in.hist[len(in.hist)-deflateWindow:]...)
		}
	}
	return out, true
}

func (in *inflater) lose() {
	if in.takeover {
		in.lost, in.hist = true, nil
	}
}

// tapConn hands every byte it reads to tap as well.
type tapConn struct {
	net.Conn
	tap io.Writer
}

func (c *tapConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		_, _ = c.tap.Write(p[:n])
	}
	return n, err
}

// CloseWrite half-closes the underlying connection when it can.
func (c *tapConn) CloseWrite() error {
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return fmt.Errorf("no half-close")
}

// wsParser follows the frames of one direction as their bytes pass.
type wsParser struct {
	log  *wsLog
	dir  string
	infl *inflater

	hdr       [14]byte
	hdrN      int
	inPayload bool
	fin       bool
	rsv1      bool
	op        byte
	masked    bool
	mask      [4]byte
	remain    uint64
	pos       uint64
	broken    bool
	scratch   []byte

	ctrl []byte

	inMsg     bool
	msgOp     byte
	msgComp   bool
	msgBuf    []byte
	msgLen    int64
	msgFrames int
	msgOver   bool
	msgStart  time.Time
}

func (w *wsParser) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 && !w.broken {
		if !w.inPayload {
			p = w.readHeader(p)
			continue
		}
		take := uint64(len(p))
		if take > w.remain {
			take = w.remain
		}
		w.payload(p[:take])
		p = p[take:]
		w.remain -= take
		if w.remain == 0 {
			w.endFrame()
		}
	}
	return n, nil
}

func headerLen(b1 byte) int {
	n := 2
	switch b1 & 0x7f {
	case 126:
		n += 2
	case 127:
		n += 8
	}
	if b1&0x80 != 0 {
		n += 4
	}
	return n
}

func (w *wsParser) readHeader(p []byte) []byte {
	for {
		need := 2
		if w.hdrN >= 2 {
			need = headerLen(w.hdr[1])
		}
		if w.hdrN >= need {
			break
		}
		if len(p) == 0 {
			return p
		}
		w.hdr[w.hdrN] = p[0]
		w.hdrN++
		p = p[1:]
	}
	w.startFrame()
	return p
}

func (w *wsParser) startFrame() {
	h := w.hdr[:w.hdrN]
	w.hdrN = 0
	w.fin, w.rsv1, w.op = h[0]&0x80 != 0, h[0]&0x40 != 0, h[0]&0x0f
	w.masked = h[1]&0x80 != 0
	length := uint64(h[1] & 0x7f)
	i := 2
	switch length {
	case 126:
		length = uint64(h[2])<<8 | uint64(h[3])
		i = 4
	case 127:
		length = 0
		for _, b := range h[2:10] {
			length = length<<8 | uint64(b)
		}
		i = 10
		if length>>63 != 0 {
			w.stop("a frame length beyond the format's range")
			return
		}
	}
	if w.masked {
		copy(w.mask[:], h[i:i+4])
	}
	w.remain, w.pos = length, 0
	switch {
	case w.op >= 8 && w.op <= 10:
		if !w.fin || length > 125 {
			w.stop("a control frame that is fragmented or longer than 125 bytes")
			return
		}
		w.ctrl = w.ctrl[:0]
	case w.op == 0:
		if !w.inMsg {
			w.stop("a continuation frame outside a message")
			return
		}
	case w.op == 1 || w.op == 2:
		if w.inMsg {
			w.stop("a new message inside a fragmented one")
			return
		}
		w.inMsg, w.msgOp, w.msgComp, w.msgStart = true, w.op, w.rsv1, time.Now()
		w.msgBuf, w.msgLen, w.msgFrames, w.msgOver = w.msgBuf[:0], 0, 0, false
	default:
		w.stop(fmt.Sprintf("an unknown opcode %d", w.op))
		return
	}
	if w.op < 8 {
		w.msgFrames++
	}
	w.inPayload = true
	if length == 0 {
		w.endFrame()
	}
}

func (w *wsParser) stop(reason string) {
	w.broken = true
	w.log.fail(w.dir, reason)
}

func (w *wsParser) payload(chunk []byte) {
	buf := append(w.scratch[:0], chunk...)
	if w.masked {
		for i := range buf {
			buf[i] ^= w.mask[(w.pos+uint64(i))%4]
		}
	}
	w.pos += uint64(len(buf))
	w.scratch = buf[:0]
	if w.op >= 8 {
		w.ctrl = append(w.ctrl, buf...)
		return
	}
	w.msgLen += int64(len(buf))
	limit := wsMessageCap
	if w.msgComp && w.infl != nil {
		limit = wsInflateInput
	}
	if room := limit - len(w.msgBuf); room < len(buf) {
		if room > 0 {
			w.msgBuf = append(w.msgBuf, buf[:room]...)
		}
		w.msgOver = true
		return
	}
	w.msgBuf = append(w.msgBuf, buf...)
}

func (w *wsParser) endFrame() {
	w.inPayload = false
	if w.op >= 8 {
		m := WSMessage{Dir: w.dir, Time: time.Now(), Opcode: opcodeName(w.op), Bytes: int64(len(w.ctrl))}
		payload := w.ctrl
		if w.op == 8 && len(payload) >= 2 {
			m.CloseCode = int(payload[0])<<8 | int(payload[1])
			payload = payload[2:]
		}
		w.log.add(m, append([]byte(nil), payload...), false)
		return
	}
	if !w.fin {
		return
	}
	w.inMsg = false
	m := WSMessage{Dir: w.dir, Time: w.msgStart, Opcode: opcodeName(w.msgOp), Bytes: w.msgLen, Compressed: w.msgComp}
	if w.msgFrames > 1 {
		m.Frames = w.msgFrames
	}
	payload, truncated := w.msgBuf, w.msgOver
	if w.msgComp {
		switch {
		case w.infl == nil:
			m.NotDecoded = true
		case w.msgOver:
			w.infl.lose()
			m.NotDecoded = true
		default:
			if out, ok := w.infl.inflate(w.msgBuf); ok {
				payload, truncated = out, false
				m.Inflated = int64(len(out))
			} else {
				m.NotDecoded = true
			}
		}
	}
	w.log.add(m, append([]byte(nil), payload[:min(len(payload), wsMessageCap)]...), truncated || len(payload) > wsMessageCap)
	if cap(w.msgBuf) > wsMessageCap {
		w.msgBuf = nil
	}
}

func opcodeName(op byte) string {
	switch op {
	case 0:
		return "continuation"
	case 1:
		return "text"
	case 2:
		return "binary"
	case 8:
		return "close"
	case 9:
		return "ping"
	case 10:
		return "pong"
	}
	return fmt.Sprintf("opcode %d", op)
}
