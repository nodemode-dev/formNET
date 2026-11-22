package flow

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// HAR is the HTTP Archive 1.2 shape browsers and proxies exchange.
type HAR struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Comment string     `json:"comment,omitempty"`
	Entries []harEntry `json:"entries"`
}

// HAROptions choose what an export carries. The zero value redacts secrets.
type HAROptions struct {
	// Secrets keeps the values of the headers that carry credentials as captured.
	Secrets bool
}

const (
	harCommentRedacted = "Exported by FormNET with secrets redacted: the values of the Authorization, Proxy-Authorization, Cookie and Set-Cookie headers read [redacted] and the cookies lists are empty. Query strings and request bodies are as captured."
	harCommentSecrets  = "Exported by FormNET with secrets: every header is as captured, including Authorization, Proxy-Authorization, Cookie and Set-Cookie."
	redacted           = "[redacted]"
)

func (o HAROptions) comment() string {
	if o.Secrets {
		return harCommentSecrets
	}
	return harCommentRedacted
}

// secretHeader names the headers whose values a default export redacts.
func secretHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	}
	return false
}

// harSkips reports flows with no HTTP shape: tunnels and refused handshakes.
func harSkips(f Flow) bool {
	return f.Kind == KindTunnel || f.Kind == KindRefused
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitempty"`
	Comment         string      `json:"comment,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harRequest struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Cookies     []harNameValue `json:"cookies"`
	Headers     []harNameValue `json:"headers"`
	QueryString []harNameValue `json:"queryString"`
	PostData    *harPostData   `json:"postData,omitempty"`
	HeadersSize int64          `json:"headersSize"`
	BodySize    int64          `json:"bodySize"`
}

type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Comment  string `json:"comment,omitempty"`
}

type harResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Cookies     []harNameValue `json:"cookies"`
	Headers     []harNameValue `json:"headers"`
	Content     harContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int64          `json:"headersSize"`
	BodySize    int64          `json:"bodySize"`
}

type harContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type harTimings struct {
	Blocked float64 `json:"blocked"`
	DNS     float64 `json:"dns"`
	Connect float64 `json:"connect"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	SSL     float64 `json:"ssl"`
}

// ToHAR converts HTTP flows to a HAR document with secrets redacted. Tunnels
// and refused handshakes have no HTTP shape and are left out.
func ToHAR(flows []Flow, creatorVersion string) HAR {
	return ToHARWith(flows, creatorVersion, HAROptions{})
}

// ToHARWith converts HTTP flows to a HAR document as opt says.
func ToHARWith(flows []Flow, creatorVersion string, opt HAROptions) HAR {
	entries := make([]harEntry, 0, len(flows))
	for _, f := range flows {
		if harSkips(f) {
			continue
		}
		entries = append(entries, toEntry(f, opt))
	}
	return HAR{Log: harLog{Version: "1.2", Creator: harCreator{Name: "formnet", Version: creatorVersion}, Comment: opt.comment(), Entries: entries}}
}

// MarshalHAR writes flows as indented HAR JSON with secrets redacted.
func MarshalHAR(flows []Flow, creatorVersion string) ([]byte, error) {
	return json.MarshalIndent(ToHAR(flows, creatorVersion), "", "  ")
}

// HARWriter streams a HAR document entry by entry, so an export of thousands
// of flows never holds more than one flow's bodies at a time.
type HARWriter struct {
	w     io.Writer
	enc   *json.Encoder
	opt   HAROptions
	count int
	err   error
}

// NewHARWriter writes the head of a document with secrets redacted and returns a writer for entries.
func NewHARWriter(w io.Writer, creatorVersion string) (*HARWriter, error) {
	return NewHARWriterWith(w, creatorVersion, HAROptions{})
}

