package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/rules"
)

// wireState holds what this milestone adds to a Proxy: the upstream proxy
// and the WebSocket message logs (wsframes.go). Its zero value is ready.
type wireState struct {
	up upstreamState
	ws wsRegistry
}

// UpstreamSetting is the upstream proxy as a person chose it.
type UpstreamSetting struct {
	Enabled  bool
	URL      string // scheme://host:port without credentials: http, https (TLS to the proxy) or socks5
	Bypass   []string
	Username string
	Password string
}

// UpstreamOptions start the upstream proxy of a run.
type UpstreamOptions struct {
	Saved UpstreamSetting // what <data>/settings.json says
	// Env is what FORMNET_UPSTREAM_PROXY chose for this run; it wins over Saved and is never written.
	Env *UpstreamSetting
	// Remember keeps a change made through the API; nil keeps nothing.
	Remember func(UpstreamSetting) error
}

// UpstreamChange is a change made through POST /api/upstream; a nil field stays as it is.
type UpstreamChange struct {
	Enabled  *bool
	URL      *string
	Bypass   *[]string
	Username *string
	Password *string // "" removes the password
}

// UpstreamStatus is what GET /api/upstream answers. The password is never in it.
type UpstreamStatus struct {
	Enabled     bool       `json:"enabled"`
	URL         string     `json:"url"`
	Bypass      []string   `json:"bypass"`
	Username    string     `json:"username"`
	HasPassword bool       `json:"has_password"`
	FromEnv     bool       `json:"from_env"` // FORMNET_UPSTREAM_PROXY chose the setting at start
	Active      bool       `json:"active"`   // enabled with a usable address: outgoing connections use it
	Error       string     `json:"error,omitempty"`
	ErrorAt     *time.Time `json:"error_at,omitempty"`
}

type upstreamState struct {
	mu       sync.Mutex
	saved    UpstreamSetting
	cur      UpstreamSetting
	fromEnv  bool
	remember func(UpstreamSetting) error
	active   atomic.Pointer[upstreamActive]

	errMu sync.Mutex
	err   string
	errAt time.Time
}

// upstreamActive is a parsed, enabled upstream, read on every connection without a lock.
type upstreamActive struct {
	scheme   string // http, https or socks5
	host     string
	hostPort string
	label    string   // scheme://host:port, the name errors and flows carry
	withUser *url.URL // for the transport, with the credentials
	user     string
	pass     string
	hosts    []string
	nets     []netip.Prefix
}

const maxBypass = 256

// UseUpstream sets the upstream proxy of the run. A setting that does not
// validate leaves the upstream off and is returned as the error.
func (p *Proxy) UseUpstream(o UpstreamOptions) error {
	u := &p.wire.up
	u.mu.Lock()
	defer u.mu.Unlock()
	u.remember = o.Remember
	u.saved = o.Saved
	u.cur = o.Saved
	if o.Env != nil {
		u.cur, u.fromEnv = *o.Env, true
	}
	a, err := p.parseUpstream(u.cur)
	if err != nil {
		u.cur.Enabled = false
		u.active.Store(nil)
		return err
	}
	u.active.Store(a)
	return nil
}

// Upstream reports the upstream proxy setting.
func (p *Proxy) Upstream() UpstreamStatus {
	u := &p.wire.up
	u.mu.Lock()
	cur, fromEnv := u.cur, u.fromEnv
	u.mu.Unlock()
	st := UpstreamStatus{Enabled: cur.Enabled, URL: cur.URL, Bypass: append([]string{}, cur.Bypass...), Username: cur.Username, HasPassword: cur.Password != "", FromEnv: fromEnv, Active: u.active.Load() != nil}
	u.errMu.Lock()
	if u.err != "" {
		at := u.errAt
		st.Error, st.ErrorAt = u.err, &at
	}
	u.errMu.Unlock()
	return st
}

