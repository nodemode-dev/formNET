// Package server is formnet's loopback API and the host of its web client.
//
// Routes (all under the configured loopback address):
//
//	GET  /healthz                  liveness: {status, product, version, proxy_addr, proxy_listening}; no token
//	GET  /api/status               proxy, CA, flow count, machine view and start notices in one body
//	POST /api/quit                 stop this run cleanly (a newer formnet replacing it asks this)
//	GET  /api/flows                flows without bodies; filters: host, domain, kind, client, q, since, pending, limit
//	GET  /api/flows/{id}           one flow with its captured bodies
//	POST /api/flows/{id}/resend    send an http flow's request again, as captured or edited: {method, url, headers, body_text | body_base64} (any absent field is as captured); answers {id} of the new flow
//	POST /api/flows/clear          forget every flow
//	GET  /api/hosts                per-host totals
//	GET  /api/machine              the latest machine snapshot
//	GET  /api/proxy                proxy status
//	POST /api/proxy                {"intercept": bool or "auto"|"on"|"off", "passthrough": [patterns]} (either field), kept in <data>/settings.json
//	GET  /api/sysproxy             whether the system proxy points at formnet: {supported, reason, enabled, active, address, services, auto_config, error}
//	POST /api/sysproxy             {"enabled": bool}: set or restore the system proxy now, kept in <data>/settings.json
//	GET  /api/recipes              lines that send curl, Node.js, Python, Java, Go and git through this run's proxy
//	GET  /api/ca                   CA info and the trust command
//	GET  /ca.pem                   the root certificate, for installing trust
//	GET  /api/export/har           HAR 1.2 of the matching flows (same filters as /api/flows); credential headers redacted unless secrets=1
//	GET  /api/stream               server-sent events: flow.added, flow.updated, flow.cleared, flow.message (at most 10 a second per flow), machine.snapshot, breakpoint.paused, breakpoint.resumed, ping
//	GET  /api/rules                the rule list {version, enabled, rules, intercept, https_note, notes}; rules change HTTPS only where it is opened
//	PUT  /api/rules                {"rules": [...]}: replace the whole list, validated (400 names the field), kept in <data>/rules.json
//	POST /api/rules/enabled        {"enabled": bool}: the switch that turns every rule off or on
//	GET  /api/breakpoints          {paused: [...], timeout_seconds, max_paused}
//	POST /api/breakpoints          {"timeout_seconds", "max_paused"} (either field), kept in <data>/settings.json
//	POST /api/breakpoints/{id}/continue  let a paused exchange go on, optionally edited: {method, url, headers, body_text | body_base64} at a request, {status, headers, body_text | body_base64} at a response
//	POST /api/breakpoints/{id}/abort     close the paused exchange's client connection
//	GET  /api/throttle             {enabled, preset, down_kbps, up_kbps, latency_ms, hosts, presets}
//	POST /api/throttle             any of those fields but presets, kept in <data>/settings.json
//	GET  /api/remote               capture from other devices: {enabled, allow, from_env, port, addresses, listening, refused, last_refused, cert_url, error}
//	POST /api/remote               {"enabled": bool, "allow": [CIDR or address]} (either field; on needs one allow entry), kept in <data>/settings.json; the proxy only, never this API
//	GET  /api/flows/{id}/messages  a WebSocket flow's messages: since=<seq>, limit; {flow_id, messages, next, count, bytes, dropped, open, evicted, ...}
//	GET  /api/upstream             the upstream proxy: {enabled, url, bypass, username, has_password, from_env, active, error}; never the password
//	POST /api/upstream             {"enabled", "url", "bypass", "username", "password"} (each optional; password is write only, "" removes it), kept in <data>/settings.json
//	     /api/terminal...          the terminal dock and the agent finder, when Deps.Terminals is set (internal/terminal)
//	POST /api/session/save         {"name"}: save every flow with its bodies and WebSocket messages as <data>/sessions/<name>.formnet (never replaces a file); {name, path, flows, bytes, saved}
//	GET  /api/session/export       the same file as a download (gzip); takes the /api/flows filters
//	GET  /api/sessions             saved sessions: {folder, sessions: [{name, path, saved, flows, bytes, app_version, error}]}
//	POST /api/session/open         {"name"} of a saved session, or a .formnet file (multipart "file" part, or the body itself): replaces the flows with the file's, marked from_session; {session, flows, dropped, messages}
//	POST /api/import/har           a HAR 1.2 file (multipart "file" part, or the body itself): adds its entries as flows marked from_session and imported "har"; {imported, skipped, dropped, messages, creator}
//	GET  /api/export/curl          a script with one curl command per matching http flow: the /api/flows filters, shell=sh|powershell|cmd, secrets=1
//	GET  /                         the web client
//
// The list, the HAR, session and cURL exports also take: status (404, 4xx or
// 200-299), method (GET,POST), type (response content type prefix), min_size
// and max_size (response body bytes), min_ms and max_ms (duration), error,
// rules, throttled and from_session (1 or 0), process (client process,
// substring), focus (host globs, comma separated), ignore (host globs whose
// flows are left out, applied after focus) and body (a substring of the
// captured text bodies, at most flow.DefaultBodyScan bytes read per
// request; the list answers body_scanned_bytes and body_scan_cut). A
// leading ! inverts status, method, type, process, domain, host or body
// (status=!2xx, method=!GET,HEAD for neither, body=!secret); a bare ! is a
// 400 naming the field. Every filter must match.
//
// Every request must carry a Host header for the loopback address (DNS
// rebinding guard). Cross-site requests from a browser are refused on every
// route but the page itself. The API, the stream and the certificate need
// the run's token: the page is opened once at /?token=<token> (printed by
// `formnet serve`, or passed by the desktop app), keeps it in sessionStorage
// and sends it as `Authorization: Bearer <token>`; the event stream and the
// downloads, which cannot set headers, pass it as a `token` query parameter.
// There is no cookie on purpose: a browser sends a cookie for 127.0.0.1 to
// every port, so a cookie would reach any other local server the person
// visits. Loopback is shared by every program and user on the computer, and
// the recordings hold other programs' secrets, so loopback alone is not enough.
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
	"github.com/nodemode-dev/formnet/internal/sysproxy"
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
	Notices []string // what the person should know about this start, such as a moved port
	Quit    func()   // stops the run; nil leaves /api/quit unanswered
	// Remember keeps a change made through POST /api/proxy for the next start; nil keeps nothing.
	Remember func(ProxySettings) error
	// SystemProxy sets and restores the system proxy; nil reports it as not available in this run.
	SystemProxy SystemProxyControl
	// BodyLimit bounds the body of a resend request; zero means 256 KiB.
	BodyLimit int64
	// Terminals adds the dock's terminal and agent finder routes under /api/terminal; nil adds none.
	Terminals interface{ Register(mux *http.ServeMux) }
	// Traffic adds rules, breakpoints, the throttle and remote capture (rules_routes.go); nil adds none.
	Traffic TrafficControl
	// Protocol adds WebSocket messages and the upstream proxy (protocol_routes.go); nil adds none.
	Protocol ProtocolControl
	// Data is the data folder; sessions are kept in <Data>/sessions (data_routes.go). Empty refuses saving and opening.
	Data string
}

