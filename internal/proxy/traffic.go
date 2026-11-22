package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/rules"
	"github.com/nodemode-dev/formnet/internal/throttle"
)

// TrafficOptions are the parts that change what passes: the rule engine,
// the throttle, breakpoints and capture from other devices.
type TrafficOptions struct {
	Rules    *rules.Engine
	Throttle *throttle.Shaper
	// BreakpointTimeout releases a forgotten pause; zero means DefaultBreakpointTimeout.
	BreakpointTimeout time.Duration
	// MaxPaused is how many exchanges may wait at once; zero means DefaultMaxPaused.
	MaxPaused int
	// RemoteFromEnv says FORMNET_REMOTE_CAPTURE or FORMNET_REMOTE_ALLOW chose the remote setting for this run.
	RemoteFromEnv bool
	// Remember* keep a change made through the API for the next start; nil keeps nothing.
	RememberThrottle    func(throttle.Config) error
	RememberRemote      func(enabled bool, allow []string) error
	RememberBreakpoints func(timeoutSeconds, maxPaused int) error
}

// trafficState is what UseTraffic set; the hooks read it without a lock.
type trafficState struct {
	opts   TrafficOptions
	rules  *rules.Engine
	shaper *throttle.Shaper
	bp     *breakpoints
	remote *remote
}

// UseTraffic gives the proxy its rules, throttle, breakpoints and remote
// capture. Call it before Serve.
func (p *Proxy) UseTraffic(o TrafficOptions) {
	if o.Throttle == nil {
		o.Throttle, _ = throttle.New(throttle.Defaults())
	}
	st := &trafficState{opts: o, rules: o.Rules, shaper: o.Throttle, bp: newBreakpoints(o.BreakpointTimeout, o.MaxPaused)}
	st.remote = newRemote(p, o.RemoteFromEnv)
	p.ext.Store(st)
}

// ErrTrafficUnavailable: the proxy was started without UseTraffic.
var ErrTrafficUnavailable = errors.New("rules, breakpoints, the throttle and remote capture are not available in this run")

// CertHost is the name the proxy answers itself with the root certificate.
const CertHost = "formnet.cert"