// SetUpstream applies a change for this run and keeps it. While
// FORMNET_UPSTREAM_PROXY chose the run's setting, the change is applied on
// top of it, and the file keeps the change on top of what it said.
func (p *Proxy) SetUpstream(c UpstreamChange) (UpstreamStatus, error) {
	u := &p.wire.up
	u.mu.Lock()
	next, err := applyUpstream(u.cur, c)
	if err != nil {
		u.mu.Unlock()
		return UpstreamStatus{}, err
	}
	a, err := p.parseUpstream(next)
	if err != nil {
		u.mu.Unlock()
		return UpstreamStatus{}, err
	}
	saved, err := applyUpstream(u.saved, c)
	if err != nil {
		saved = next
	}
	u.cur, u.saved = next, saved
	u.active.Store(a)
	remember := u.remember
	u.mu.Unlock()
	u.errMu.Lock()
	u.err = ""
	u.errMu.Unlock()
	p.transport.CloseIdleConnections()
	if remember != nil {
		if err := remember(saved); err != nil {
			p.log.Warn("settings file", "err", err)
		}
	}
	return p.Upstream(), nil
}

// UpstreamFromURL reads an upstream given as one URL (FORMNET_UPSTREAM_PROXY),
// with its credentials in the URL; empty means off. bypass is kept as given.
// UseUpstream checks the rest.
func UpstreamFromURL(raw string, bypass []string) (UpstreamSetting, error) {
	on := raw != ""
	return applyUpstream(UpstreamSetting{Bypass: bypass}, UpstreamChange{URL: &raw, Enabled: &on})
}

// applyUpstream returns s with c applied and cleaned. Credentials written in
// the URL move into Username and Password, so the URL never carries them.
func applyUpstream(s UpstreamSetting, c UpstreamChange) (UpstreamSetting, error) {
	s.Bypass = append([]string{}, s.Bypass...)
	if c.URL != nil {
		raw := strings.TrimSpace(*c.URL)
		if raw == "" {
			s.URL = ""
		} else {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" {
				return s, &rules.FieldError{Field: "url", Reason: "must look like http://host:port, https://host:port or socks5://host:port"}
			}
			if u.User != nil {
				s.Username = u.User.Username()
				if pw, ok := u.User.Password(); ok {
					s.Password = pw
				}
				u.User = nil
			}
			s.URL = u.String()
		}
	}
	if c.Username != nil {
		s.Username = strings.TrimSpace(*c.Username)
	}
	if c.Password != nil {
		s.Password = *c.Password
	}
	if c.Bypass != nil {
		clean, err := cleanBypass(*c.Bypass)
		if err != nil {
			return s, err
		}
		s.Bypass = clean
	}
	if c.Enabled != nil {
		s.Enabled = *c.Enabled
	}
	return s, nil
}

// cleanBypass keeps host names, *.suffix patterns (a leading dot means the
// same), IP addresses and CIDR ranges, lower case.
func cleanBypass(list []string) ([]string, error) {
	if len(list) > maxBypass {
		return nil, &rules.FieldError{Field: "bypass", Reason: fmt.Sprintf("at most %d entries", maxBypass)}
	}
	out := make([]string, 0, len(list))
	for i, e := range list {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		if strings.HasPrefix(e, ".") {
			e = "*" + e
		}
		if _, err := netip.ParsePrefix(e); err == nil {
			out = append(out, e)
			continue
		}
		if strings.ContainsAny(e, " /\\@:") && net.ParseIP(strings.Trim(e, "[]")) == nil {
			return nil, &rules.FieldError{Field: fmt.Sprintf("bypass[%d]", i), Reason: "must be a host name, *.suffix, an address or a CIDR range"}
		}
		out = append(out, strings.Trim(e, "[]"))
	}
	return out, nil
}

