// Package server is formnet's loopback API and the host of its web client.
//
// Routes (all under the configured loopback address):
//
//	GET  /healthz                  liveness: {status, product, version}
//	GET  /api/status               proxy, CA, flow count and machine view in one body
//	GET  /api/flows                flows without bodies; filters: host, domain, kind, client, q, since, pending, limit
//	GET  /api/flows/{id}           one flow with its captured bodies
//	POST /api/flows/clear          forget every flow
//	GET  /api/hosts                per-host totals
//	GET  /api/machine              the latest machine snapshot
//	GET  /api/proxy                proxy status
//	POST /api/proxy                {"intercept": bool, "passthrough": [patterns]} (either field)
//	GET  /api/ca                   CA info and the trust command
//	GET  /ca.pem                   the root certificate, for installing trust
//	GET  /api/export/har           HAR 1.2 of the matching flows (same filters as /api/flows)
//	GET  /api/stream               server-sent events: flow.added, flow.updated, flow.cleared, machine.snapshot, ping
//	GET  /                         the web client
//
// Every request must carry a Host header for the loopback address (DNS
// rebinding guard). Cross-site requests from a browser are refused on every
// route but the page itself. The API, the stream and the certificate need a
// session: the page is opened once at /?token=<the run's token> (printed by
// `formnet serve`, or passed by the desktop app), the server answers with an
// HttpOnly, SameSite=Strict cookie and redirects to /, and from then on the
// cookie (or an Authorization: Bearer header, for scripts) is the session.
// Loopback is shared by every program and user on the computer, and the
// recordings hold other programs' secrets, so loopback alone is not enough.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/app"
	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/machine"
	"github.com/nodemode-dev/formnet/internal/proxy"
)

// ProxyControl is what the API needs from the proxy.
type ProxyControl interface {
	Status() proxy.Status
	SetIntercept(on bool)
	SetPassthrough(patterns []string)
}

// CASource is what the API needs from the certificate authority.
type CASource interface {
	Info() ca.Info
	PEM() []byte
}

// MachineSource is what the API needs from the machine monitor.
type MachineSource interface {
	Latest() machine.Snapshot
	Subscribe(ctx context.Context) <-chan machine.Snapshot
}

// SessionCookie is the cookie that carries the session.
const SessionCookie = "formnet_session"

// Deps are the parts the handler serves.
type Deps struct {
	Addr    string // the loopback address the server listens on, for the Host check
	Token   string // the run's token; every API call needs it as a cookie or bearer
	Store   *flow.Store
	Proxy   ProxyControl
	CA      CASource
	Machine MachineSource
	Dist    fs.FS
	Log     *slog.Logger
}

// Handler builds the API and client handler. It panics without a token,
// because serving the API open would hand every recording to any local program.
func Handler(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if len(d.Token) < 16 {
		panic("server: a token of at least 16 characters is required")
	}
	s := &handler{Deps: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/flows", s.listFlows)
	mux.HandleFunc("GET /api/flows/{id}", s.getFlow)
	mux.HandleFunc("POST /api/flows/clear", s.clearFlows)
	mux.HandleFunc("GET /api/hosts", s.hosts)
	mux.HandleFunc("GET /api/machine", s.machineLatest)
	mux.HandleFunc("GET /api/proxy", s.proxyStatus)
	mux.HandleFunc("POST /api/proxy", s.proxyUpdate)
	mux.HandleFunc("GET /api/ca", s.caInfo)
	mux.HandleFunc("GET /ca.pem", s.caPEM)
	mux.HandleFunc("GET /api/export/har", s.exportHAR)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "no such route") })
	mux.Handle("/", s.client())
	return s.guard(mux)
}

type handler struct {
	Deps
}

// guard refuses requests whose Host is not this server, cross-site requests
// from a browser (on every route but the page itself), writes that do not
// come from the page, and API calls without a session.
func (s *handler) guard(next http.Handler) http.Handler {
	port := portOf(s.Addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, port) {
			writeError(w, http.StatusForbidden, "formnet answers only on its loopback address")
			return
		}
		if crossSite(r) && !(r.URL.Path == "/" && r.Method == http.MethodGet) {
			writeError(w, http.StatusForbidden, "cross-site requests are refused")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r, port) {
			writeError(w, http.StatusForbidden, "writes are accepted from the formnet page only")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if needsSession(r.URL.Path) && !s.hasSession(r) {
			writeError(w, http.StatusUnauthorized, "no session: open formnet from the address printed by formnet serve, or run formnet open")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return true
	}
	return false
}

func needsSession(path string) bool {
	return strings.HasPrefix(path, "/api/") || path == "/ca.pem"
}

func (s *handler) tokenMatches(candidate string) bool {
	return candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(s.Token)) == 1
}

