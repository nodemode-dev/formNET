package proxy

import (
	"net"
	"net/http"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// askOwner asks who holds a new client connection as soon as it is
// accepted, so a background lookup has the most time before a short-lived
// program exits; the answer is used when the flow is stored or finished.
func (p *Proxy) askOwner(c net.Conn, st http.ConnState) {
	if st == http.StateNew && p.opts.Owner != nil {
		p.opts.Owner(c.RemoteAddr().String())
	}
}

// LoadMessages keeps the messages of a WebSocket flow opened from a session
// file or imported, under the same bounds as captured ones, so the messages
// route lists them. The flow must already be in the Store.
func (p *Proxy) LoadMessages(flowID string, msgs []flow.SessionMessage, sum *flow.MessageSummary) {
	log := p.newWSLog(flowID)
	log.ended.Store(true)
	for _, m := range msgs {
		payload := m.Payload()
		log.add(WSMessage{Dir: m.Dir, Time: m.Time, Opcode: m.Opcode, Bytes: m.Bytes, Compressed: m.Compressed, NotDecoded: m.NotDecoded, Inflated: m.Inflated, Frames: m.Frames, CloseCode: m.CloseCode}, payload, m.Truncated)
	}
	if sum != nil {
		log.mu.Lock()
		if sum.Count >= log.sum.Count {
			log.sum = *sum
		}
		log.mu.Unlock()
	}
}

// SessionMessages returns the kept messages of a flow in the shape a session file holds.
func (p *Proxy) SessionMessages(flowID string) []flow.SessionMessage {
	page, ok := p.Messages(flowID, 0, 0)
	if !ok {
		return nil
	}
	out := make([]flow.SessionMessage, 0, len(page.Messages))
	for _, m := range page.Messages {
		out = append(out, flow.SessionMessage{Seq: m.Seq, Dir: m.Dir, Time: m.Time, Opcode: m.Opcode, Text: m.Text, Base64: m.Base64, Bytes: m.Bytes, Truncated: m.Truncated, Compressed: m.Compressed, NotDecoded: m.NotDecoded, Inflated: m.Inflated, Frames: m.Frames, CloseCode: m.CloseCode})
	}
	return out
}