// NewHARWriterWith writes the document head and returns a writer for entries, as opt says.
func NewHARWriterWith(w io.Writer, creatorVersion string, opt HAROptions) (*HARWriter, error) {
	head, err := json.Marshal(harCreator{Name: "formnet", Version: creatorVersion})
	if err != nil {
		return nil, err
	}
	comment, err := json.Marshal(opt.comment())
	if err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(w, "{\n  \"log\": {\n    \"version\": \"1.2\",\n    \"creator\": %s,\n    \"comment\": %s,\n    \"entries\": [", head, comment); err != nil {
		return nil, err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &HARWriter{w: w, enc: enc, opt: opt}, nil
}

// Write adds one flow; tunnels and refused handshakes are skipped.
func (h *HARWriter) Write(f Flow) error {
	if h.err != nil || harSkips(f) {
		return h.err
	}
	sep := "\n"
	if h.count > 0 {
		sep = ",\n"
	}
	if _, err := io.WriteString(h.w, sep); err != nil {
		h.err = err
		return err
	}
	if err := h.enc.Encode(toEntry(f, h.opt)); err != nil {
		h.err = err
		return err
	}
	h.count++
	return nil
}

// Close writes the document tail.
func (h *HARWriter) Close() error {
	if h.err != nil {
		return h.err
	}
	_, h.err = io.WriteString(h.w, "\n    ]\n  }\n}\n")
	return h.err
}

func toEntry(f Flow, opt HAROptions) harEntry {
	clientProto := f.ClientProtocol
	if clientProto == "" {
		clientProto = f.Protocol
	}
	req := harRequest{
		Method:      f.Method,
		URL:         f.URL,
		HTTPVersion: clientProto,
		Cookies:     []harNameValue{},
		Headers:     nameValues(f.Request.Headers, opt),
		QueryString: queryValues(f.Query),
		HeadersSize: sizeOrUnknown(f.Request.HeaderBytes),
		BodySize:    f.Request.BodyBytes,
	}
	if len(f.Request.Body) > 0 {
		text, enc := bodyText(f.Request.Body)
		req.PostData = &harPostData{MimeType: f.Request.ContentType, Text: text, Comment: bodyComment(enc, f.Request)}
	}
	res := harResponse{
		Status:      f.Status,
		StatusText:  statusText(f.Status),
		HTTPVersion: f.Protocol,
		Cookies:     []harNameValue{},
		Headers:     []harNameValue{},
		RedirectURL: "",
		HeadersSize: -1,
		BodySize:    -1,
		Content:     harContent{Size: 0, MimeType: ""},
	}
	if f.Response != nil {
		res.Headers = nameValues(f.Response.Headers, opt)
		res.HeadersSize = sizeOrUnknown(f.Response.HeaderBytes)
		res.BodySize = f.Response.BodyBytes
		res.Content = harContent{Size: contentSize(*f.Response), MimeType: f.Response.ContentType}
		res.RedirectURL = headerValue(f.Response.Headers, "Location")
		if len(f.Response.Body) > 0 {
			var enc string
			res.Content.Text, enc = bodyText(f.Response.Body)
			res.Content.Encoding = enc
			res.Content.Comment = bodyComment(enc, *f.Response)
		}
	}
	wait := f.TTFBMS
	receive := f.DurationMS - f.ConnectMS - f.TTFBMS
	if receive < 0 {
		receive = 0
	}
	e := harEntry{
		StartedDateTime: f.Start.UTC().Format(time.RFC3339Nano),
		Time:            f.DurationMS,
		Request:         req,
		Response:        res,
		Timings:         harTimings{Blocked: -1, DNS: -1, Connect: f.ConnectMS, Send: 0, Wait: wait, Receive: receive, SSL: -1},
	}
	if f.Error != "" {
		e.Comment = "error: " + f.Error
	}
	return e
}

// sizeOrUnknown maps a header size formnet did not measure to HAR's -1.
func sizeOrUnknown(n int64) int64 {
	if n <= 0 {
		return -1
	}
	return n
}

// contentSize is the decoded body size HAR asks for: the captured length when
// the whole body was captured and decoded, else the wire size.
func contentSize(m Message) int64 {
	if m.Encoding != "" && !m.BodyTruncated && len(m.Body) > 0 {
		return int64(len(m.Body))
	}
	return m.BodyBytes
}

// bodyComment explains a body that is not plain text as captured.
func bodyComment(enc string, m Message) string {
	var notes []string
	if m.BodyTruncated {
		notes = append(notes, "truncated by formnet's body limit")
		if m.Encoding != "" {
			notes = append(notes, "still "+m.Encoding+"-encoded")
		}
	}
	if enc == "base64" {
		notes = append(notes, "base64")
	}
	return strings.Join(notes, "; ")
}

func nameValues(h []Header, opt HAROptions) []harNameValue {
	out := make([]harNameValue, len(h))
	for i, x := range h {
		out[i] = harNameValue{Name: x.Name, Value: x.Value}
		if !opt.Secrets && secretHeader(x.Name) {
			out[i].Value = redacted
		}
	}
	return out
}

// queryValues splits a query in its original order; a pair that does not
// unescape is kept as written rather than dropped.
func queryValues(raw string) []harNameValue {
	out := []harNameValue{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.FieldsFunc(raw, func(r rune) bool { return r == '&' || r == ';' }) {
		name, value, _ := strings.Cut(pair, "=")
		if n, err := url.QueryUnescape(name); err == nil {
			name = n
		}
		if v, err := url.QueryUnescape(value); err == nil {
			value = v
		}
		out = append(out, harNameValue{Name: name, Value: value})
	}
	return out
}

func headerValue(h []Header, name string) string {
	for _, x := range h {
		if strings.EqualFold(x.Name, name) {
			return x.Value
		}
	}
	return ""
}

// bodyText returns a body as text when it is valid UTF-8, otherwise base64.
func bodyText(b []byte) (text, encoding string) {
	if utf8.Valid(b) {
		return string(b), ""
	}
	return base64.StdEncoding.EncodeToString(b), "base64"
}

func statusText(code int) string {
	switch {
	case code == 0:
		return ""
	case code >= 100 && code < 200:
		return "Informational"
	case code >= 200 && code < 300:
		return "OK"
	case code >= 300 && code < 400:
		return "Redirect"
	case code >= 400 && code < 500:
		return "Client Error"
	case code >= 500:
		return "Server Error"
	}
	return ""
}