// hasSession accepts the session cookie or a bearer token.
func (s *handler) hasSession(r *http.Request) bool {
	if c, err := r.Cookie(SessionCookie); err == nil && s.tokenMatches(c.Value) {
		return true
	}
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return s.tokenMatches(strings.TrimSpace(bearer))
	}
	return false
}

func (s *handler) setSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: s.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}

func hostAllowed(host, port string) bool {
	h, p, err := net.SplitHostPort(host)
	if err != nil {
		h, p = host, ""
	}
	if p != port && !(p == "" && (port == "80" || port == "")) {
		return false
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(r *http.Request, port string) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "cross-site", "same-site":
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // not a browser
	}
	rest, ok := strings.CutPrefix(origin, "http://")
	return ok && hostAllowed(rest, port)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type healthBody struct {
	Status  string `json:"status"`
	Product string `json:"product"`
	Version string `json:"version"`
}

func (s *handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, healthBody{Status: "ok", Product: app.Product, Version: app.Version})
}

type statusBody struct {
	Product string        `json:"product"`
	Version string        `json:"version"`
	Addr    string        `json:"addr"`
	Epoch   string        `json:"epoch"`
	Flows   int           `json:"flows"`
	Pending int           `json:"pending"`
	Hosts   int           `json:"hosts"`
	Store   flow.Stats    `json:"store"`
	Proxy   proxy.Status  `json:"proxy"`
	CA      ca.Info       `json:"ca"`
	Machine machineStatus `json:"machine"`
	Now     time.Time     `json:"now"`
}

type machineStatus struct {
	Supported   bool      `json:"supported"`
	Source      string    `json:"source"`
	Taken       time.Time `json:"taken"`
	Processes   int       `json:"processes"`
	Connections int       `json:"connections"`
	Error       string    `json:"error,omitempty"`
}

func (s *handler) status(w http.ResponseWriter, r *http.Request) {
	snap := s.Machine.Latest()
	st := s.Store.Stats()
	writeJSON(w, http.StatusOK, statusBody{
		Product: app.Product,
		Version: app.Version,
		Addr:    s.Addr,
		Epoch:   s.Store.Epoch(),
		Flows:   st.Flows,
		Pending: st.Pending,
		Hosts:   st.Hosts,
		Store:   st,
		Proxy:   s.Proxy.Status(),
		CA:      s.CA.Info(),
		Machine: machineStatus{Supported: snap.Supported, Source: snap.Source, Taken: snap.Taken, Processes: len(snap.Processes), Connections: len(snap.Connections), Error: snap.Error},
		Now:     time.Now(),
	})
}

func queryFrom(r *http.Request) (flow.Query, error) {
	v := r.URL.Query()
	q := flow.Query{Host: v.Get("host"), Domain: v.Get("domain"), Kind: v.Get("kind"), Client: v.Get("client"), Text: v.Get("q")}
	if since := v.Get("since"); since != "" {
		n, err := strconv.ParseUint(since, 10, 64)
		if err != nil {
			return q, fmt.Errorf("since: %w", err)
		}
		q.SinceSeq = n
	}
	if limit := v.Get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 0 {
			return q, errors.New("limit must be a non-negative number")
		}
		q.Limit = n
	}
	if p := v.Get("pending"); p != "" {
		b, err := strconv.ParseBool(p)
		if err != nil {
			return q, errors.New("pending must be true or false")
		}
		q.Pending = &b
	}
	return q, nil
}

func (s *handler) listFlows(w http.ResponseWriter, r *http.Request) {
	q, err := queryFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	flows := s.Store.List(q)
	writeJSON(w, http.StatusOK, map[string]any{"flows": flows, "total": s.Store.Len()})
}

