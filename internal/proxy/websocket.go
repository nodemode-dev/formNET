package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

const handshakeLimit = 64 << 10

// handleWebSocket relays an Upgrade: websocket handshake and the frames after it.
func (p *Proxy) handleWebSocket(w http.ResponseWriter, r *http.Request, o origin) {
	rawURL := o.url(r)
	f := p.newFlow(flow.KindWebSocket, o, r.Method, rawURL)
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
	f.Request = newCapture(http.NoBody, 0).message(r.Header, requestLine, 0)
	f.ClientProtocol = r.Proto
	f.Protocol = r.Proto
	f = p.opts.Store.Add(f)

	dialStart := time.Now()
	upstream, err := p.dialUpstream(r.Context(), o)
	if err != nil {
		p.fail(w, f, statusFor(err), "formnet could not reach "+f.Host+": "+err.Error())
		return
	}
	defer upstream.Close()
	p.track(upstream)
	defer p.untrack(upstream)
	stop := context.AfterFunc(r.Context(), func() { upstream.Close() })
	defer stop()
	f.ConnectMS = millis(dialStart, time.Now())
	if tc, ok := upstream.(*tls.Conn); ok {
		cs := tc.ConnectionState()
		f.TLS = tlsInfo(&cs, true)
	}

	out, err := http.NewRequestWithContext(r.Context(), r.Method, rawURL, nil)
	if err != nil {
		p.fail(w, f, http.StatusBadRequest, err.Error())
		return
	}
	out.Header = forwardable(r.Header, true)
	out.Host = r.Host
	wrote := time.Now()
	if err := out.Write(upstream); err != nil {
		p.fail(w, f, http.StatusBadGateway, "handshake not sent: "+err.Error())
		return
	}
	capped := &capReader{r: upstream}
	capped.limit.Store(handshakeLimit)
	reader := bufio.NewReader(capped)
	_ = upstream.SetReadDeadline(time.Now().Add(handshakeTimeout))
	resp, err := http.ReadResponse(reader, out)
	_ = upstream.SetReadDeadline(time.Time{})
	if err != nil {
		p.fail(w, f, http.StatusBadGateway, "no handshake answer: "+err.Error())
		return
	}
	f.TTFBMS = millis(wrote, time.Now())
	capped.limit.Store(-1)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		f.Kind = flow.KindHTTP
		f, _ = p.relayResponse(w, resp, f, &timing{}, newCapture(http.NoBody, 0), requestLine, r.Header)
		p.finish(f)
		return
	}
	p.relayUpgrade(w, resp, reader, upstream, f)
}

// dialUpstream connects through the guard and, for https, opens TLS on top.
func (p *Proxy) dialUpstream(ctx context.Context, o origin) (net.Conn, error) {
	raw, err := p.guard.DialContext(ctx, "tcp", o.hostPort())
	if err != nil {
		return nil, err
	}
	if o.scheme != "https" {
		return raw, nil
	}
	tc := tls.Client(raw, p.upstreamTLS(o.host))
	_ = tc.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := tc.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	_ = tc.SetDeadline(time.Time{})
	return tc, nil
}

// relayUpgrade hands the 101 to the client and then copies frames both ways.
func (p *Proxy) relayUpgrade(w http.ResponseWriter, resp *http.Response, upstreamReader *bufio.Reader, upstream net.Conn, f flow.Flow) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		p.fail(w, f, http.StatusInternalServerError, "formnet cannot take over this connection")
		return
	}
	client, bufrw, err := hj.Hijack()
	if err != nil {
		p.fail(w, f, http.StatusInternalServerError, "formnet cannot take over this connection")
		return
	}
	p.track(client)
	defer p.untrack(client)
	defer client.Close()
	statusLine := fmt.Sprintf("%s %s", resp.Proto, resp.Status)
	res := newCapture(http.NoBody, 0).message(resp.Header, statusLine, 0)
	f.Response = &res
	f.Status = resp.StatusCode
	f.Protocol = resp.Proto
	if err := resp.Write(client); err != nil {
		f.Error = "handshake not delivered: " + err.Error()
		p.finish(f.Completed(time.Now()))
		return
	}
	f, _ = p.opts.Store.Update(f)
	clientSide := newPeekedConn(client, bufrw.Reader)
	var upstreamSide net.Conn = upstream
	if upstreamReader.Buffered() > 0 {
		upstreamSide = &readerConn{Conn: upstream, r: io.MultiReader(io.LimitReader(upstreamReader, int64(upstreamReader.Buffered())), upstream)}
	}
	sent, received := pipe(clientSide, upstreamSide, p.opts.TunnelIdle)
	f.BytesSent = f.Request.HeaderBytes + sent
	f.BytesReceived = res.HeaderBytes + received
	p.finish(f.Completed(time.Now()))
}

// capReader reads at most limit bytes until the limit is lifted (set below zero).
type capReader struct {
	r     io.Reader
	limit atomic.Int64
	read  int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if limit := c.limit.Load(); limit >= 0 {
		if c.read >= limit {
			return 0, errors.New("handshake response too large")
		}
		if int64(len(p)) > limit-c.read {
			p = p[:limit-c.read]
		}
	}
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

// readerConn reads from r (buffered bytes first) and otherwise behaves as Conn.
type readerConn struct {
	net.Conn
	r io.Reader
}

func (c *readerConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// CloseWrite half-closes the underlying connection when it can.
func (c *readerConn) CloseWrite() error {
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("no half-close")
}