// parseUpstream checks a setting and, when it is on, returns what the dialers use.
func (p *Proxy) parseUpstream(s UpstreamSetting) (*upstreamActive, error) {
	if _, err := cleanBypass(s.Bypass); err != nil {
		return nil, err
	}
	if s.URL == "" {
		if s.Enabled {
			return nil, &rules.FieldError{Field: "url", Reason: "is required to turn the upstream proxy on"}
		}
		return nil, nil
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" {
		return nil, &rules.FieldError{Field: "url", Reason: "must look like http://host:port, https://host:port or socks5://host:port"}
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "socks5h" {
		scheme = "socks5"
	}
	switch scheme {
	case "http", "https", "socks5":
	default:
		return nil, &rules.FieldError{Field: "url", Reason: "the scheme must be http, https or socks5"}
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, &rules.FieldError{Field: "url", Reason: "must be scheme://host:port with nothing after the port"}
	}
	port := u.Port()
	if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
		return nil, &rules.FieldError{Field: "url", Reason: "must name the proxy's port, as in http://proxy.example:3128"}
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil && p.guard.refused(ip, port) {
		return nil, &rules.FieldError{Field: "url", Reason: "that is FormNET's own address; the upstream must be another proxy"}
	}
	if isLocalName(host) && (p.guard.refused(net.IPv4(127, 0, 0, 1), port) || p.guard.refused(net.IPv6loopback, port)) {
		return nil, &rules.FieldError{Field: "url", Reason: "that is FormNET's own address; the upstream must be another proxy"}
	}
	if !s.Enabled {
		return nil, nil
	}
	hostPort := net.JoinHostPort(host, port)
	a := &upstreamActive{scheme: scheme, host: host, hostPort: hostPort, label: scheme + "://" + hostPort, user: s.Username, pass: s.Password}
	a.withUser = &url.URL{Scheme: scheme, Host: hostPort}
	if s.Username != "" || s.Password != "" {
		a.withUser.User = url.UserPassword(s.Username, s.Password)
	}
	for _, e := range s.Bypass {
		if pfx, err := netip.ParsePrefix(e); err == nil {
			a.nets = append(a.nets, pfx.Masked())
			continue
		}
		a.hosts = append(a.hosts, strings.ToLower(e))
	}
	return a, nil
}

// applies reports whether a connection to host goes through the upstream.
// Loopback addresses and localhost names never do.
func (a *upstreamActive) applies(host string) bool {
	host = strings.Trim(strings.TrimSuffix(strings.ToLower(host), "."), "[]")
	if host == "" || isLocalName(host) {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return false
		}
		for _, n := range a.nets {
			if n.Contains(ip.Unmap()) {
				return false
			}
		}
	}
	for _, pattern := range a.hosts {
		if flow.HostMatches(host, pattern) {
			return false
		}
	}
	return true
}

func (p *Proxy) upstreamFor(host string) *upstreamActive {
	a := p.wire.up.active.Load()
	if a == nil || !a.applies(host) {
		return nil
	}
	return a
}

// upstreamLabel names the upstream a connection to host would use, or "".
func (p *Proxy) upstreamLabel(host string) string {
	if a := p.upstreamFor(host); a != nil {
		return a.label
	}
	return ""
}

// upstreamURL is the transport's Proxy function. The destination is checked
// here because the transport then dials the upstream, not the destination.
func (p *Proxy) upstreamURL(req *http.Request) (*url.URL, error) {
	a := p.upstreamFor(req.URL.Hostname())
	if a == nil {
		return nil, nil
	}
	hostPort := net.JoinHostPort(req.URL.Hostname(), strconv.Itoa(portOr(req.URL.Port(), req.URL.Scheme)))
	if err := p.checkDestination(req.Context(), hostPort); err != nil {
		return nil, err
	}
	return a.withUser, nil
}

// checkDestination applies the destination guard. When the upstream proxy
// carries the connection a name this computer cannot look up is left to the
// upstream; a name that resolves to loopback or to FormNET is still refused.
func (p *Proxy) checkDestination(ctx context.Context, hostPort string) error {
	_, err := p.guard.resolve(ctx, hostPort)
	if err == nil {
		return nil
	}
	var refused *RefusedError
	if errors.As(err, &refused) {
		return err
	}
	if host, _, splitErr := net.SplitHostPort(hostPort); splitErr == nil && p.upstreamFor(host) != nil {
		return nil
	}
	return err
}