// SystemProxyControl is what the API needs from the system proxy manager.
type SystemProxyControl interface {
	Status() sysproxy.Status
	Set(on bool) sysproxy.Status
}

// Resender is a proxy that can send a captured request again.
type Resender interface {
	Resend(orig flow.Flow, edit proxy.ResendEdit) (string, error)
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
	mux.HandleFunc("POST /api/flows/{id}/resend", s.resend)
	mux.HandleFunc("POST /api/flows/clear", s.clearFlows)
	mux.HandleFunc("GET /api/sysproxy", s.sysproxyStatus)
	mux.HandleFunc("POST /api/sysproxy", s.sysproxyUpdate)
	mux.HandleFunc("GET /api/recipes", s.recipes)
	mux.HandleFunc("GET /api/hosts", s.hosts)
	mux.HandleFunc("GET /api/machine", s.machineLatest)
	mux.HandleFunc("GET /api/proxy", s.proxyStatus)
	mux.HandleFunc("POST /api/proxy", s.proxyUpdate)
	mux.HandleFunc("GET /api/ca", s.caInfo)
	mux.HandleFunc("GET /ca.pem", s.caPEM)
	mux.HandleFunc("GET /api/export/har", s.exportHAR)
	mux.HandleFunc("GET /api/stream", s.stream)
	if d.Quit != nil {
		mux.HandleFunc("POST /api/quit", s.quit)
	}
	if d.Terminals != nil {
		d.Terminals.Register(mux)
	}
	s.registerRules(mux)
	s.registerProtocol(mux)
	s.registerData(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "no such route") })
	mux.Handle("/", s.client())
	return s.guard(mux)
}

