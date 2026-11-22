package proxy

import (
	"encoding/pem"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/rules"
)

// remoteRefresh is how often the computer's network addresses are read again while remote capture is on.
const remoteRefresh = 10 * time.Second

// RemoteStatus is what GET /api/remote answers.
type RemoteStatus struct {
	Enabled     bool     `json:"enabled"`
	Allow       []string `json:"allow"`
	FromEnv     bool     `json:"from_env"`  // FORMNET_REMOTE_CAPTURE or FORMNET_REMOTE_ALLOW chose the setting at start
	Port        int      `json:"port"`      // the proxy port to type into the other device
	Addresses   []string `json:"addresses"` // this computer's network addresses
	Listening   []string `json:"listening"` // ip:port the proxy listens on for other devices
	Refused     uint64   `json:"refused"`   // connections from addresses outside the allow list, closed unread
	LastRefused string   `json:"last_refused,omitempty"`
	CertURL     string   `json:"cert_url"`
	Error       string   `json:"error,omitempty"`
}

// remote listens for the proxy on the computer's network addresses for the
// addresses on the allow list. The API and the page never listen there.
type remote struct {
	p       *Proxy
	fromEnv bool

	mu          sync.Mutex
	enabled     bool
	allow       []string
	nets        []netip.Prefix
	srv         *http.Server
	listeners   map[string]net.Listener
	connMu      sync.Mutex // guards conns apart from mu: the server closes connections while mu is held
	conns       map[*remoteConn]struct{}
	stop        chan struct{}
	errs        map[string]string
	lastRefused string
	refused     atomic.Uint64
}

func newRemote(p *Proxy, fromEnv bool) *remote {
	return &remote{p: p, fromEnv: fromEnv, allow: []string{}, listeners: map[string]net.Listener{}, conns: map[*remoteConn]struct{}{}, errs: map[string]string{}}
}

// ParseAllow reads an allow list: CIDR ranges or single addresses.
func ParseAllow(list []string) ([]string, []netip.Prefix, error) {
	if len(list) > 64 {
		return nil, nil, &rules.FieldError{Field: "allow", Reason: "at most 64 entries"}
	}
	clean := make([]string, 0, len(list))
	nets := make([]netip.Prefix, 0, len(list))
	for i, raw := range list {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		var pfx netip.Prefix
		if strings.Contains(s, "/") {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, nil, &rules.FieldError{Field: fmt.Sprintf("allow[%d]", i), Reason: strconv.Quote(s) + " is not an address range such as 192.168.1.0/24"}
			}
			pfx = p.Masked()
		} else {
			a, err := netip.ParseAddr(s)
			if err != nil || a.Zone() != "" {
				return nil, nil, &rules.FieldError{Field: fmt.Sprintf("allow[%d]", i), Reason: strconv.Quote(s) + " is not an address such as 192.168.1.23"}
			}
			pfx = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		if pfx.Bits() == 0 {
			return nil, nil, &rules.FieldError{Field: fmt.Sprintf("allow[%d]", i), Reason: "a range that holds every address is not an allow list"}
		}
		clean = append(clean, pfx.String())
		nets = append(nets, pfx)
	}
	return clean, nets, nil
}

