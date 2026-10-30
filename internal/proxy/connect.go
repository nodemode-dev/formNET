package proxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// handleConnect takes over a CONNECT: either opens the TLS inside it with the
// local root and serves the requests it carries, or tunnels it and counts bytes.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host, portText, err := net.SplitHostPort(r.Host)
	if err != nil {
		host, portText = r.Host, "443"
	}
	o := origin{client: r.RemoteAddr, scheme: "https", host: host, port: portOr(portText, "https")}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "formnet cannot take over this connection", http.StatusInternalServerError)
		return
	}
	if _, err := p.guard.resolve(r.Context(), o.hostPort()); err != nil {
		p.refuseConnect(w, o, err)
		return
	}
	if !p.shouldIntercept(host) {
		p.tunnel(hj, o)
		return
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		http.Error(w, "formnet cannot take over this connection", http.StatusInternalServerError)
		return
	}
	p.track(conn)
	defer p.untrack(conn)
	defer conn.Close()
	if _, err := io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	pc := newPeekedConn(conn, bufrw.Reader)
	first, err := pc.peek(1, peekTimeout)
	if err != nil {
		// Nothing arrived: a server-first protocol, or an idle client. Tunnel it as it is.
		p.relay(pc, o)
		return
	}
	if first[0] == 0x16 {
		p.serveTLS(pc, o)
		return
	}
	o.scheme = "http"
	p.serveInside(pc, o)
}

// refuseConnect answers a CONNECT the guard rejected and records it.
func (p *Proxy) refuseConnect(w http.ResponseWriter, o origin, err error) {
	f := p.newFlow(flow.KindTunnel, o, http.MethodConnect, "https://"+o.hostPort())
	p.opts.Store.Add(f)
	p.fail(w, f, statusFor(err), err.Error())
}

// serveTLS opens the client's TLS with a leaf for the CONNECT host and serves
// the HTTP it carries as https requests.
func (p *Proxy) serveTLS(pc *peekedConn, o origin) {
	tlsConn := tls.Server(pc, p.opts.CA.TLSConfigFor(o.host))
	_ = tlsConn.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := tlsConn.Handshake(); err != nil {
		p.log.Debug("client refused the opened connection", "host", o.host, "err", err)
		return
	}
	_ = tlsConn.SetDeadline(time.Time{})
	p.serveInside(tlsConn, o)
}

// serveInside serves HTTP/1.1 on one connection, each request forwarded as from origin.
func (p *Proxy) serveInside(conn net.Conn, o origin) {
	ln := newOneConnListener(conn)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serveOrigin(w, r, o) }),
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	_ = srv.Serve(ln)
}

// peekedConn is a connection whose first bytes may already sit in a buffer.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func newPeekedConn(c net.Conn, buffered *bufio.Reader) *peekedConn {
	var src io.Reader = c
	if buffered != nil && buffered.Buffered() > 0 {
		src = io.MultiReader(io.LimitReader(buffered, int64(buffered.Buffered())), c)
	}
	return &peekedConn{Conn: c, r: bufio.NewReaderSize(src, 16384)}
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// CloseWrite half-closes the underlying connection when it can.
func (c *peekedConn) CloseWrite() error {
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("no half-close")
}

// peek returns the next n bytes without consuming them, waiting at most d.
func (c *peekedConn) peek(n int, d time.Duration) ([]byte, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(d))
	defer c.Conn.SetReadDeadline(time.Time{})
	return c.r.Peek(n)
}

// peekRecord returns the first TLS record without consuming it, or nil when
// what arrives first is not a TLS record, or nothing arrives within d. A
// record split across segments gets a short second wait for the rest.
func (c *peekedConn) peekRecord(d time.Duration) []byte {
	head, err := c.peek(5, d)
	if err != nil || len(head) < 5 || head[0] != 0x16 {
		return nil
	}
	n := 5 + (int(head[3])<<8 | int(head[4]))
	if n > 16384 {
		n = 16384
	}
	rec, err := c.peek(n, time.Second)
	if err != nil {
		if buffered, berr := c.r.Peek(c.r.Buffered()); berr == nil {
			return buffered
		}
		return nil
	}
	return rec
}

// oneConnListener hands out one connection, then blocks until it closes.
type oneConnListener struct {
	conn   *closeNotifyConn
	once   sync.Once
	closed chan struct{}
}

type closeNotifyConn struct {
	net.Conn
	once sync.Once
	done chan struct{}
}

func (c *closeNotifyConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.done) })
	return err
}

// CloseWrite half-closes the underlying connection when it can.
func (c *closeNotifyConn) CloseWrite() error {
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("no half-close")
}

func newOneConnListener(c net.Conn) *oneConnListener {
	return &oneConnListener{conn: &closeNotifyConn{Conn: c, done: make(chan struct{})}, closed: make(chan struct{})}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var out net.Conn
	l.once.Do(func() { out = l.conn })
	if out != nil {
		return out, nil
	}
	select {
	case <-l.conn.done:
	case <-l.closed:
	}
	return nil, errors.New("listener closed")
}

func (l *oneConnListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *oneConnListener) Addr() net.Addr { return l.conn.LocalAddr() }
