package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// tunnel answers a CONNECT the proxy will not open: dial, confirm, relay and count.
func (p *Proxy) tunnel(hj http.Hijacker, o origin) {
	upstream, err := p.guard.DialContext(context.Background(), "tcp", o.hostPort())
	if err != nil {
		conn, _, herr := hj.Hijack()
		if herr != nil {
			return
		}
		defer conn.Close()
		status := statusFor(err)
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Type: text/plain\r\nConnection: close\r\n\r\n%s\r\n", status, http.StatusText(status), err.Error())
		f := p.newFlow(flow.KindTunnel, o, http.MethodConnect, "https://"+o.hostPort())
		f.Status = status
		f.Error = err.Error()
		p.opts.Store.Add(f)
		p.finish(f.Completed(time.Now()))
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	p.track(conn)
	defer p.untrack(conn)
	defer conn.Close()
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		upstream.Close()
		return
	}
	p.relayTo(newPeekedConn(conn, bufrw.Reader), upstream, o)
}

// relay dials the upstream for an already confirmed CONNECT and relays it.
func (p *Proxy) relay(pc *peekedConn, o origin) {
	upstream, err := p.guard.DialContext(context.Background(), "tcp", o.hostPort())
	if err != nil {
		f := p.newFlow(flow.KindTunnel, o, http.MethodConnect, "https://"+o.hostPort())
		f.Status = statusFor(err)
		f.Error = err.Error()
		p.opts.Store.Add(f)
		p.finish(f.Completed(time.Now()))
		return
	}
	p.relayTo(pc, upstream, o)
}

// relayTo copies bytes both ways between the client and upstream, recording one tunnel flow.
func (p *Proxy) relayTo(client *peekedConn, upstream net.Conn, o origin) {
	p.track(upstream)
	defer p.untrack(upstream)
	defer upstream.Close()
	f := p.newFlow(flow.KindTunnel, o, http.MethodConnect, "https://"+o.hostPort())
	if rec := client.peekRecord(peekTimeout); rec != nil {
		f.TLS = &flow.TLSInfo{ServerName: serverName(rec), Intercepted: false}
		if f.TLS.ServerName == "" {
			f.TLS.ServerName = o.host
		}
	}
	upstream, f.Throttled = p.throttleTunnel(upstream, o)
	f = p.opts.Store.Add(f)
	sent, received := pipe(client, upstream, p.opts.TunnelIdle)
	f.BytesSent, f.BytesReceived = sent, received
	p.finish(f.Completed(time.Now()))
}

// pipe copies both ways until both sides are done and returns the byte counts.
// Each direction is half-closed when its source ends, so a server can finish
// answering after the client stops sending; a relay that carries nothing for
// idle is closed on both sides.
func pipe(client, upstream net.Conn, idle time.Duration) (sent, received int64) {
	var up, down counter
	last := &activity{}
	last.touch()
	stopIdle := watchIdle(last, idle, client, upstream)
	defer stopIdle()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.MultiWriter(upstream, &up, last), client)
		closeWrite(upstream)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(io.MultiWriter(client, &down, last), upstream)
		closeWrite(client)
	}()
	wg.Wait()
	return up.Total(), down.Total()
}

// activity remembers when bytes last moved.
type activity struct{ at atomic.Int64 }

func (a *activity) touch()                      { a.at.Store(time.Now().UnixNano()) }
func (a *activity) Write(p []byte) (int, error) { a.touch(); return len(p), nil }
func (a *activity) since() time.Duration        { return time.Since(time.Unix(0, a.at.Load())) }

// watchIdle closes both connections once nothing has moved for idle.
func watchIdle(last *activity, idle time.Duration, conns ...net.Conn) func() {
	done := make(chan struct{})
	go func() {
		step := idle / 4
		if step < 10*time.Millisecond {
			step = 10 * time.Millisecond
		}
		ticker := time.NewTicker(step)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if last.since() >= idle {
					for _, c := range conns {
						_ = c.Close()
					}
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

// closeWriter is the half-close net.TCPConn, tls.Conn and the wrappers here offer.
type closeWriter interface{ CloseWrite() error }

// closeWrite half-closes a connection for writing, so the peer sees EOF but
// can still send what it has left. A connection that cannot half-close is
// left to the idle watch.
func closeWrite(c net.Conn) {
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
	}
}

// serverName reads the SNI out of a TLS ClientHello record, best effort.
func serverName(rec []byte) string {
	if len(rec) < 43 || rec[0] != 0x16 || rec[5] != 0x01 {
		return ""
	}
	b := rec[9:] // past record header (5) and handshake header (4)
	if len(b) < 34 {
		return ""
	}
	b = b[34:] // version (2) + random (32)
	if len(b) < 1 {
		return ""
	}
	sidLen := int(b[0])
	b = b[1:]
	if len(b) < sidLen+2 {
		return ""
	}
	b = b[sidLen:]
	csLen := int(b[0])<<8 | int(b[1])
	b = b[2:]
	if len(b) < csLen+1 {
		return ""
	}
	b = b[csLen:]
	cmLen := int(b[0])
	b = b[1:]
	if len(b) < cmLen+2 {
		return ""
	}
	b = b[cmLen:]
	extLen := int(b[0])<<8 | int(b[1])
	b = b[2:]
	if len(b) < extLen {
		extLen = len(b)
	}
	return sniFromExtensions(b[:extLen])
}

func sniFromExtensions(b []byte) string {
	for len(b) >= 4 {
		typ := int(b[0])<<8 | int(b[1])
		l := int(b[2])<<8 | int(b[3])
		b = b[4:]
		if len(b) < l {
			return ""
		}
		if typ == 0 {
			return sniFromList(b[:l])
		}
		b = b[l:]
	}
	return ""
}

func sniFromList(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	b = b[2:]
	for len(b) >= 3 {
		nameType := b[0]
		l := int(b[1])<<8 | int(b[2])
		b = b[3:]
		if len(b) < l {
			return ""
		}
		if nameType == 0 {
			return string(b[:l])
		}
		b = b[l:]
	}
	return ""
}