// body is how a captured body travels to the client.
type body struct {
	Text      string `json:"text,omitempty"`
	Base64    string `json:"base64,omitempty"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
}

func encodeBody(m *flow.Message) *body {
	if m == nil || len(m.Body) == 0 {
		return nil
	}
	b := &body{Bytes: len(m.Body), Truncated: m.BodyTruncated}
	if utf8.Valid(m.Body) && !hasControl(m.Body) {
		b.Text = string(m.Body)
	} else {
		b.Base64 = base64.StdEncoding.EncodeToString(m.Body)
	}
	return b
}

func hasControl(b []byte) bool {
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return true
		}
	}
	return false
}

type flowDetail struct {
	flow.Flow
	RequestBody  *body `json:"request_body,omitempty"`
	ResponseBody *body `json:"response_body,omitempty"`
}

func (s *handler) getFlow(w http.ResponseWriter, r *http.Request) {
	f, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such flow")
		return
	}
	d := flowDetail{Flow: f, RequestBody: encodeBody(&f.Request), ResponseBody: encodeBody(f.Response)}
	writeJSON(w, http.StatusOK, d)
}

func (s *handler) clearFlows(w http.ResponseWriter, r *http.Request) {
	s.Store.Clear()
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}

func (s *handler) hosts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hosts": s.Store.Hosts()})
}

func (s *handler) machineLatest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Machine.Latest())
}

func (s *handler) proxyStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Proxy.Status())
}

type proxyUpdate struct {
	Intercept   *bool    `json:"intercept"`
	Passthrough []string `json:"passthrough"`
}

func (s *handler) proxyUpdate(w http.ResponseWriter, r *http.Request) {
	var u proxyUpdate
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&u); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON with intercept and/or passthrough")
		return
	}
	if u.Intercept != nil {
		s.Proxy.SetIntercept(*u.Intercept)
	}
	if u.Passthrough != nil {
		clean := make([]string, 0, len(u.Passthrough))
		for _, p := range u.Passthrough {
			p = strings.ToLower(strings.TrimSpace(p))
			if p == "" {
				continue
			}
			if strings.ContainsAny(p, " /\\") {
				writeError(w, http.StatusBadRequest, "passthrough entries are host names or *.suffix patterns")
				return
			}
			clean = append(clean, p)
		}
		s.Proxy.SetPassthrough(clean)
	}
	writeJSON(w, http.StatusOK, s.Proxy.Status())
}

func (s *handler) caInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.CA.Info())
}

func (s *handler) caPEM(w http.ResponseWriter, r *http.Request) {
	pem := s.CA.PEM()
	if len(pem) == 0 {
		writeError(w, http.StatusServiceUnavailable, "the certificate authority is not ready")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="formnet-ca.pem"`)
	_, _ = w.Write(pem)
}

func (s *handler) exportHAR(w http.ResponseWriter, r *http.Request) {
	q, err := queryFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The list strips bodies; the export wants them, so each flow is re-read
	// and written on its own, and the document never sits in memory whole.
	listed := s.Store.List(q)
	name := "formnet-" + time.Now().UTC().Format("20060102-150405") + ".har"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	hw, err := flow.NewHARWriter(w, app.Version)
	if err != nil {
		s.Log.Warn("har export", "err", err)
		return
	}
	for _, f := range listed {
		if g, ok := s.Store.Get(f.ID); ok {
			if err := hw.Write(g); err != nil {
				return
			}
		}
	}
	_ = hw.Close()
}

func (s *handler) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	ctx := r.Context()
	flows, dropped := s.Store.SubscribeCounted(ctx)
	snaps := s.Machine.Subscribe(ctx)
	send := func(event string, v any) bool {
		raw, err := json.Marshal(v)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	send("hello", map[string]any{"version": app.Version, "flows": s.Store.Len(), "epoch": s.Store.Epoch(), "dropped": 0})
	var told uint64
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-flows:
			if !send(string(e.Type), e) {
				return
			}
			// Events this subscriber lost are reported once per gap, so the
			// client reloads the list instead of trusting what it has.
			if d := dropped(); d > told {
				told = d
				if !send("resync", map[string]any{"dropped": d}) {
					return
				}
			}
		case snap, ok := <-snaps:
			if !ok {
				snaps = nil
				continue
			}
			if !send("machine.snapshot", snap) {
				return
			}
		case <-ticker.C:
			if !send("ping", map[string]any{"now": time.Now()}) {
				return
			}
		}
	}
}

const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'"

// client serves the embedded web client. Static files need no session; the
// page itself exchanges ?token= for the session cookie and otherwise needs it.
// Paths without a file fall back to index.html so the page can own its routes.
func (s *handler) client() http.Handler {
	files := http.FS(s.Dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean("/" + r.URL.Path)
		if name != "/" && name != "/index.html" {
			if f, err := s.Dist.Open(strings.TrimPrefix(name, "/")); err == nil {
				info, statErr := f.Stat()
				_ = f.Close()
				if statErr == nil && !info.IsDir() {
					if strings.HasPrefix(name, "/assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					w.Header().Set("Content-Security-Policy", csp)
					http.FileServer(files).ServeHTTP(w, r)
					return
				}
			}
		}
		if token := r.URL.Query().Get("token"); token != "" {
			if !s.tokenMatches(token) {
				s.noSession(w, http.StatusForbidden)
				return
			}
			s.setSession(w)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if !s.hasSession(r) {
			s.noSession(w, http.StatusUnauthorized)
			return
		}
		index, err := s.Dist.Open("index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "the web client is not built; run make build")
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", csp)
		_, _ = io.Copy(w, index)
	})
}

const noSessionPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>formnet</title>
<meta name="color-scheme" content="dark light"><style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;line-height:1.5}code{font-family:ui-monospace,monospace}</style></head>
<body><h1>formnet</h1><p>This page has no session. formnet answers only to the page it opened itself, so that no other program on this computer can read the recordings.</p>
<p>Open formnet from the address printed by <code>formnet serve</code> (it carries this run's token), or run <code>formnet open</code>.</p></body></html>`

func (s *handler) noSession(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, noSessionPage)
}