// preflight answers what the proxy handles before any routing: the
// certificate host, and a client on another device asking for this
// computer's loopback. It reports whether it answered.
func (p *Proxy) preflight(w http.ResponseWriter, r *http.Request) bool {
	host := r.URL.Hostname()
	if r.Method == http.MethodConnect {
		host, _, _ = net.SplitHostPort(r.Host)
		if host == "" {
			host = r.Host
		}
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == CertHost {
		p.serveCert(w, r)
		return true
	}
	if !loopbackClient(r.RemoteAddr) && loopbackTarget(host) {
		http.Error(w, "FormNET does not let another device reach this computer's loopback addresses", http.StatusForbidden)
		return true
	}
	return false
}

func loopbackClient(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip == nil || ip.IsLoopback()
}

func loopbackTarget(host string) bool {
	host = strings.Trim(host, "[]")
	if isLocalName(host) {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && isLocal(ip)
}

// ownTarget reports whether o is one of FormNET's own addresses.
func (p *Proxy) ownTarget(o origin) bool {
	port := strconv.Itoa(o.port)
	host := strings.Trim(o.host, "[]")
	if isLocalName(host) {
		return p.guard.refused(net.IPv4(127, 0, 0, 1), port) || p.guard.refused(net.IPv6loopback, port)
	}
	ip := net.ParseIP(host)
	return ip != nil && p.guard.refused(ip, port)
}

// serveHooked carries out the rules and the throttle for one exchange the
// proxy can read. It reports whether it answered; false leaves the exchange
// to the plain path.
func (p *Proxy) serveHooked(w http.ResponseWriter, r *http.Request, o origin) bool {
	st := p.ext.Load()
	if st == nil {
		return false
	}
	var plan *rules.Plan
	if st.rules != nil && !p.ownTarget(o) {
		if u, err := url.Parse(o.url(r)); err == nil {
			plan = st.rules.PlanFor(rules.RequestOf(r.Method, u))
		}
	}
	throttled := st.shaper.Applies(o.host)
	if plan == nil && !throttled {
		return false
	}
	if throttled {
		st.shaper.Latency(r.Context())
		r = r.WithContext(r.Context())
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = readCloser{Reader: st.shaper.Up(r.Body), Closer: r.Body}
		}
		w = &throttledWriter{ResponseWriter: w, out: st.shaper.DownWriter(w)}
	}
	if plan == nil {
		mark := &flowHook{mark: func(f flow.Flow) flow.Flow { f.Throttled = true; return f }}
		if err := p.exchange(w, r, o, mark); err != nil {
			panic(http.ErrAbortHandler)
		}
		return true
	}
	p.ruleExchange(w, r, o, plan, throttled, st)
	return true
}

// setExtra replaces the extra ip:port addresses the proxy never connects to.
func (g *destinationGuard) setExtra(addrs []string) {
	m := map[string]struct{}{}
	for _, a := range addrs {
		host, port, err := net.SplitHostPort(a)
		if err != nil {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			m[net.JoinHostPort(normalise(ip).String(), port)] = struct{}{}
		}
	}
	g.extra.Store(&m)
}

func (g *destinationGuard) extraRefused(ip net.IP, port string) bool {
	m := g.extra.Load()
	if m == nil {
		return false
	}
	_, ok := (*m)[net.JoinHostPort(normalise(ip).String(), port)]
	return ok
}

type readCloser struct {
	io.Reader
	io.Closer
}

// throttledWriter slows the body written to the client.
type throttledWriter struct {
	http.ResponseWriter
	out io.Writer
}

func (t *throttledWriter) Write(b []byte) (int, error) { return t.out.Write(b) }

// Flush passes a flush through.
func (t *throttledWriter) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// throttleTunnel slows a tunnel's upstream connection when the throttle
// covers its host, after the latency of a new connection.
func (p *Proxy) throttleTunnel(upstream net.Conn, o origin) (net.Conn, bool) {
	st := p.ext.Load()
	if st == nil || !st.shaper.Applies(o.host) {
		return upstream, false
	}
	st.shaper.Latency(p.baseContext())
	return st.shaper.Conn(upstream), true
}

// RulesStatus is what GET /api/rules answers.
type RulesStatus struct {
	Version   int               `json:"version"`
	Enabled   bool              `json:"enabled"`
	Rules     []rules.Rule      `json:"rules"`
	Intercept bool              `json:"intercept"`  // HTTPS is opened in this run, for hosts not on the passthrough list
	HTTPSNote string            `json:"https_note"` // the sentence the page shows about HTTPS
	Notes     map[string]string `json:"notes"`      // by rule id: why a rule cannot change HTTPS as written
}

// RulesSummary is the rules object of /api/status.
type RulesSummary struct {
	Enabled bool `json:"enabled"`
	Count   int  `json:"count"`
}

func (p *Proxy) rulesEngine() (*rules.Engine, error) {
	st := p.ext.Load()
	if st == nil || st.rules == nil {
		return nil, ErrTrafficUnavailable
	}
	return st.rules, nil
}

// Rules reports the rule list and where it can apply.
func (p *Proxy) Rules() RulesStatus {
	e, err := p.rulesEngine()
	if err != nil {
		return RulesStatus{Version: rules.Version, Rules: []rules.Rule{}, Notes: map[string]string{}, HTTPSNote: err.Error()}
	}
	return p.rulesStatus(e.Current())
}

// RulesSummary is the switch and the count.
func (p *Proxy) RulesSummary() RulesSummary {
	e, err := p.rulesEngine()
	if err != nil {
		return RulesSummary{}
	}
	s := e.Current()
	return RulesSummary{Enabled: s.Enabled, Count: len(s.Rules)}
}

func (p *Proxy) rulesStatus(s rules.Set) RulesStatus {
	intercept := p.Status().Intercept
	out := RulesStatus{Version: s.Version, Enabled: s.Enabled, Rules: s.Rules, Intercept: intercept, Notes: map[string]string{}}
	if intercept {
		out.HTTPSNote = "Rules change HTTPS only for hosts FormNET opens: interception is on, so every host but those on the passthrough list. Tunnelled HTTPS passes unchanged."
	} else {
		out.HTTPSNote = "HTTPS is not opened in this run (interception is off, or the root certificate is not trusted), so rules change plain HTTP only. Tunnelled HTTPS passes unchanged."
	}
	for _, r := range s.Rules {
		if strings.EqualFold(r.Match.Scheme, "http") {
			continue
		}
		m, err := rules.Compile(r.Match, "")
		if err != nil {
			continue
		}
		switch host := m.LiteralHost(); {
		case !intercept:
			out.Notes[r.ID] = "HTTPS is not opened in this run, so this rule changes plain HTTP only."
		case host != "" && !p.shouldIntercept(host):
			out.Notes[r.ID] = host + " is on the passthrough list: its HTTPS is tunnelled, so this rule changes plain HTTP to it only."
		}
	}
	return out
}

// ReplaceRules validates and keeps the whole list.
func (p *Proxy) ReplaceRules(list []rules.Rule) (RulesStatus, error) {
	e, err := p.rulesEngine()
	if err != nil {
		return RulesStatus{}, err
	}
	s, err := e.Replace(list)
	if err != nil {
		return RulesStatus{}, err
	}
	return p.rulesStatus(s), nil
}

// SetRulesEnabled turns every rule on or off.
func (p *Proxy) SetRulesEnabled(on bool) (RulesStatus, error) {
	e, err := p.rulesEngine()
	if err != nil {
		return RulesStatus{}, err
	}
	s, err := e.SetEnabled(on)
	if err != nil {
		return RulesStatus{}, err
	}
	return p.rulesStatus(s), nil
}

// Throttle reports the throttle setting.
func (p *Proxy) Throttle() throttle.Status {
	st := p.ext.Load()
	if st == nil {
		return throttle.Status{Config: throttle.Defaults(), Presets: throttle.Presets}
	}
	return st.shaper.Status()
}

// SetThrottle puts a throttle setting in force and keeps it.
func (p *Proxy) SetThrottle(c throttle.Config) (throttle.Status, error) {
	st := p.ext.Load()
	if st == nil {
		return throttle.Status{}, ErrTrafficUnavailable
	}
	c, err := st.shaper.Set(c)
	if err != nil {
		return throttle.Status{}, err
	}
	if st.opts.RememberThrottle != nil {
		if err := st.opts.RememberThrottle(c); err != nil {
			p.log.Warn("settings file", "err", err)
		}
	}
	return st.shaper.Status(), nil
}
