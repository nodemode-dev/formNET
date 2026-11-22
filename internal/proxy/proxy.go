// Package proxy is the HTTP(S) proxy formnet records through. A program
// pointed at it (a browser, curl, an app that honours proxy settings) sends
// plain requests and CONNECT tunnels; the proxy forwards them, and with the
// local CA trusted it opens the TLS inside the tunnel and records the exchange
// as a flow. Hosts on the passthrough list, and every host while interception
// is off, are tunnelled as they are and only counted.
package proxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/flow"
)

const (
	dialTimeout      = 10 * time.Second
	handshakeTimeout = 15 * time.Second
	peekTimeout      = 5 * time.Second
	shutdownWait     = 3 * time.Second
)

// Status is what the API reports about the proxy.
type Status struct {
	Addr          string   `json:"addr"`
	Running       bool     `json:"running"`
	Intercept     bool     `json:"intercept"`
	InterceptMode string   `json:"intercept_mode"` // auto (follows the root's trust), on or off
	Passthrough   []string `json:"passthrough"`
	Error         string   `json:"error,omitempty"`
}

// Options configure a Proxy.
type Options struct {
	CA          *ca.CA
	Store       *flow.Store
	BodyLimit   int64
	Passthrough []string
	Intercept   bool
	// InterceptMode is auto, on or off; empty means on when Intercept is set, off otherwise.
	// In auto, HTTPS is opened while Trusted reports true, read again every TrustInterval.
	InterceptMode string
	Trusted       func() bool
	TrustInterval time.Duration
	// Owner names the local program behind a client address (ip:port), when known.
	Owner func(clientAddr string) (pid int, name string, ok bool)
	Log   *slog.Logger
	// UpstreamRootCAs replaces the system roots the proxy trusts in upstream
	// servers; tests use it, a deployment leaves it nil.
	UpstreamRootCAs *x509.CertPool
	// Refuse lists ip:port destinations the proxy never connects to: formnet's
	// own API and proxy addresses, so a page cannot drive the proxy into them.
	Refuse []string
	// Resolver looks destination names up; nil means the system resolver.
	Resolver Resolver
	// TunnelIdle closes a tunnel or WebSocket relay that carried nothing for
	// this long; zero means DefaultTunnelIdle.
	TunnelIdle time.Duration
}

// DefaultTunnelIdle is how long a silent tunnel stays open.
const DefaultTunnelIdle = 5 * time.Minute

// Proxy is one running proxy.
type Proxy struct {
	opts      Options
	transport *http.Transport
	guard     *destinationGuard
	log       *slog.Logger

	mu          sync.Mutex
	status      Status
	intercept   bool
	mode        string
	passthrough []string
	hijacked    map[net.Conn]struct{}
	serveCtx    context.Context // ends when Serve stops; resends run under it

	refusedMu sync.Mutex
	refused   map[string]time.Time // last refused handshake per program and host

	ext atomic.Pointer[trafficState] // rules, throttle, breakpoints and remote capture (traffic.go)
}

// DefaultTrustInterval is how often auto mode reads the root's trust state again.
const DefaultTrustInterval = 10 * time.Second

// New prepares a proxy; ListenAndServe starts it.
func New(opts Options) *Proxy {
	if opts.Store == nil {
		opts.Store = flow.NewStore(1)
	}
	if opts.BodyLimit <= 0 {
		opts.BodyLimit = 256 << 10
	}
	if opts.TunnelIdle <= 0 {
		opts.TunnelIdle = DefaultTunnelIdle
	}
	if opts.TrustInterval <= 0 {
		opts.TrustInterval = DefaultTrustInterval
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	mode := opts.InterceptMode
	if mode == "" {
		mode = ModeOff
		if opts.Intercept {
			mode = ModeOn
		}
	}
	p := &Proxy{
		opts:        opts,
		log:         log,
		mode:        mode,
		intercept:   mode == ModeOn || (mode == ModeAuto && opts.Trusted != nil && opts.Trusted()),
		passthrough: append([]string(nil), opts.Passthrough...),
		hijacked:    map[net.Conn]struct{}{},
		guard:       newGuard(opts.Refuse, opts.Resolver),
	}
	p.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           p.guard.DialContext,
		TLSClientConfig:       p.upstreamTLS(""),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   handshakeTimeout,
		ExpectContinueTimeout: time.Second,
		// The proxy never asks for compression itself and never decodes
		// what it relays; the client's own Accept-Encoding goes through as sent.
		DisableCompression: true,
	}
	return p
}

