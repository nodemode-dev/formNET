// Package flow is formnet's record of what actually crossed the wire.
//
// A Flow is one exchange the proxy saw: an HTTP request and its response, an
// opaque CONNECT tunnel the proxy did not open, or a WebSocket upgrade. Flows
// are values: a producer builds a new Flow for every change and hands it to
// the Store, which keeps the latest copy per id and tells subscribers.
package flow

import (
	"net"
	"net/url"
	"strings"
	"time"
)

// Kind says how much of the exchange formnet could see.
const (
	KindHTTP      = "http"      // a request and response, headers and bodies
	KindTunnel    = "tunnel"    // a CONNECT the proxy passed through: only the host and byte counts
	KindWebSocket = "websocket" // an upgrade the proxy forwarded: headers, then byte counts
	KindRefused   = "refused"   // a CONNECT the proxy opened whose client refused the certificate
)

// Header is one header line. Names are sorted (net/http keeps no wire order); the values of one name keep their order.
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Message is one side of an HTTP exchange.
type Message struct {
	Headers       []Header `json:"headers"`
	HeaderBytes   int64    `json:"header_bytes"`
	BodyBytes     int64    `json:"body_bytes"`     // the full size that crossed the wire
	Body          []byte   `json:"-"`              // the captured prefix, up to the body limit
	BodyTruncated bool     `json:"body_truncated"` // true when Body holds less than BodyBytes
	ContentType   string   `json:"content_type"`
	Encoding      string   `json:"encoding"` // the Content-Encoding as sent
	Decoded       bool     `json:"-"`        // Body holds the decoded form of a Content-Encoding
}

// TLSInfo describes the TLS the proxy negotiated with the upstream server.
type TLSInfo struct {
	ServerName  string `json:"server_name"`
	Version     string `json:"version"`
	Cipher      string `json:"cipher"`
	Intercepted bool   `json:"intercepted"` // false for a tunnel formnet only counted
}

// Flow is one exchange.
type Flow struct {
	ID      string     `json:"id"`
	Seq     uint64     `json:"seq"` // assigned by the Store in arrival order
	Kind    string     `json:"kind"`
	Start   time.Time  `json:"start"`
	End     *time.Time `json:"end,omitempty"`
	Pending bool       `json:"pending"` // true until the response (or the tunnel) ends

	Client        string `json:"client"`                   // ip:port of the program that used the proxy
	ClientPID     int    `json:"client_pid,omitempty"`     // the owner of that port, when the machine view knows it
	ClientProcess string `json:"client_process,omitempty"` // its name

	Scheme         string `json:"scheme"`
	Method         string `json:"method"`
	Host           string `json:"host"` // host without port
	Port           int    `json:"port"`
	Path           string `json:"path"`
	Query          string `json:"query"`
	URL            string `json:"url"`
	Protocol       string `json:"protocol"`                  // what the upstream answered with: HTTP/1.1, HTTP/2.0
	ClientProtocol string `json:"client_protocol,omitempty"` // what the program sent to the proxy

	TLS      *TLSInfo `json:"tls,omitempty"`
	Request  Message  `json:"request"`
	Response *Message `json:"response,omitempty"`
	Status   int      `json:"status"`

	BytesSent     int64 `json:"bytes_sent"`     // from the client towards the server
	BytesReceived int64 `json:"bytes_received"` // from the server towards the client

	ConnectMS  float64 `json:"connect_ms"`  // dial plus TLS to the upstream, when formnet made the connection
	TTFBMS     float64 `json:"ttfb_ms"`     // first response byte after the request was sent
	DurationMS float64 `json:"duration_ms"` // start to end

	Error string `json:"error,omitempty"`

	ResentFrom string `json:"resent_from,omitempty"` // the flow this one sent again, when FormNET resent it
	Edited     bool   `json:"edited,omitempty"`      // the resent request was changed before it went out

	Rules     []string `json:"rules,omitempty"`     // names of the rules that matched this exchange
	Paused    string   `json:"paused,omitempty"`    // request or response while it waits at a breakpoint
	Throttled bool     `json:"throttled,omitempty"` // the throttle slowed this exchange or tunnel
	Notes     []string `json:"notes,omitempty"`     // what FormNET did to it: blocked, mapped, edited, released

	ClientProto string          `json:"client_proto,omitempty"` // the protocol between the program and FormNET: HTTP/1.1, HTTP/2.0
	ServerProto string          `json:"server_proto,omitempty"` // the protocol between FormNET and the server, when a server answered
	Upstream    string          `json:"upstream,omitempty"`     // the upstream proxy this exchange went through (no credentials)
	Messages    *MessageSummary `json:"messages,omitempty"`     // WebSocket messages seen on this flow (messages.go)

	FromSession bool   `json:"from_session,omitempty"` // opened from a session file or imported, not captured in this run (session.go)
	Imported    string `json:"imported,omitempty"`     // the format it was imported from: har
}

// Summary is the Flow without bodies, as the list and the stream carry it.
// Bodies travel only through the detail route of one flow.
type Summary = Flow

// Completed returns a copy of f marked ended now.
func (f Flow) Completed(end time.Time) Flow {
	out := f
	out.End = &end
	out.Pending = false
	out.DurationMS = float64(end.Sub(f.Start)) / float64(time.Millisecond)
	return out
}

// secondLevel lists the labels that sit under a country-code TLD as a public
// registry (co.uk, com.au, ne.jp), so three labels make the domain there.
var secondLevel = map[string]bool{"co": true, "com": true, "org": true, "net": true, "gov": true, "edu": true, "ac": true, "ne": true, "or": true, "go": true}

// Domain is the registrable-looking tail of a host: the last two labels, or
// three when the host ends in a known second-level registry under a two-letter
// TLD. IP addresses are returned as they are. It groups subdomains of one
// owner without a public suffix list, so it is a grouping aid, not a rule.
func Domain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || net.ParseIP(strings.Trim(host, "[]")) != nil {
		return host
	}
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	if secondLevel[parts[len(parts)-2]] && len(parts[len(parts)-1]) == 2 {
		return strings.Join(parts[len(parts)-3:], ".")
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// WithURL fills URL, Scheme, Host, Port, Path and Query from a URL string.
func (f Flow) WithURL(raw string) Flow {
	out := f
	u, err := url.Parse(raw)
	if err != nil {
		out.URL = raw
		return out
	}
	out.URL = raw
	out.Scheme = u.Scheme
	out.Host = u.Hostname()
	out.Port = portOf(u)
	out.Path = u.Path
	out.Query = u.RawQuery
	return out
}

func portOf(u *url.URL) int {
	if p := u.Port(); p != "" {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	switch u.Scheme {
	case "https", "wss":
		return 443
	case "http", "ws":
		return 80
	}
	return 0
}