func (rm *remote) allowed(addr net.Addr) bool {
	ap, err := netip.ParseAddrPort(addr.String())
	if err != nil {
		return false
	}
	ip := ap.Addr().Unmap()
	rm.mu.Lock()
	defer rm.mu.Unlock()
	for _, n := range rm.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// SetRemote turns capture from other devices on or off and sets the allow list.
func (p *Proxy) SetRemote(enabled bool, allow []string) (RemoteStatus, error) {
	return p.setRemote(enabled, allow, true)
}

// StartRemote applies the remote setting a run starts with, without keeping it:
// it may come from the environment, which is never written to the settings file.
func (p *Proxy) StartRemote(enabled bool, allow []string) (RemoteStatus, error) {
	return p.setRemote(enabled, allow, false)
}

func (p *Proxy) setRemote(enabled bool, allow []string, keep bool) (RemoteStatus, error) {
	st := p.ext.Load()
	if st == nil {
		return RemoteStatus{}, ErrTrafficUnavailable
	}
	rm := st.remote
	clean, nets, err := ParseAllow(allow)
	if err != nil {
		return rm.status(), err
	}
	if enabled && len(clean) == 0 {
		return rm.status(), &rules.FieldError{Field: "allow", Reason: "at least one address or range is required to turn capture from other devices on"}
	}
	if err := rm.set(enabled, clean, nets); err != nil {
		return rm.status(), err
	}
	if keep && st.opts.RememberRemote != nil {
		if err := st.opts.RememberRemote(enabled, clean); err != nil {
			p.log.Warn("settings file", "err", err)
		}
	}
	return rm.status(), nil
}

// Remote reports capture from other devices.
func (p *Proxy) Remote() RemoteStatus {
	st := p.ext.Load()
	if st == nil {
		return RemoteStatus{Allow: []string{}, Addresses: []string{}, Listening: []string{}, CertURL: "http://" + CertHost + "/"}
	}
	return st.remote.status()
}

func (rm *remote) set(enabled bool, allow []string, nets []netip.Prefix) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.allow, rm.nets = allow, nets
	switch {
	case enabled && rm.srv == nil:
		if !rm.p.Status().Running {
			return badInput("the proxy is not running, so it cannot listen for other devices")
		}
		rm.enabled = true
		rm.srv = &http.Server{Handler: rm.p, ReadHeaderTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
		rm.stop = make(chan struct{})
		rm.refreshLocked()
		go rm.follow(rm.stop)
		rm.p.log.Info("capture from other devices is on", "listening", strings.Join(rm.listeningLocked(), " "), "allow", strings.Join(allow, " "))
	case !enabled && rm.srv != nil:
		rm.enabled = false
		rm.shutdownLocked()
		rm.p.log.Info("capture from other devices is off")
	default:
		rm.enabled = enabled
	}
	rm.connMu.Lock()
	for c := range rm.conns {
		if !rm.containsLocked(c.ip) {
			_ = c.Conn.Close()
		}
	}
	rm.connMu.Unlock()
	return nil
}

func (rm *remote) containsLocked(ip netip.Addr) bool {
	for _, n := range rm.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// follow reads the network addresses again while remote capture is on, and
// stops it when the proxy stops.
func (rm *remote) follow(stop chan struct{}) {
	ctx := rm.p.baseContext()
	t := time.NewTicker(remoteRefresh)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			rm.mu.Lock()
			if rm.stop == stop {
				rm.shutdownLocked()
			}
			rm.mu.Unlock()
			return
		case <-t.C:
			rm.mu.Lock()
			if rm.stop == stop {
				rm.refreshLocked()
			}
			rm.mu.Unlock()
		}
	}
}

func (rm *remote) shutdownLocked() {
	if rm.stop != nil {
		close(rm.stop)
		rm.stop = nil
	}
	if rm.srv != nil {
		_ = rm.srv.Close()
		rm.srv = nil
	}
	for addr, ln := range rm.listeners {
		_ = ln.Close()
		delete(rm.listeners, addr)
	}
	rm.connMu.Lock()
	for c := range rm.conns {
		_ = c.Conn.Close()
	}
	rm.connMu.Unlock()
	rm.errs = map[string]string{}
	rm.p.guard.setExtra(nil)
}

// networkAddresses lists this computer's unicast addresses other devices can reach.
func networkAddresses() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || n.IP.IsLoopback() || !n.IP.IsGlobalUnicast() {
			continue
		}
		if ip, ok := netip.AddrFromSlice(n.IP); ok {
			out = append(out, ip.Unmap())
		}
	}
	return out
}

// refreshLocked listens on new network addresses and lets go of gone ones.
func (rm *remote) refreshLocked() {
	_, port, err := net.SplitHostPort(rm.p.Status().Addr)
	if err != nil {
		return
	}
	want := map[string]bool{}
	for _, ip := range networkAddresses() {
		want[net.JoinHostPort(ip.String(), port)] = true
	}
	for addr, ln := range rm.listeners {
		if !want[addr] {
			_ = ln.Close()
			delete(rm.listeners, addr)
		}
	}
	for addr := range rm.errs {
		if !want[addr] {
			delete(rm.errs, addr)
		}
	}
	for addr := range want {
		if _, ok := rm.listeners[addr]; ok {
			continue
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			rm.errs[addr] = err.Error()
			continue
		}
		delete(rm.errs, addr)
		al := &allowListener{Listener: ln, rm: rm}
		rm.listeners[addr] = al
		srv := rm.srv
		go func() { _ = srv.Serve(al) }()
	}
	rm.p.guard.setExtra(rm.listeningLocked())
}

func (rm *remote) listeningLocked() []string {
	out := make([]string, 0, len(rm.listeners))
	for addr := range rm.listeners {
		out = append(out, addr)
	}
	return out
}