func (p *Proxy) upstreamTLS(serverName string) *tls.Config {
	return &tls.Config{RootCAs: p.opts.UpstreamRootCAs, ServerName: serverName, MinVersion: tls.VersionTLS12}
}

// ListenAndServe serves on addr until ctx ends.
func (p *Proxy) ListenAndServe(ctx context.Context, addr string) error {
	if err := requireLoopback(addr); err != nil {
		p.setError(err)
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		p.setError(err)
		return fmt.Errorf("proxy: listen %s: %w", addr, err)
	}
	return p.Serve(ctx, ln)
}

// Serve serves on a loopback listener the caller bound until ctx ends.
func (p *Proxy) Serve(ctx context.Context, ln net.Listener) error {
	addr := ln.Addr().String()
	if err := requireLoopback(addr); err != nil {
		_ = ln.Close()
		p.setError(err)
		return err
	}
	p.setRunning(addr, true)
	p.mu.Lock()
	p.serveCtx = ctx
	p.mu.Unlock()
	p.log.Info("proxy listening", "addr", ln.Addr().String())
	go p.followTrust(ctx)
	srv := &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), shutdownWait)
			defer cancel()
			_ = srv.Shutdown(shutdown)
			p.closeHijacked()
		case <-done:
		}
	}()
	err := srv.Serve(ln)
	close(done)
	p.setRunning(addr, false)
	p.transport.CloseIdleConnections()
	p.log.Info("proxy stopped", "addr", addr)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("proxy: address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("proxy: %q is not a loopback address; formnet listens on this computer only", addr)
	}
	return nil
}

// ServeHTTP handles one proxied request: a tunnel, an upgrade or a plain exchange.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.preflight(w, r) {
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	if !r.URL.IsAbs() {
		http.Error(w, "formnet is a proxy: send absolute URLs or configure it as your HTTP proxy", http.StatusBadRequest)
		return
	}
	o := origin{client: r.RemoteAddr, scheme: r.URL.Scheme, host: r.URL.Hostname(), port: portOr(r.URL.Port(), r.URL.Scheme)}
	p.serveOrigin(w, r, o)
}

// serveOrigin handles a request whose origin (client, scheme, host) is known,
// directly or inside an opened tunnel.
func (p *Proxy) serveOrigin(w http.ResponseWriter, r *http.Request, o origin) {
	if isWebSocketUpgrade(r.Header) {
		p.handleWebSocket(w, r, o)
		return
	}
	if p.serveHooked(w, r, o) {
		return
	}
	p.forward(w, r, o)
}

// origin says where a request inside the proxy is going and who sent it.
type origin struct {
	client string
	scheme string
	host   string
	port   int
}

func (o origin) hostPort() string {
	return net.JoinHostPort(o.host, fmt.Sprint(o.port))
}

func (o origin) url(r *http.Request) string {
	if r.URL.IsAbs() {
		return r.URL.String()
	}
	hostPort := o.host
	if !((o.scheme == "https" && o.port == 443) || (o.scheme == "http" && o.port == 80)) {
		hostPort = o.hostPort()
	}
	return o.scheme + "://" + hostPort + r.URL.RequestURI()
}

func portOr(port, scheme string) int {
	if port != "" {
		n := 0
		for _, c := range port {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	if scheme == "https" || scheme == "wss" {
		return 443
	}
	return 80
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// newFlow starts the record of one exchange.
func (p *Proxy) newFlow(kind string, o origin, method, rawURL string) flow.Flow {
	f := flow.Flow{ID: newID(), Kind: kind, Start: time.Now(), Pending: true, Client: o.client, Method: method}.WithURL(rawURL)
	if f.Host == "" {
		f.Scheme, f.Host, f.Port = o.scheme, o.host, o.port
	}
	if p.opts.Owner != nil {
		if pid, name, ok := p.opts.Owner(o.client); ok {
			f.ClientPID, f.ClientProcess = pid, name
		}
	}
	return f
}

func (p *Proxy) track(c net.Conn) {
	p.mu.Lock()
	p.hijacked[c] = struct{}{}
	p.mu.Unlock()
}

func (p *Proxy) untrack(c net.Conn) {
	p.mu.Lock()
	delete(p.hijacked, c)
	p.mu.Unlock()
}

func (p *Proxy) closeHijacked() {
	p.mu.Lock()
	conns := make([]net.Conn, 0, len(p.hijacked))
	for c := range p.hijacked {
		conns = append(conns, c)
	}
	p.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func isWebSocketUpgrade(h http.Header) bool {
	if !strings.EqualFold(h.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range h.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
				return true
			}
		}
	}
	return false
}
