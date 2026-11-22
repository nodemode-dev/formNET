package mcp

import (
	"context"
	"encoding/base64"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/machine"
	"github.com/nodemode-dev/formnet/internal/proxy"
)

const (
	defaultFlows     = 25
	maxFlows         = 200
	defaultBodyShown = 8 << 10
	maxBodyShown     = 64 << 10
	defaultRows      = 50
	maxRows          = 500
)

func filterProps() []prop {
	sinceMin, _ := intRange(0, 0)
	return []prop{
		{Name: "host", Type: "string", MaxLength: 253, Description: "Only flows to this host: an exact name such as api.example.com, or a *.suffix pattern such as *.example.com."},
		{Name: "domain", Type: "string", MaxLength: 253, Description: "Only flows whose host belongs to this registrable domain, such as example.com (it covers its subdomains)."},
		{Name: "kind", Type: "string", Enum: []string{flow.KindHTTP, flow.KindWebSocket, flow.KindTunnel, flow.KindRefused}, Description: "Only flows of this kind: http (opened, with headers and bodies), websocket, tunnel (HTTPS passed through and only counted) or refused (the program refused FormNET's certificate, usually because it pins certificates)."},
		{Name: "client", Type: "string", MaxLength: 64, Description: "Only flows from a client address starting with this ip:port prefix, as list_flows shows it in client."},
		{Name: "q", Type: "string", MaxLength: 200, Description: "Case-insensitive text found in the URL, method, status code, process name or response content type."},
		{Name: "since", Type: "integer", Min: sinceMin, Description: "Only flows with a seq greater than this. Pass the highest seq of the last answer to read only what arrived after it."},
		{Name: "pending", Type: "boolean", Description: "true for exchanges still in progress, false for finished ones; leave out for both."},
	}
}

func flowQuery(a args, limit int64) url.Values {
	v := url.Values{}
	for _, k := range []string{"host", "domain", "kind", "client", "q"} {
		if s := a.str(k); s != "" {
			v.Set(k, s)
		}
	}
	if n, ok := a.num("since"); ok {
		v.Set("since", strconv.FormatInt(n, 10))
	}
	if b, ok := a.flag("pending"); ok {
		v.Set("pending", strconv.FormatBool(b))
	}
	if limit > 0 {
		v.Set("limit", strconv.FormatInt(limit, 10))
	}
	return v
}

// flowRow is a flow as list_flows returns it: no headers, no bodies.
type flowRow struct {
	ID            string  `json:"id"`
	Seq           uint64  `json:"seq"`
	Kind          string  `json:"kind"`
	Start         string  `json:"start"`
	Pending       bool    `json:"pending"`
	Method        string  `json:"method"`
	URL           string  `json:"url"`
	Host          string  `json:"host"`
	Status        int     `json:"status"`
	Client        string  `json:"client"`
	ClientPID     int     `json:"client_pid"`
	ClientProcess string  `json:"client_process"`
	ContentType   string  `json:"content_type"`
	BytesSent     int64   `json:"bytes_sent"`
	BytesReceived int64   `json:"bytes_received"`
	DurationMS    float64 `json:"duration_ms"`
	Intercepted   bool    `json:"intercepted"`
	Error         string  `json:"error"`
}

func row(f flow.Flow) flowRow {
	r := flowRow{ID: f.ID, Seq: f.Seq, Kind: f.Kind, Start: f.Start.UTC().Format(time.RFC3339Nano), Pending: f.Pending, Method: f.Method, URL: f.URL, Host: f.Host, Status: f.Status,
		Client: f.Client, ClientPID: f.ClientPID, ClientProcess: f.ClientProcess, BytesSent: f.BytesSent, BytesReceived: f.BytesReceived, DurationMS: f.DurationMS, Error: f.Error}
	if f.Response != nil {
		r.ContentType = f.Response.ContentType
	}
	r.Intercepted = f.Kind == flow.KindHTTP || f.Kind == flow.KindWebSocket
	return r
}