func (rm *remote) status() RemoteStatus {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	s := RemoteStatus{Enabled: rm.enabled, Allow: append([]string{}, rm.allow...), FromEnv: rm.fromEnv, Addresses: []string{}, Listening: rm.listeningLocked(), Refused: rm.refused.Load(), LastRefused: rm.lastRefused, CertURL: "http://" + CertHost + "/"}
	if _, port, err := net.SplitHostPort(rm.p.Status().Addr); err == nil {
		s.Port, _ = strconv.Atoi(port)
	}
	for _, ip := range networkAddresses() {
		s.Addresses = append(s.Addresses, ip.String())
	}
	var errs []string
	for addr, e := range rm.errs {
		errs = append(errs, addr+": "+e)
	}
	if rm.enabled && len(rm.listeners) == 0 && len(errs) == 0 {
		errs = append(errs, "this computer has no network address other devices can reach")
	}
	s.Error = strings.Join(errs, "; ")
	return s
}

// allowListener closes a connection from outside the allow list before reading anything.
type allowListener struct {
	net.Listener
	rm *remote
}

func (l *allowListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ap, perr := netip.ParseAddrPort(c.RemoteAddr().String())
		if perr == nil && l.rm.allowed(c.RemoteAddr()) {
			rc := &remoteConn{Conn: c, rm: l.rm, ip: ap.Addr().Unmap()}
			l.rm.connMu.Lock()
			l.rm.conns[rc] = struct{}{}
			l.rm.connMu.Unlock()
			return rc, nil
		}
		_ = c.Close()
		l.rm.refused.Add(1)
		l.rm.mu.Lock()
		l.rm.lastRefused = c.RemoteAddr().String()
		l.rm.mu.Unlock()
	}
}

// remoteConn is a connection from another device, closed when remote capture stops.
type remoteConn struct {
	net.Conn
	rm   *remote
	ip   netip.Addr
	once sync.Once
}

func (c *remoteConn) Close() error {
	c.once.Do(func() {
		c.rm.connMu.Lock()
		delete(c.rm.conns, c)
		c.rm.connMu.Unlock()
	})
	return c.Conn.Close()
}

// CloseWrite half-closes the underlying connection when it can.
func (c *remoteConn) CloseWrite() error {
	if cw, ok := c.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return errors.New("no half-close")
}

// serveCert answers http://formnet.cert/ with the root certificate, while
// capture from other devices is on. The name never reaches the network.
func (p *Proxy) serveCert(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		http.Error(w, "FormNET serves its root certificate over plain HTTP only: open http://formnet.cert/", http.StatusForbidden)
		return
	}
	st := p.ext.Load()
	if st == nil || !st.remote.status().Enabled {
		http.Error(w, "Capture from other devices is off in FormNET, so formnet.cert does not answer. Turn it on in FormNET first.", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if p.opts.CA == nil {
		http.Error(w, "the root certificate is not ready", http.StatusServiceUnavailable)
		return
	}
	certPEM := p.opts.CA.PEM()
	var resp *http.Response
	switch r.URL.Path {
	case "", "/":
		page := `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>FormNET root certificate</title></head><body>` +
			`<h1>FormNET root certificate</h1><p>Install it on this device, then trust it, to let FormNET open this device's HTTPS.</p>` +
			`<ul><li><a href="/ca.pem">ca.pem</a> (PEM: iOS, macOS, Linux)</li><li><a href="/ca.cer">ca.cer</a> (DER: Android, Windows)</li></ul>` +
			`<p>Fingerprint (SHA-256): <code>` + html.EscapeString(p.opts.CA.Fingerprint()) + `</code></p></body></html>`
		resp = synthetic(http.StatusOK, "text/html; charset=utf-8", []byte(page))
	case "/ca.pem":
		resp = synthetic(http.StatusOK, "application/x-pem-file", certPEM)
		resp.Header.Set("Content-Disposition", `attachment; filename="formnet-ca.pem"`)
	case "/ca.cer":
		block, _ := pem.Decode(certPEM)
		if block == nil {
			http.Error(w, "the root certificate is not ready", http.StatusServiceUnavailable)
			return
		}
		resp = synthetic(http.StatusOK, "application/x-x509-ca-cert", block.Bytes)
		resp.Header.Set("Content-Disposition", `attachment; filename="formnet-ca.cer"`)
	default:
		resp = synthetic(http.StatusNotFound, "text/plain; charset=utf-8", []byte("FormNET answers / , /ca.pem and /ca.cer here.\n"))
	}
	o := origin{client: r.RemoteAddr, scheme: "http", host: CertHost, port: 80}
	f := p.newFlow(flow.KindHTTP, o, r.Method, "http://"+CertHost+r.URL.RequestURI())
	f.Notes = []string{"Answered by FormNET itself: formnet.cert never reaches the network."}
	f = p.opts.Store.Add(f)
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
	f, _ = p.relayResponse(w, resp, f, &timing{}, newCapture(http.NoBody, 0), requestLine, r.Header)
	p.finish(f)
}