type handler struct {
	Deps
	data *dataState // the opened session (data_routes.go)
}

// guard refuses requests whose Host is not this server, cross-site requests
// from a browser (on every route but the page itself), writes that do not
// come from the page, and API calls without a session.
func (s *handler) guard(next http.Handler) http.Handler {
	port := portOf(s.Addr)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, port) {
			writeError(w, http.StatusForbidden, "FormNET answers only on its loopback address")
			return
		}
		if crossSite(r) && !(r.URL.Path == "/" && r.Method == http.MethodGet) {
			writeError(w, http.StatusForbidden, "cross-site requests are refused")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r, port) {
			writeError(w, http.StatusForbidden, "writes are accepted from the FormNET page only")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if needsSession(r.URL.Path) && !s.hasSession(r) {
			writeError(w, http.StatusUnauthorized, "no session: open FormNET from the address printed by formnet serve, or run formnet open")
			return
		}
		if r.URL.Query().Has("token") {
			// Never let a token-bearing URL leak through the referrer.
			w.Header().Set("Referrer-Policy", "no-referrer")
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

// hasSession accepts the token as a bearer header or, for the stream and
// the downloads, as a query parameter.
func (s *handler) hasSession(r *http.Request) bool {
	if bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return s.tokenMatches(strings.TrimSpace(bearer))
	}
	return s.tokenMatches(r.URL.Query().Get("token"))
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
	Status         string `json:"status"`
	Product        string `json:"product"`
	Version        string `json:"version"`
	ProxyAddr      string `json:"proxy_addr"`
	ProxyListening bool   `json:"proxy_listening"`
}

func (s *handler) health(w http.ResponseWriter, r *http.Request) {
	px := s.Proxy.Status()
	writeJSON(w, http.StatusOK, healthBody{Status: "ok", Product: app.Product, Version: app.Version, ProxyAddr: px.Addr, ProxyListening: px.Running})
}

func (s *handler) quit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]any{"stopping": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	s.Log.Info("asked to stop by another formnet")
	go s.Quit()
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
	Notices []string      `json:"notices"`
	Now     time.Time     `json:"now"`

	SysProxy sysproxy.Status `json:"sysproxy"`

	*TrafficStatus // rules, throttle, remote; absent when Deps.Traffic is nil

	Upstream *proxy.UpstreamStatus `json:"upstream,omitempty"` // absent when Deps.Protocol is nil

	Session SessionStatus `json:"session"` // an opened session file (data_routes.go)
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
		Notices: append([]string{}, s.Notices...),
		Now:     time.Now(),

		SysProxy: s.systemProxy(),

		TrafficStatus: s.trafficStatus(),

		Upstream: s.upstreamStatus(),

		Session: s.sessionStatus(),
	})
}

func (s *handler) systemProxy() sysproxy.Status {
	if s.SystemProxy == nil {
		return sysproxy.Status{Reason: "the system proxy is not available in this run", Services: []sysproxy.Service{}}
	}
	return s.SystemProxy.Status()
}