var flowRowSchema = obj(map[string]any{
	"id":             typ("string", "The flow id; pass it to get_flow."),
	"seq":            typ("integer", "Arrival order; pass the highest one as since to read newer flows."),
	"kind":           typ("string", "http, websocket, tunnel or refused."),
	"start":          typ("string", "When the exchange began, RFC 3339 UTC."),
	"pending":        typ("boolean", "True while the exchange is still in progress."),
	"method":         typ("string", "HTTP method, or CONNECT for a tunnel."),
	"url":            typ("string", "The full URL, or host:port for a tunnel."),
	"host":           typ("string", "The host without its port."),
	"status":         typ("integer", "HTTP status, 0 when there is none."),
	"client":         typ("string", "ip:port of the program that used the proxy."),
	"client_pid":     typ("integer", "Process id behind the client port, 0 when unknown."),
	"client_process": typ("string", "Process name behind the client port, empty when unknown."),
	"content_type":   typ("string", "Response content type."),
	"bytes_sent":     typ("integer", "Bytes from the program towards the server."),
	"bytes_received": typ("integer", "Bytes from the server towards the program."),
	"duration_ms":    typ("number", "Milliseconds from start to end."),
	"intercepted":    typ("boolean", "True when FormNET saw inside the exchange; false for HTTPS that was only counted."),
	"error":          typ("string", "Transport error or refusal reason, empty when none."),
}, "id", "seq", "kind", "start", "pending", "method", "url", "host", "status", "client", "client_pid", "client_process", "content_type", "bytes_sent", "bytes_received", "duration_ms", "intercepted", "error")

func listFlows(ctx context.Context, s *Server, a args) (any, error) {
	limit, ok := a.num("limit")
	if !ok {
		limit = defaultFlows
	}
	var body struct {
		Flows []flow.Flow `json:"flows"`
		Total int         `json:"total"`
	}
	if _, err := s.call(ctx, "GET", "/api/flows", flowQuery(a, limit), nil, &body); err != nil {
		return nil, err
	}
	rows := make([]flowRow, 0, len(body.Flows))
	var last uint64
	for _, f := range body.Flows {
		rows = append(rows, row(f))
		last = max(last, f.Seq)
	}
	return struct {
		Flows    []flowRow `json:"flows"`
		Returned int       `json:"returned"`
		Recorded int       `json:"recorded"`
		LastSeq  uint64    `json:"last_seq"`
	}{rows, len(rows), body.Total, last}, nil
}

// secretHeaders are the headers whose values never leave this server.
var secretHeaders = []string{"authorization", "proxy-authorization", "cookie", "set-cookie"}

func redact(h []flow.Header) []flow.Header {
	out := make([]flow.Header, len(h))
	for i, x := range h {
		out[i] = x
		if slices.Contains(secretHeaders, strings.ToLower(x.Name)) {
			out[i].Value = "[redacted]"
		}
	}
	return out
}

