package proxy

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// offerH2 lets a client whose HTTPS FormNET opens choose HTTP/2 or HTTP/1.1 by ALPN.
func offerH2(c *tls.Config) *tls.Config {
	c.NextProtos = []string{"h2", "http/1.1"}
	return c
}

// serveH2 serves one opened TLS connection on which the client chose h2,
// through the standard library's HTTP/2 server. Every stream is its own
// request, handed to serveOrigin like a request read over HTTP/1.1, so rules,
// breakpoints, the throttle, the capture limit and the destination guard
// apply to each. The listener hands out the *tls.Conn itself, which is how
// net/http finds the negotiated protocol, and closes once the server reports
// the connection closed. FormNET never pushes; Go's HTTP/2 client refuses
// pushes from servers, so none arrive to relay.
func (p *Proxy) serveH2(conn *tls.Conn, o origin) {
	ln := &h2Listener{conn: conn, done: make(chan struct{})}
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				http.Error(w, "FormNET does not open tunnels inside an opened HTTP/2 connection", http.StatusMethodNotAllowed)
				return
			}
			p.serveOrigin(w, r, o)
		}),
		Protocols:   &protocols,
		IdleTimeout: 90 * time.Second,
		ConnState: func(c net.Conn, s http.ConnState) {
			if s == http.StateClosed || s == http.StateHijacked {
				ln.finish()
			}
		},
	}
	_ = srv.Serve(ln)
}

// h2Listener hands out one connection, then blocks until it is finished.
type h2Listener struct {
	conn  net.Conn
	given bool
	mu    sync.Mutex
	once  sync.Once
	done  chan struct{}
}

func (l *h2Listener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.given {
		l.given = true
		l.mu.Unlock()
		return l.conn, nil
	}
	l.mu.Unlock()
	<-l.done
	return nil, errors.New("listener closed")
}

func (l *h2Listener) finish() { l.once.Do(func() { close(l.done) }) }

func (l *h2Listener) Close() error {
	l.finish()
	return nil
}

func (l *h2Listener) Addr() net.Addr { return l.conn.LocalAddr() }

// keepTE carries "TE: trailers" to the server (gRPC asks for it and the
// header is otherwise hop-by-hop) and the request's trailers, which net/http
// fills in once the body has been read.
func keepTE(out, in http.Header, trailer http.Header, req *http.Request) {
	for _, v := range in.Values("Te") {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "trailers") {
				out.Set("Te", "trailers")
			}
		}
	}
	if len(trailer) > 0 {
		req.Trailer = trailer
	}
}

// afterBody sends the response's trailers to the client once the body has
// passed (net/http fills resp.Trailer at the end of the body) and records the
// protocol FormNET spoke to the server; a response FormNET made itself (a
// rule's block or local file) has no request and records none. A 407 on an
// exchange sent through the upstream proxy is the upstream asking for
// credentials, and the flow says so.
func afterBody(w http.ResponseWriter, resp *http.Response, f *flow.Flow) {
	h := w.Header()
	for name, values := range resp.Trailer {
		for _, v := range values {
			h.Add(http.TrailerPrefix+name, v)
		}
	}
	if resp.Request == nil {
		return
	}
	f.ServerProto = resp.Proto
	if f.Upstream != "" && resp.StatusCode == http.StatusProxyAuthRequired && f.Error == "" {
		f.Error = "the upstream proxy " + f.Upstream + " asked for credentials (407): set or correct the username and password"
	}
}
