// Package flow is formnet's record of what actually crossed the wire.
//
// A Flow is one exchange the proxy saw: an HTTP request and its response, an
// opaque CONNECT tunnel the proxy did not open, or a WebSocket upgrade. Flows
// are values: a producer builds a new Flow for every change and hands it to
// the Store, which keeps the latest copy per id and tells subscribers.
package flow

import (
	"net/url"
	"strings"
	"time"
)

// Kind says how much of the exchange formnet could see.
const (
	KindHTTP      = "http"      // a request and response, headers and bodies
	KindTunnel    = "tunnel"    // a CONNECT the proxy passed through: only the host and byte counts
	KindWebSocket = "websocket" // an upgrade the proxy forwarded: headers, then byte counts
)

// Header is one header line, kept in wire order because order is part of what a client sends.
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

	Scheme   string `json:"scheme"`
	Method   string `json:"method"`
	Host     string `json:"host"` // host without port
	Port     int    `json:"port"`
	Path     string `json:"path"`
	Query    string `json:"query"`
	URL      string `json:"url"`
	Protocol string `json:"protocol"` // HTTP/1.1, HTTP/2.0

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

// Domain is the registrable-looking tail of a host: the last two labels, or
// three when the second is a short country-code style label (co.uk, com.au).
// It groups subdomains of one owner without a public suffix list.
func Domain(host string) string {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	second := parts[len(parts)-2]
	if len(second) <= 3 && len(parts[len(parts)-1]) == 2 {
		return strings.Join(parts[len(parts)-3:], ".")
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// ParseURL fills Scheme, Host, Port, Path and Query from a URL string.
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