type wireBody struct {
	Text      string `json:"text"`
	Base64    string `json:"base64"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Decoded   bool   `json:"decoded"`
	Encoding  string `json:"encoding"`
}

type shownBody struct {
	Text             string `json:"text,omitempty"`
	Base64           string `json:"base64,omitempty"`
	Shown            int    `json:"shown_bytes"`
	Captured         int    `json:"captured_bytes"`
	OnWire           int64  `json:"wire_bytes"`
	Truncated        bool   `json:"truncated"`
	CaptureTruncated bool   `json:"capture_truncated"`
	Decoded          bool   `json:"decoded"`
	Encoding         string `json:"encoding"`
}

func show(b *wireBody, wire int64, limit int) *shownBody {
	if b == nil {
		return nil
	}
	out := &shownBody{Captured: b.Bytes, OnWire: wire, CaptureTruncated: b.Truncated, Decoded: b.Decoded, Encoding: b.Encoding}
	if b.Text != "" {
		t := b.Text
		if len(t) > limit {
			t = t[:limit]
			for len(t) > 0 && !utf8.ValidString(t) {
				t = t[:len(t)-1]
			}
		}
		out.Text, out.Shown = t, len(t)
	} else if raw, err := base64.StdEncoding.DecodeString(b.Base64); err == nil {
		if len(raw) > limit {
			raw = raw[:limit]
		}
		out.Base64, out.Shown = base64.StdEncoding.EncodeToString(raw), len(raw)
	}
	out.Truncated = out.Shown < b.Bytes || b.Truncated
	return out
}

var bodySchema = map[string]any{
	"type":        []string{"object", "null"},
	"description": "A captured body, null when there was none. truncated is true when shown_bytes is less than the full body: either this tool cut it (raise max_body_bytes) or FormNET kept only a prefix (capture_truncated).",
	"properties": map[string]any{
		"text":              typ("string", "The body as text, when it is valid UTF-8."),
		"base64":            typ("string", "The body as base64, when it is binary."),
		"shown_bytes":       typ("integer", "Bytes shown here."),
		"captured_bytes":    typ("integer", "Bytes FormNET kept."),
		"wire_bytes":        typ("integer", "Bytes that crossed the wire."),
		"truncated":         typ("boolean", "True when less than the whole body is shown."),
		"capture_truncated": typ("boolean", "True when FormNET kept only a prefix (FORMNET_BODY_LIMIT)."),
		"decoded":           typ("boolean", "True when the text is the decoded form of the Content-Encoding."),
		"encoding":          typ("string", "The Content-Encoding as sent."),
	},
	"required": []string{"shown_bytes", "captured_bytes", "wire_bytes", "truncated", "capture_truncated", "decoded", "encoding"},
}

var headerList = arr(obj(map[string]any{"name": typ("string", "Header name."), "value": typ("string", "Header value; credential headers read [redacted].")}, "name", "value"))

func getFlow(ctx context.Context, s *Server, a args) (any, error) {
	limit := int64(defaultBodyShown)
	if n, ok := a.num("max_body_bytes"); ok {
		limit = n
	}
	bodies := true
	if b, ok := a.flag("include_bodies"); ok {
		bodies = b
	}
	var d struct {
		flow.Flow
		RequestBody  *wireBody `json:"request_body"`
		ResponseBody *wireBody `json:"response_body"`
	}
	if _, err := s.call(ctx, "GET", "/api/flows/"+url.PathEscape(a.str("id")), nil, nil, &d); err != nil {
		if te, ok := err.(*toolError); ok && te.msg == "no such flow" {
			return nil, failf("no flow %s: it may have been cleared or dropped as the oldest; call list_flows for current ids", a.str("id"))
		}
		return nil, err
	}
	type side struct {
		Headers []flow.Header `json:"headers"`
		Body    *shownBody    `json:"body"`
	}
	out := struct {
		flowRow
		Scheme         string        `json:"scheme"`
		Protocol       string        `json:"protocol"`
		ClientProtocol string        `json:"client_protocol"`
		TLS            *flow.TLSInfo `json:"tls"`
		ConnectMS      float64       `json:"connect_ms"`
		TTFBMS         float64       `json:"ttfb_ms"`
		Request        side          `json:"request"`
		Response       *side         `json:"response"`
	}{flowRow: row(d.Flow), Scheme: d.Scheme, Protocol: d.Protocol, ClientProtocol: d.ClientProtocol, TLS: d.TLS, ConnectMS: d.ConnectMS, TTFBMS: d.TTFBMS}
	out.Request = side{Headers: redact(d.Request.Headers)}
	if out.Request.Headers == nil {
		out.Request.Headers = []flow.Header{}
	}
	if bodies {
		out.Request.Body = show(d.RequestBody, d.Request.BodyBytes, int(limit))
	}
	if d.Response != nil {
		r := side{Headers: redact(d.Response.Headers)}
		if bodies {
			r.Body = show(d.ResponseBody, d.Response.BodyBytes, int(limit))
		}
		out.Response = &r
	}
	return out, nil
}

func listHosts(ctx context.Context, s *Server, a args) (any, error) {
	limit, ok := a.num("limit")
	if !ok {
		limit = defaultRows
	}
	var body struct {
		Hosts []flow.HostSummary `json:"hosts"`
	}
	if _, err := s.call(ctx, "GET", "/api/hosts", nil, nil, &body); err != nil {
		return nil, err
	}
	q := strings.ToLower(a.str("q"))
	out := []flow.HostSummary{}
	matched := 0
	for _, h := range body.Hosts {
		if q != "" && !strings.Contains(strings.ToLower(h.Host+" "+strings.Join(h.Processes, " ")), q) {
			continue
		}
		matched++
		if int64(len(out)) < limit {
			if h.Processes == nil {
				h.Processes = []string{}
			}
			out = append(out, h)
		}
	}
	return struct {
		Hosts     []flow.HostSummary `json:"hosts"`
		Matched   int                `json:"matched"`
		Truncated bool               `json:"truncated"`
	}{out, matched, matched > len(out)}, nil
}

func machineSnapshot(ctx context.Context, s *Server, a args) (any, error) {
	limit, ok := a.num("limit")
	if !ok {
		limit = defaultRows
	}
	var snap machine.Snapshot
	if _, err := s.call(ctx, "GET", "/api/machine", nil, nil, &snap); err != nil {
		return nil, err
	}
	name := strings.ToLower(a.str("process"))
	pid, byPID := a.num("pid")
	state := strings.ToUpper(a.str("state"))
	procs := []machine.Process{}
	conns := []machine.Connection{}
	matchedP, matchedC := 0, 0
	for _, p := range snap.Processes {
		if (name != "" && !strings.Contains(strings.ToLower(p.Name), name)) || (byPID && int64(p.PID) != pid) {
			continue
		}
		matchedP++
		if int64(len(procs)) < limit {
			procs = append(procs, p)
		}
	}
	for _, c := range snap.Connections {
		if (name != "" && !strings.Contains(strings.ToLower(c.Process), name)) || (byPID && int64(c.PID) != pid) || (state != "" && c.State != state) {
			continue
		}
		matchedC++
		if int64(len(conns)) < limit {
			conns = append(conns, c)
		}
	}
	return struct {
		Taken                string               `json:"taken"`
		Source               string               `json:"source"`
		Supported            bool                 `json:"supported"`
		Error                string               `json:"error"`
		Processes            []machine.Process    `json:"processes"`
		Connections          []machine.Connection `json:"connections"`
		MatchedProcesses     int                  `json:"matched_processes"`
		MatchedConnections   int                  `json:"matched_connections"`
		Truncated            bool                 `json:"truncated"`
		TotalProcesses       int                  `json:"total_processes"`
		TotalConnectionsSeen int                  `json:"total_connections"`
	}{snap.Taken.UTC().Format(time.RFC3339Nano), snap.Source, snap.Supported, snap.Error, procs, conns, matchedP, matchedC, matchedP > len(procs) || matchedC > len(conns), len(snap.Processes), len(snap.Connections)}, nil
}

type proxyView struct {
	proxy.Status
	Listening bool              `json:"listening"`
	Version   string            `json:"formnet_version"`
	CAPath    string            `json:"ca_path"`
	Env       map[string]string `json:"env"`
	Hint      string            `json:"hint"`
}

func proxyRead(ctx context.Context, s *Server) (proxyView, error) {
	var st proxy.Status
	r, err := s.call(ctx, "GET", "/api/proxy", nil, nil, &st)
	if err != nil {
		return proxyView{}, err
	}
	addr := st.Addr
	if addr == "" {
		addr = r.ProxyAddr
	}
	if st.Passthrough == nil {
		st.Passthrough = []string{}
	}
	st.Addr = addr
	v := proxyView{Status: st, Listening: st.Running, Version: r.Version, CAPath: r.CAPath}
	v.Env = map[string]string{"HTTPS_PROXY": "http://" + addr, "HTTP_PROXY": "http://" + addr, "NODE_USE_ENV_PROXY": "1", "NODE_EXTRA_CA_CERTS": r.CAPath}
	v.Hint = "Set HTTPS_PROXY and HTTP_PROXY for the program. Node 22.21 or 24 and later also need NODE_USE_ENV_PROXY=1, and NODE_EXTRA_CA_CERTS to trust the root, both read only at process start. Go's ProxyFromEnvironment never proxies localhost or loopback addresses, so a Go program's requests to 127.0.0.1 are not recorded."
	if !st.Intercept {
		v.Hint += " Interception is off: HTTPS is tunnelled and only counted (kind tunnel) until the root is trusted or the mode is on."
	}
	return v, nil
}

func proxyStatus(ctx context.Context, s *Server, _ args) (any, error) { return proxyRead(ctx, s) }

var proxySchema = obj(map[string]any{
	"addr":            typ("string", "The proxy's ip:port."),
	"running":         typ("boolean", "True while the proxy accepts connections."),
	"listening":       typ("boolean", "Same as running."),
	"intercept":       typ("boolean", "True when HTTPS is opened now; false when it is tunnelled and only counted."),
	"intercept_mode":  typ("string", "auto (open HTTPS while the root is trusted), on or off."),
	"passthrough":     arr(typ("string", "A host or *.suffix pattern tunnelled without interception.")),
	"error":           typ("string", "Why the proxy is not running, when it is not."),
	"formnet_version": typ("string", "The running FormNET's version."),
	"ca_path":         typ("string", "The root certificate (PEM) a program must trust."),
	"env":             map[string]any{"type": "object", "description": "Environment variables that point a program at this proxy.", "additionalProperties": map[string]any{"type": "string"}},
	"hint":            typ("string", "How to use the variables, and what they do not cover."),
}, "addr", "running", "intercept", "intercept_mode", "passthrough", "env", "hint")

func caInfo(ctx context.Context, s *Server, _ args) (any, error) {
	var info ca.Info
	if _, err := s.call(ctx, "GET", "/api/ca", nil, nil, &info); err != nil {
		return nil, err
	}
	return struct {
		ca.Info
		Note string `json:"note"`
	}{info, "trust_command and untrust_command are for the person to run in a terminal; they change this computer's trust store. This server never runs them. The private key never leaves the data folder."}, nil
}

func readTools() []*Tool {
	flowLimitLo, flowLimitHi := intRange(1, maxFlows)
	bodyLo, bodyHi := intRange(0, maxBodyShown)
	rowsLo, rowsHi := intRange(1, maxRows)
	pidLo, pidHi := intRange(0, 1<<31-1)
	return []*Tool{
		{
			Name:  "list_flows",
			Title: "List recorded flows",
			Description: "Read-only. Lists the exchanges FormNET's proxy recorded, newest last, without headers or bodies: method, URL, status, kind, sizes, timing and the process that sent it. " +
				"Use it to see what a program sent, to find a flow id for get_flow, or to poll for new traffic by passing the last_seq of the previous answer as since. " +
				"Filters are those of the FormNET API (host, domain, kind, client, q, since, pending); limit defaults to 25 and returns the newest matches. Kind refused lists programs that rejected FormNET's certificate.",
			Class:  ReadOnly,
			Props:  append(filterProps(), prop{Name: "limit", Type: "integer", Min: flowLimitLo, Max: flowLimitHi, Default: defaultFlows, Description: "How many of the newest matching flows to return, 1 to 200."}),
			Output: obj(map[string]any{"flows": arr(flowRowSchema), "returned": typ("integer", "Flows in this answer."), "recorded": typ("integer", "Flows FormNET holds in all."), "last_seq": typ("integer", "Highest seq in this answer, 0 when empty; pass it as since next time.")}, "flows", "returned", "recorded", "last_seq"),
			Run:    listFlows,
		},
		{
			Name:  "get_flow",
			Title: "Get one flow",
			Description: "Read-only. Returns one recorded exchange by id: request and response headers, timing, TLS details and the captured bodies. " +
				"Use it after list_flows to read what a request actually carried or what came back. Bodies are cut to max_body_bytes (default 8192, at most 65536) and flagged truncated when cut, as text when UTF-8 and base64 otherwise. " +
				"Authorization, Proxy-Authorization, Cookie and Set-Cookie values always read [redacted].",
			Class: ReadOnly,
			Props: []prop{
				{Name: "id", Type: "string", Pattern: "^[0-9a-f]{1,64}$", Description: "The flow id from list_flows."},
				{Name: "include_bodies", Type: "boolean", Default: true, Description: "false to leave both bodies out and read only headers and timing."},
				{Name: "max_body_bytes", Type: "integer", Min: bodyLo, Max: bodyHi, Default: defaultBodyShown, Description: "Bytes of each body to show, 0 to 65536."},
			},
			Required: []string{"id"},
			Output: obj(mergeProps(flowRowSchema, map[string]any{
				"scheme":          typ("string", "http or https."),
				"protocol":        typ("string", "What the server answered with, such as HTTP/2.0."),
				"client_protocol": typ("string", "What the program sent to the proxy."),
				"tls":             map[string]any{"type": []string{"object", "null"}, "description": "TLS with the server, null for plain HTTP."},
				"connect_ms":      typ("number", "Dial plus TLS to the server, in milliseconds."),
				"ttfb_ms":         typ("number", "Time to the first response byte, in milliseconds."),
				"request":         obj(map[string]any{"headers": headerList, "body": bodySchema}, "headers"),
				"response":        map[string]any{"type": []string{"object", "null"}, "description": "Null until a response arrived.", "properties": map[string]any{"headers": headerList, "body": bodySchema}},
			}), "id", "kind", "url", "request"),
			Run: getFlow,
		},
		{
			Name:  "list_hosts",
			Title: "List hosts",
			Description: "Read-only. Lists every host the proxy has seen with totals, most traffic first: flows, errors, bytes each way, first and last seen, whether anything was opened, the processes that reached it and FormNET's reading-aid label. " +
				"Use it for an overview of where traffic goes before narrowing with list_flows. The label (category, owner) is a hint about the host, not a verdict.",
			Class: ReadOnly,
			Props: []prop{
				{Name: "q", Type: "string", MaxLength: 200, Description: "Case-insensitive text in the host name or a process name."},
				{Name: "limit", Type: "integer", Min: rowsLo, Max: rowsHi, Default: defaultRows, Description: "Hosts to return, 1 to 500."},
			},
			Output: obj(map[string]any{"hosts": arr(map[string]any{"type": "object"}), "matched": typ("integer", "Hosts that matched q."), "truncated": typ("boolean", "True when more hosts matched than were returned.")}, "hosts", "matched", "truncated"),
			Run:    listHosts,
		},
		{
			Name:  "machine_snapshot",
			Title: "Machine connections",
			Description: "Read-only. Returns the latest table of every connection this computer holds and the process that owns it, from the running FormNET (the same table as `formnet snapshot`). " +
				"Use it to see which programs talk to the network even when they ignore the proxy. Filter by process name, pid or state (ESTABLISHED, LISTEN, ...); limit bounds both lists (default 50) and truncated says when more matched.",
			Class: ReadOnly,
			Props: []prop{
				{Name: "process", Type: "string", MaxLength: 200, Description: "Case-insensitive part of the process name."},
				{Name: "pid", Type: "integer", Min: pidLo, Max: pidHi, Description: "Only this process id."},
				{Name: "state", Type: "string", MaxLength: 32, Description: "Only connections in this state, such as ESTABLISHED or LISTEN."},
				{Name: "limit", Type: "integer", Min: rowsLo, Max: rowsHi, Default: defaultRows, Description: "Processes and connections to return, each 1 to 500."},
			},
			Output: obj(map[string]any{
				"taken": typ("string", "When the table was read, RFC 3339 UTC."), "source": typ("string", "Which tool or file produced it."), "supported": typ("boolean", "False on a platform FormNET cannot read."),
				"error": typ("string", "Why the table is incomplete, when it is."), "processes": arr(map[string]any{"type": "object"}), "connections": arr(map[string]any{"type": "object"}),
				"matched_processes": typ("integer", "Processes that matched."), "matched_connections": typ("integer", "Connections that matched."), "truncated": typ("boolean", "True when more matched than were returned."),
				"total_processes": typ("integer", "Processes in the whole table."), "total_connections": typ("integer", "Connections in the whole table."),
			}, "taken", "supported", "processes", "connections", "truncated"),
			Run: machineSnapshot,
		},
		{
			Name:  "proxy_status",
			Title: "Proxy status",
			Description: "Read-only. Returns the proxy's address, whether it is listening, the interception mode (auto, on or off) and whether HTTPS is opened now, the passthrough list, the root certificate path, and the environment variables that point a program at the proxy. " +
				"Use it first in any task that routes a program through FormNET, and to read back a change made with set_interception or set_passthrough.",
			Class:  ReadOnly,
			Output: proxySchema,
			Run:    proxyStatus,
		},
		{
			Name:  "ca_info",
			Title: "Root certificate",
			Description: "Read-only. Returns FormNET's root certificate: path, common name, SHA-256 fingerprint, expiry, whether this computer trusts it, and the exact commands that trust and untrust it on this system. " +
				"Use it when HTTPS is only counted, when a program refuses the certificate, or when the person asks how to trust or remove the root. Show the command for the person to run; never run it, never invent another, never ask for the private key.",
			Class: ReadOnly,
			Output: obj(map[string]any{
				"path": typ("string", "The root certificate, PEM."), "common_name": typ("string", "Its subject common name."), "fingerprint": typ("string", "SHA-256 of the certificate, hex with colons."),
				"not_after": typ("string", "Expiry, RFC 3339."), "trusted": typ("boolean", "True when the system trust store accepts it."), "trust_command": typ("string", "Command the person runs to trust it."),
				"trust_hint": typ("string", "What that command does."), "untrust_command": typ("string", "Command the person runs to remove the trust."), "note": typ("string", "Who runs the commands."),
			}, "path", "fingerprint", "trusted", "trust_command", "untrust_command"),
			Run: caInfo,
		},
	}
}

func mergeProps(base map[string]any, extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range base["properties"].(map[string]any) {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