func (s *handler) sysproxyStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.systemProxy())
}

func (s *handler) sysproxyUpdate(w http.ResponseWriter, r *http.Request) {
	var u struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&u); err != nil || u.Enabled == nil {
		writeError(w, http.StatusBadRequest, `body must be JSON: {"enabled": true} or {"enabled": false}`)
		return
	}
	if s.SystemProxy == nil {
		st := s.systemProxy()
		st.Error = st.Reason
		writeJSON(w, http.StatusOK, st)
		return
	}
	writeJSON(w, http.StatusOK, s.SystemProxy.Set(*u.Enabled))
}

type resendBody struct {
	Method     *string        `json:"method"`
	URL        *string        `json:"url"`
	Headers    *[]flow.Header `json:"headers"`
	BodyText   *string        `json:"body_text"`
	BodyBase64 *string        `json:"body_base64"`
}

func (s *handler) resend(w http.ResponseWriter, r *http.Request) {
	rs, ok := s.Proxy.(Resender)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this proxy cannot send requests again")
		return
	}
	orig, ok := s.Store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such flow")
		return
	}
	limit := s.BodyLimit
	if limit <= 0 {
		limit = 256 << 10
	}
	ceiling := limit*2 + 64<<10
	raw, err := io.ReadAll(io.LimitReader(r.Body, ceiling+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the request body could not be read")
		return
	}
	if int64(len(raw)) > ceiling {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a resend request may be at most %d bytes", ceiling))
		return
	}
	var in resendBody
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			writeError(w, http.StatusBadRequest, "body must be JSON with method, url, headers, body_text or body_base64 (each optional)")
			return
		}
	}
	if in.BodyText != nil && in.BodyBase64 != nil {
		writeError(w, http.StatusBadRequest, "give body_text or body_base64, not both")
		return
	}
	edit := proxy.ResendEdit{Method: in.Method, URL: in.URL, Headers: in.Headers}
	if in.BodyText != nil {
		b := []byte(*in.BodyText)
		edit.Body = &b
	}
	if in.BodyBase64 != nil {
		b, err := base64.StdEncoding.DecodeString(*in.BodyBase64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "body_base64 is not base64")
			return
		}
		edit.Body = &b
	}
	if edit.Body != nil && int64(len(*edit.Body)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("an edited body may be at most %d bytes, the body limit of this run", limit))
		return
	}
	id, err := rs.Resend(orig, edit)
	var refused *proxy.RefusedError
	var input *proxy.ResendInputError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"id": id})
	case errors.Is(err, proxy.ErrBodyTruncated), errors.Is(err, proxy.ErrNotResendable):
		writeError(w, http.StatusConflict, err.Error())
	case errors.As(err, &refused):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.As(err, &input):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
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
	flows, scan, err := s.search(r, q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": flows, "total": s.Store.Len(), "body_scanned_bytes": scan.BodyScanned, "body_scan_cut": scan.BodyCut})
}