// dialTransport is the transport's dialer: the upstream proxy's address is
// dialled as the upstream (errors name it), everything else through the guard.
func (p *Proxy) dialTransport(ctx context.Context, network, addr string) (net.Conn, error) {
	if a := p.wire.up.active.Load(); a != nil && addr == a.hostPort {
		c, err := p.dialProxyConn(ctx, a, false)
		if err != nil {
			return nil, p.upstreamFailed(err)
		}
		p.upstreamOK()
		return c, nil
	}
	return p.guard.DialContext(ctx, network, addr)
}

// upstreamConnectResponse names the upstream when it refuses a tunnel the transport asked for.
func (p *Proxy) upstreamConnectResponse(ctx context.Context, proxyURL *url.URL, req *http.Request, res *http.Response) error {
	if res.StatusCode == http.StatusOK {
		p.upstreamOK()
		return nil
	}
	label := proxyURL.Scheme + "://" + proxyURL.Host
	return p.upstreamFailed(fmt.Errorf("the upstream proxy %s answered %q to CONNECT %s", label, res.Status, req.Host))
}

// dialOut connects to hostPort for a tunnel or a WebSocket: through the
// upstream proxy when it applies, otherwise through the guard.
func (p *Proxy) dialOut(ctx context.Context, hostPort string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(hostPort)
	if err != nil {
		return nil, &RefusedError{Target: hostPort, Reason: "not a host:port"}
	}
	a := p.upstreamFor(host)
	if a == nil {
		return p.guard.DialContext(ctx, "tcp", hostPort)
	}
	if err := p.checkDestination(ctx, hostPort); err != nil {
		return nil, err
	}
	c, err := p.connectThrough(ctx, a, hostPort)
	if err != nil {
		return nil, p.upstreamFailed(err)
	}
	p.upstreamOK()
	return c, nil
}

// dialProxyConn connects to the upstream proxy itself. A loopback upstream
// (a local proxy agent) is allowed; FormNET's own addresses are not.
func (p *Proxy) dialProxyConn(ctx context.Context, a *upstreamActive, withTLS bool) (net.Conn, error) {
	_, port, _ := net.SplitHostPort(a.hostPort)
	var ips []net.IP
	switch ip := net.ParseIP(a.host); {
	case ip != nil:
		ips = []net.IP{ip}
	case isLocalName(a.host):
		ips = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	default:
		addrs, err := p.guard.resolver.LookupIPAddr(ctx, a.host)
		if err != nil {
			return nil, fmt.Errorf("the upstream proxy %s is not reachable: %w", a.label, err)
		}
		for _, x := range addrs {
			ips = append(ips, x.IP)
		}
	}
	var conn net.Conn
	last := errors.New("no addresses")
	for _, ip := range ips {
		if p.guard.refused(ip, port) {
			return nil, &RefusedError{Target: a.label, Reason: "the upstream proxy is FormNET's own address"}
		}
		c, err := p.guard.dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
		if err == nil {
			conn = c
			break
		}
		last = err
	}
	if conn == nil {
		return nil, fmt.Errorf("the upstream proxy %s is not reachable: %w", a.label, last)
	}
	if !withTLS {
		return conn, nil
	}
	tc := tls.Client(conn, &tls.Config{ServerName: a.host, RootCAs: p.opts.UpstreamRootCAs, MinVersion: tls.VersionTLS12})
	_ = tc.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := tc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("the upstream proxy %s refused TLS: %w", a.label, err)
	}
	_ = tc.SetDeadline(time.Time{})
	return tc, nil
}

// connectThrough opens a tunnel to hostPort through the upstream: CONNECT for
// an http or https proxy, the SOCKS5 CONNECT command for socks5.
func (p *Proxy) connectThrough(ctx context.Context, a *upstreamActive, hostPort string) (net.Conn, error) {
	c, err := p.dialProxyConn(ctx, a, a.scheme == "https")
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(handshakeTimeout))
	stop := context.AfterFunc(ctx, func() { _ = c.SetDeadline(time.Now()) })
	defer stop()
	if a.scheme == "socks5" {
		if err := socks5Connect(c, hostPort, a.user, a.pass); err != nil {
			c.Close()
			return nil, fmt.Errorf("the upstream proxy %s: %w", a.label, err)
		}
		_ = c.SetDeadline(time.Time{})
		return c, nil
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: hostPort}, Host: hostPort, Header: http.Header{}}
	if a.user != "" || a.pass != "" {
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.user+":"+a.pass)))
	}
	if err := req.Write(c); err != nil {
		c.Close()
		return nil, fmt.Errorf("the upstream proxy %s closed the connection: %w", a.label, err)
	}
	br := bufio.NewReader(&io.LimitedReader{R: c, N: handshakeLimit})
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("the upstream proxy %s did not answer the CONNECT: %w", a.label, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		c.Close()
		return nil, fmt.Errorf("the upstream proxy %s answered %q to CONNECT %s", a.label, resp.Status, hostPort)
	}
	_ = c.SetDeadline(time.Time{})
	if n := br.Buffered(); n > 0 {
		early, _ := br.Peek(n)
		return &readerConn{Conn: c, r: io.MultiReader(strings.NewReader(string(early)), c)}, nil
	}
	return c, nil
}

func (p *Proxy) upstreamFailed(err error) error {
	u := &p.wire.up
	u.errMu.Lock()
	u.err, u.errAt = err.Error(), time.Now()
	u.errMu.Unlock()
	return err
}

func (p *Proxy) upstreamOK() {
	u := &p.wire.up
	u.errMu.Lock()
	u.err = ""
	u.errMu.Unlock()
}

// socks5Connect asks a SOCKS5 server (RFC 1928) for a connection to
// hostPort, by name so the server resolves it, with username and password
// authentication (RFC 1929) when credentials are set.
func socks5Connect(c net.Conn, hostPort, user, pass string) error {
	host, portText, err := net.SplitHostPort(hostPort)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return errors.New("bad port " + portText)
	}
	methods := []byte{0x00}
	if user != "" || pass != "" {
		if len(user) > 255 || len(pass) > 255 {
			return errors.New("the username and password must be at most 255 bytes each")
		}
		methods = []byte{0x02}
	}
	if _, err := c.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		return fmt.Errorf("no SOCKS5 greeting: %w", err)
	}
	if reply[0] != 0x05 {
		return errors.New("it is not a SOCKS5 server")
	}
	switch reply[1] {
	case 0x00:
	case 0x02:
		msg := append([]byte{0x01, byte(len(user))}, user...)
		msg = append(append(msg, byte(len(pass))), pass...)
		if _, err := c.Write(msg); err != nil {
			return err
		}
		if _, err := io.ReadFull(c, reply[:]); err != nil {
			return fmt.Errorf("no answer to the credentials: %w", err)
		}
		if reply[1] != 0x00 {
			return errors.New("it refused the username and password")
		}
	case 0xff:
		return errors.New("it accepts none of the offered authentication methods (FormNET offers none, or username and password)")
	default:
		return fmt.Errorf("it chose an authentication method FormNET does not offer (%d)", reply[1])
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(append(req, 0x01), v4...)
		} else {
			req = append(append(req, 0x04), ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return errors.New("the host name is too long for SOCKS5")
		}
		req = append(append(req, 0x03, byte(len(host))), host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		return err
	}
	var head [4]byte
	if _, err := io.ReadFull(c, head[:]); err != nil {
		return fmt.Errorf("no answer to CONNECT %s: %w", hostPort, err)
	}
	if head[1] != 0x00 {
		return fmt.Errorf("it refused CONNECT %s: %s", hostPort, socksReply(head[1]))
	}
	var skip int
	switch head[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(c, l[:]); err != nil {
			return err
		}
		skip = int(l[0])
	default:
		return errors.New("it answered with an unknown address type")
	}
	_, err = io.ReadFull(c, make([]byte, skip+2))
	return err
}

func socksReply(code byte) string {
	switch code {
	case 0x01:
		return "general failure"
	case 0x02:
		return "not allowed by its rules"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	}
	return "reply " + strconv.Itoa(int(code))
}