// body is how a captured body travels to the client.
type body struct {
	Text      string `json:"text,omitempty"`
	Base64    string `json:"base64,omitempty"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Decoded   bool   `json:"decoded"`  // the text is the decoded form of Encoding
	Encoding  string `json:"encoding"` // the Content-Encoding as sent
}

func encodeBody(m *flow.Message) *body {
	if m == nil || len(m.Body) == 0 {
		return nil
	}
	b := &body{Bytes: len(m.Body), Truncated: m.BodyTruncated, Decoded: m.Decoded, Encoding: m.Encoding}
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
	s.sessionClosed()
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
	Intercept   json.RawMessage `json:"intercept"`
	Passthrough []string        `json:"passthrough"`
}

// ProxySettings is a change made through POST /api/proxy; a nil field did not change.
type ProxySettings struct {
	Intercept   *string  // auto, on or off
	Passthrough []string // nil: unchanged
}

// modeSetter is a proxy that also takes the auto mode.
type modeSetter interface {
	SetInterceptMode(mode string) error
}

// interceptMode reads intercept as a boolean (true is on) or as "auto", "on" or "off".
func interceptMode(raw json.RawMessage) (string, error) {
	var on bool
	if err := json.Unmarshal(raw, &on); err == nil {
		if on {
			return proxy.ModeOn, nil
		}
		return proxy.ModeOff, nil
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err == nil {
		switch mode = strings.ToLower(strings.TrimSpace(mode)); mode {
		case proxy.ModeAuto, proxy.ModeOn, proxy.ModeOff:
			return mode, nil
		}
	}
	return "", errors.New(`intercept must be true, false, "auto", "on" or "off"`)
}

func (s *handler) proxyUpdate(w http.ResponseWriter, r *http.Request) {
	var u proxyUpdate
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&u); err != nil {
		writeError(w, http.StatusBadRequest, "body must be JSON with intercept and/or passthrough")
		return
	}
	var change ProxySettings
	if len(u.Intercept) > 0 && string(u.Intercept) != "null" {
		mode, err := interceptMode(u.Intercept)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		change.Intercept = &mode
	}
	if change.Intercept != nil {
		if ms, ok := s.Proxy.(modeSetter); ok {
			if err := ms.SetInterceptMode(*change.Intercept); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		} else if *change.Intercept == proxy.ModeAuto {
			writeError(w, http.StatusBadRequest, "this proxy takes intercept on or off only")
			return
		} else {
			s.Proxy.SetIntercept(*change.Intercept == proxy.ModeOn)
		}
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
		change.Passthrough = clean
	}
	if s.Remember != nil && (change.Intercept != nil || change.Passthrough != nil) {
		if err := s.Remember(change); err != nil {
			s.Log.Warn("settings file", "err", err)
		}
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
	listed, _, err := s.search(r, q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := "formnet-" + time.Now().UTC().Format("20060102-150405") + ".har"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	hw, err := flow.NewHARWriterWith(w, app.Version, flow.HAROptions{Secrets: r.URL.Query().Get("secrets") == "1"})
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
	pauses := s.breakpointEvents(ctx)
	messages := s.messageEvents(ctx)
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
		case e := <-pauses:
			if !send(e.Type, e) {
				return
			}
		case e := <-messages:
			if !send("flow.message", e) {
				return
			}
		case <-ticker.C:
			if !send("ping", map[string]any{"now": time.Now()}) {
				return
			}
		}
	}
}

// The desktop window shows this page from 127.0.0.1, a remote origin to Tauri, whose commands travel over its own
// ipc: scheme (http://ipc.localhost on Windows); a browser never reaches those, so allowing them costs nothing there.
// The page itself also gets ws:// of its own Host (checked by the guard), for the terminal dock's socket, since
// older WebKit does not count ws: as 'self'.
const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self' ipc: http://ipc.localhost; frame-ancestors 'none'"

// client serves the embedded web client. The page and its files are static
// and need no session; the page reads ?token= itself and keeps it in sessionStorage.
// A wrong token on the page is refused so a typo is noticed at once. Paths
// without a file fall back to index.html so the page can own its routes.
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
		if token := r.URL.Query().Get("token"); token != "" && !s.tokenMatches(token) {
			s.noSession(w, http.StatusForbidden)
			return
		}
		index, err := s.Dist.Open("index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "the web client is not built; run make build")
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", strings.Replace(csp, "connect-src 'self'", "connect-src 'self' ws://"+r.Host, 1))
		_, _ = io.Copy(w, index)
	})
}

const noSessionPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>FormNET</title>
<meta name="color-scheme" content="dark light"><style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;line-height:1.5}code{font-family:ui-monospace,monospace}</style></head>
<body><h1>FormNET</h1><p>That token is not this run's. FormNET answers only to the page it opened itself, so that no other program on this computer can read the recordings.</p>
<p>Open FormNET from the address printed by <code>formnet serve</code> (it carries this run's token), or run <code>formnet open</code>.</p></body></html>`

func (s *handler) noSession(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, noSessionPage)
}
